// Package pack turns a presentation source directory into a single packed HTML
// document.
//
// The pipeline is: walk -> validate -> deterministic tar -> gzip -> base64 ->
// HTML envelope. Validation runs before anything is written, so a source tree
// with a missing image or a remote dependency never becomes a knowingly broken
// package.
package pack

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/pwagstro/slidepack/internal/archive"
	"github.com/pwagstro/slidepack/internal/diag"
	"github.com/pwagstro/slidepack/internal/envelope"
	"github.com/pwagstro/slidepack/internal/manifest"
	"github.com/pwagstro/slidepack/internal/mimes"
	"github.com/pwagstro/slidepack/internal/source"
	"github.com/pwagstro/slidepack/internal/validate"
)

// Options configures a pack run.
type Options struct {
	// SourceDir is the presentation directory.
	SourceDir string
	// Output is the .html file to create.
	Output string
	// Entrypoint is the package path of the document to render; defaults to
	// index.html.
	Entrypoint string
	// Force allows an existing output file to be replaced.
	Force bool
	// Generator is recorded in the manifest, e.g. "slidepack/1.0.0".
	Generator string
}

// Result describes a completed pack.
type Result struct {
	Manifest   *manifest.Manifest
	Validation *diag.Result
	OutputPath string
	OutputSize int64
}

// ErrValidation reports that the source tree failed validation. The caller
// should render Result.Validation for the user.
var ErrValidation = errors.New("source validation failed")

// ValidationError carries the diagnostics that stopped a pack.
type ValidationError struct {
	Result *diag.Result
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("source validation failed: %s", validate.Summarize(e.Result))
}

func (e *ValidationError) Unwrap() error { return ErrValidation }

// Run packs a source directory into a single HTML file.
func Run(opts Options) (*Result, error) {
	entry := opts.Entrypoint
	if entry == "" {
		entry = source.DefaultEntrypoint
	}
	entry = filepath.ToSlash(entry)

	srcAbs, err := filepath.Abs(opts.SourceDir)
	if err != nil {
		return nil, err
	}
	outAbs, err := filepath.Abs(opts.Output)
	if err != nil {
		return nil, err
	}
	if err := checkOutputLocation(srcAbs, outAbs); err != nil {
		return nil, err
	}
	if !opts.Force {
		if _, err := os.Lstat(outAbs); err == nil {
			return nil, fmt.Errorf("%s already exists; pass --force to replace it", opts.Output)
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}

	tree, err := source.LoadDiskTree(srcAbs)
	if err != nil {
		return nil, err
	}

	vres := validate.Tree(tree, validate.Options{Entrypoint: entry})
	vres.Target = opts.SourceDir
	if !vres.Valid {
		return &Result{Validation: vres}, &ValidationError{Result: vres}
	}

	entries := tree.Entries()
	man := &manifest.Manifest{
		Format:     manifest.Format,
		Version:    manifest.Version,
		Generator:  opts.Generator,
		Entrypoint: entry,
		Payload: manifest.Payload{
			Archive:     "tar",
			Compression: "gzip",
			Encoding:    "base64",
		},
		Files: make([]manifest.File, 0, len(entries)),
	}

	// Build the archive into a temporary file first. The manifest must carry
	// the payload digest, and the manifest is written to the document *before*
	// the payload, so the payload has to exist in full before the envelope can
	// begin. Staging it on disk rather than in memory keeps peak memory
	// independent of presentation size.
	payloadFile, err := os.CreateTemp("", "slidepack-payload-*.tar.gz")
	if err != nil {
		return nil, fmt.Errorf("creating temporary payload file: %w", err)
	}
	defer func() {
		payloadFile.Close()
		os.Remove(payloadFile.Name())
	}()

	fileHashes := make([]hash.Hash, len(entries))
	tarEntries := make([]archive.Entry, len(entries))
	for i, e := range entries {
		i, e := i, e
		h := sha256.New()
		fileHashes[i] = h
		tarEntries[i] = archive.Entry{
			Path: e.Path,
			Mode: e.Mode,
			Size: e.Size,
			Open: func() (io.ReadCloser, error) {
				f, err := os.Open(e.FSPath)
				if err != nil {
					return nil, err
				}
				// Hashing as the bytes stream into the archive means each source
				// file is read exactly once, however large the tree is.
				return &hashingReader{r: io.TeeReader(f, h), c: f}, nil
			},
		}
	}

	payloadHash := sha256.New()
	counter := &countingWriter{w: io.MultiWriter(payloadFile, payloadHash)}
	gz, err := archive.NewDeterministicGzipWriter(counter)
	if err != nil {
		return nil, err
	}
	tarSize := &countingWriter{w: gz}
	if err := archive.WriteTar(tarSize, tarEntries); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("finishing gzip stream: %w", err)
	}

	man.Payload.SHA256 = hex.EncodeToString(payloadHash.Sum(nil))
	man.Payload.CompressedSize = counter.n
	man.Payload.ArchiveSize = tarSize.n

	for i, e := range entries {
		man.Files = append(man.Files, manifest.File{
			Path:   e.Path,
			Size:   e.Size,
			SHA256: hex.EncodeToString(fileHashes[i].Sum(nil)),
			MIME:   mimes.ForPath(e.Path),
			Mode:   fmt.Sprintf("%04o", e.Mode.Perm()),
		})
	}
	manifest.SortFiles(man.Files)

	title := documentTitle(tree, entry)

	if _, err := payloadFile.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(outAbs), 0o755); err != nil {
		return nil, fmt.Errorf("creating output directory: %w", err)
	}

	size, err := writeAtomic(outAbs, func(w io.Writer) error {
		return envelope.Write(w, envelope.WriteOptions{
			Manifest: man,
			Title:    title,
			Payload:  payloadFile,
		})
	})
	if err != nil {
		return nil, err
	}

	return &Result{Manifest: man, Validation: vres, OutputPath: outAbs, OutputSize: size}, nil
}

// checkOutputLocation refuses to write the packed file into the tree being
// packed, which would either archive a stale copy of the output or, worse,
// produce output that differs from run to run.
func checkOutputLocation(srcAbs, outAbs string) error {
	rel, err := filepath.Rel(srcAbs, outAbs)
	if err != nil {
		return nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil
	}
	return fmt.Errorf("output %s is inside the source directory %s; write the packed file somewhere else so it is not archived into itself", outAbs, srcAbs)
}

// documentTitle reads the entrypoint's <title> so the browser tab is right
// before any JavaScript runs. A failure here is cosmetic, never fatal.
func documentTitle(tree source.Tree, entry string) string {
	data, err := tree.Read(entry)
	if err != nil {
		return ""
	}
	return source.ScanHTML(data).Title
}

// writeAtomic writes through a temporary file in the destination directory and
// renames it into place, so a failure part-way through never leaves a
// half-written .html that looks like a real presentation.
func writeAtomic(dest string, fn func(io.Writer) error) (int64, error) {
	dir := filepath.Dir(dest)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(dest)+".tmp-*")
	if err != nil {
		return 0, fmt.Errorf("creating temporary output: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpName)
	}
	if err := fn(tmp); err != nil {
		cleanup()
		return 0, err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return 0, err
	}
	info, err := tmp.Stat()
	if err != nil {
		cleanup()
		return 0, err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return 0, err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		os.Remove(tmpName)
		return 0, err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		os.Remove(tmpName)
		return 0, fmt.Errorf("moving output into place: %w", err)
	}
	return info.Size(), nil
}

// countingWriter records how many bytes passed through it.
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// hashingReader pairs a tee'd reader with the underlying file's Close.
type hashingReader struct {
	r io.Reader
	c io.Closer
}

func (h *hashingReader) Read(p []byte) (int, error) { return h.r.Read(p) }
func (h *hashingReader) Close() error               { return h.c.Close() }
