// Package unpack reads a packed presentation back into its source tree.
//
// Everything an archive says about itself is untrusted. Paths are validated
// before a single byte is written, hashes are checked against the manifest,
// and extraction happens into a staging directory that is moved into place
// only once it is complete.
package unpack

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/pwagstro/slidepack/internal/archive"
	"github.com/pwagstro/slidepack/internal/diag"
	"github.com/pwagstro/slidepack/internal/envelope"
	"github.com/pwagstro/slidepack/internal/manifest"
	"github.com/pwagstro/slidepack/internal/pathutil"
	"github.com/pwagstro/slidepack/internal/source"
)

// MaxArchiveBytes caps how much slidepack will expand a payload into memory.
//
// This is a decompression-bomb guard, not a format limit: gzip can turn a few
// hundred kilobytes into gigabytes of zeros, and every consumer of this
// package holds the expanded archive in memory. 2 GiB is far beyond any real
// presentation while staying well inside what a 64-bit process can handle.
const MaxArchiveBytes = 2 << 30

// Error is a failure at a named stage of the load pipeline, carrying the
// stable diagnostic code that describes it.
type Error struct {
	Code diag.Code
	Path string
	Err  error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

func failf(code diag.Code, format string, args ...any) *Error {
	return &Error{Code: code, Err: fmt.Errorf(format, args...)}
}

// File is one regular file recovered from the archive.
type File struct {
	Path string
	Mode os.FileMode
	Data []byte
}

// Package is a fully decoded presentation.
type Package struct {
	Manifest *manifest.Manifest
	Files    []File
}

// Options controls how much verification Open performs.
type Options struct {
	// SkipFileHashes omits the per-file digest check. Only useful for tooling
	// that has already verified the payload digest and wants the bytes fast.
	SkipFileHashes bool
}

// OpenFile reads and decodes a packed presentation from disk.
func OpenFile(path string, opts Options) (*Package, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, failf(diag.NotSlidepack, "cannot read %s: %v", path, err)
	}
	return Open(data, opts)
}

// ReadManifest performs the cheap half of Open: envelope and manifest only.
//
// inspect uses this so that reporting on a 40 MB presentation does not decode,
// decompress and hash 40 MB of payload just to print a file listing.
func ReadManifest(data []byte) (*manifest.Manifest, *envelope.Document, error) {
	doc, err := envelope.Parse(data)
	if err != nil {
		if errors.Is(err, envelope.ErrNotSlidepack) {
			return nil, nil, &Error{Code: diag.NotSlidepack, Err: err}
		}
		return nil, nil, &Error{Code: diag.MalformedEnvelope, Err: err}
	}
	man, err := manifest.Unmarshal(doc.ManifestJSON)
	if err != nil {
		return nil, nil, &Error{Code: diag.MalformedManifest, Err: err}
	}
	if err := man.Validate(); err != nil {
		var uv *manifest.ErrUnsupportedVersion
		if errors.As(err, &uv) {
			return nil, nil, &Error{Code: diag.UnsupportedVersion, Err: err}
		}
		return nil, nil, &Error{Code: diag.MalformedManifest, Err: err}
	}
	return man, doc, nil
}

// Open decodes a packed presentation held in memory.
//
// Stages run in the order the format defines them, so the first thing that is
// wrong is the thing reported: a truncated file fails the payload digest check
// rather than producing a baffling gzip error.
func Open(data []byte, opts Options) (*Package, error) {
	man, doc, err := ReadManifest(data)
	if err != nil {
		return nil, err
	}

	gzBytes, err := base64.StdEncoding.DecodeString(string(doc.PayloadBase64))
	if err != nil {
		return nil, failf(diag.CorruptBase64, "the embedded payload is not valid base64: %v", err)
	}
	if int64(len(gzBytes)) != man.Payload.CompressedSize && man.Payload.CompressedSize != 0 {
		return nil, failf(diag.PayloadHashMismatch,
			"the payload is %d bytes but the manifest declares %d; the file is truncated or has been modified",
			len(gzBytes), man.Payload.CompressedSize)
	}

	sum := sha256.Sum256(gzBytes)
	if got := hex.EncodeToString(sum[:]); got != man.Payload.SHA256 {
		return nil, failf(diag.PayloadHashMismatch,
			"payload integrity check failed: archive hashes to %s but the manifest expects %s", got, man.Payload.SHA256)
	}

	// Bound the expansion before performing it. A few hundred kilobytes of
	// gzip can expand to gigabytes of zeros, so decompressing first and
	// checking the size afterwards would let a crafted file exhaust memory
	// before any check ran.
	limit := int64(MaxArchiveBytes)
	if declared := man.Payload.ArchiveSize; declared > 0 {
		if declared > MaxArchiveBytes {
			return nil, failf(diag.CorruptTar,
				"the manifest declares a %d byte archive, beyond the %d byte limit slidepack will expand",
				declared, int64(MaxArchiveBytes))
		}
		limit = declared
	}

	zr, err := gzip.NewReader(bytes.NewReader(gzBytes))
	if err != nil {
		return nil, failf(diag.CorruptGzip, "the payload is not a valid gzip stream: %v", err)
	}
	// One byte past the limit, so overrunning it is detectable rather than
	// silently truncating.
	tarBytes, err := io.ReadAll(io.LimitReader(zr, limit+1))
	if err != nil {
		return nil, failf(diag.CorruptGzip, "the payload could not be decompressed: %v", err)
	}
	if int64(len(tarBytes)) > limit {
		return nil, failf(diag.CorruptTar,
			"the payload expands to more than the %d bytes the manifest declares; refusing to continue", limit)
	}
	if err := zr.Close(); err != nil {
		return nil, failf(diag.CorruptGzip, "the gzip stream is truncated: %v", err)
	}
	gzBytes = nil

	if man.Payload.ArchiveSize != 0 && int64(len(tarBytes)) != man.Payload.ArchiveSize {
		return nil, failf(diag.CorruptTar,
			"the archive expands to %d bytes but the manifest declares %d", len(tarBytes), man.Payload.ArchiveSize)
	}

	var files []File
	seen := map[string]bool{}
	err = archive.ReadTar(bytes.NewReader(tarBytes), func(h archive.Header, body io.Reader) error {
		// archive.ReadTar has already run pathutil.Check on the name, so a
		// "../" or absolute entry never reaches this point.
		if seen[h.Path] {
			return fmt.Errorf("the archive contains %q twice", h.Path)
		}
		seen[h.Path] = true
		buf := make([]byte, h.Size)
		if _, err := io.ReadFull(body, buf); err != nil {
			return fmt.Errorf("entry %q is truncated: %w", h.Path, err)
		}
		files = append(files, File{Path: h.Path, Mode: h.Mode, Data: buf})
		return nil
	})
	if err != nil {
		return nil, failf(diag.CorruptTar, "%v", err)
	}
	tarBytes = nil

	pkg := &Package{Manifest: man, Files: files}
	if err := crossCheck(pkg, opts); err != nil {
		return nil, err
	}
	return pkg, nil
}

// crossCheck confirms that the archive and the manifest describe the same tree.
func crossCheck(pkg *Package, opts Options) error {
	inArchive := make(map[string]*File, len(pkg.Files))
	for i := range pkg.Files {
		inArchive[pkg.Files[i].Path] = &pkg.Files[i]
	}

	for _, mf := range pkg.Manifest.Files {
		f, ok := inArchive[mf.Path]
		if !ok {
			return &Error{Code: diag.ManifestMismatch, Path: mf.Path,
				Err: fmt.Errorf("the manifest lists %q but the archive does not contain it", mf.Path)}
		}
		if int64(len(f.Data)) != mf.Size {
			return &Error{Code: diag.ManifestMismatch, Path: mf.Path,
				Err: fmt.Errorf("%q is %d bytes in the archive but the manifest says %d", mf.Path, len(f.Data), mf.Size)}
		}
		if !opts.SkipFileHashes {
			sum := sha256.Sum256(f.Data)
			if got := hex.EncodeToString(sum[:]); got != mf.SHA256 {
				return &Error{Code: diag.FileHashMismatch, Path: mf.Path,
					Err: fmt.Errorf("%q hashes to %s but the manifest expects %s", mf.Path, got, mf.SHA256)}
			}
		}
	}

	if len(pkg.Files) != len(pkg.Manifest.Files) {
		listed := make(map[string]bool, len(pkg.Manifest.Files))
		for _, mf := range pkg.Manifest.Files {
			listed[mf.Path] = true
		}
		for _, f := range pkg.Files {
			if !listed[f.Path] {
				return &Error{Code: diag.ManifestMismatch, Path: f.Path,
					Err: fmt.Errorf("the archive contains %q but the manifest does not list it", f.Path)}
			}
		}
	}
	return nil
}

// Tree exposes the decoded package as a source tree, so that the same
// validation code can run against a packed file and against a directory.
func (p *Package) Tree() *source.MemTree {
	t := source.NewMemTree()
	for _, f := range p.Files {
		t.Add(f.Path, f.Mode, f.Data)
	}
	t.Sort()
	return t
}

// TotalSize returns the summed size of the recovered files.
func (p *Package) TotalSize() int64 {
	var n int64
	for _, f := range p.Files {
		n += int64(len(f.Data))
	}
	return n
}

// CheckPaths re-validates every archive path. ReadTar already does this, but
// running it again immediately before extraction makes the guarantee local to
// the code that writes files, where a future reader will look for it.
func (p *Package) CheckPaths() error {
	for _, f := range p.Files {
		if err := pathutil.Check(f.Path); err != nil {
			return &Error{Code: diag.InvalidPath, Path: f.Path, Err: fmt.Errorf("unsafe archive path %q: %w", f.Path, err)}
		}
	}
	return nil
}
