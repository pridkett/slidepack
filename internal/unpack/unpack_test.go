package unpack

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pridkett/slidepack/internal/archive"
	"github.com/pridkett/slidepack/internal/diag"
	"github.com/pridkett/slidepack/internal/envelope"
	"github.com/pridkett/slidepack/internal/manifest"
	"github.com/pridkett/slidepack/internal/mimes"
)

/* ------------------------------------------------------------------ */
/* Building packed documents by hand, including hostile ones.          */
/* ------------------------------------------------------------------ */

type rawFile struct {
	path string
	mode os.FileMode
	data string
}

// buildDoc assembles a packed document from files, bypassing pack.Run so that
// tests can construct archives the packer would refuse to produce.
func buildDoc(t *testing.T, files []rawFile, mutate func(*manifest.Manifest)) []byte {
	t.Helper()
	tarBytes := buildTar(t, files, false)
	return wrap(t, tarBytes, files, mutate)
}

func buildTar(t *testing.T, files []rawFile, raw bool) []byte {
	t.Helper()
	var buf bytes.Buffer
	if raw {
		// Hand-rolled records, for paths WriteTar rejects.
		for _, f := range files {
			buf.Write(rawHeader(t, f))
			buf.WriteString(f.data)
			if pad := 512 - len(f.data)%512; pad != 512 {
				buf.Write(make([]byte, pad))
			}
		}
		buf.Write(make([]byte, 1024))
		return buf.Bytes()
	}
	entries := make([]archive.Entry, 0, len(files))
	for _, f := range files {
		f := f
		entries = append(entries, archive.Entry{
			Path: f.path, Mode: f.mode, Size: int64(len(f.data)),
			Open: func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(f.data)), nil },
		})
	}
	if err := archive.WriteTar(&buf, entries); err != nil {
		t.Fatalf("WriteTar: %v", err)
	}
	return buf.Bytes()
}

// rawHeader writes a USTAR record with no path validation at all.
func rawHeader(t *testing.T, f rawFile) []byte {
	t.Helper()
	b := make([]byte, 512)
	if len(f.path) > 100 {
		t.Fatalf("test helper only handles short names, got %d bytes", len(f.path))
	}
	copy(b[0:100], f.path)
	putOct(b[100:108], uint64(f.mode.Perm()))
	putOct(b[108:116], 0)
	putOct(b[116:124], 0)
	putOct(b[124:136], uint64(len(f.data)))
	putOct(b[136:148], 0)
	b[156] = '0'
	copy(b[257:263], "ustar\x00")
	copy(b[263:265], "00")
	for i := 148; i < 156; i++ {
		b[i] = ' '
	}
	var sum uint64
	for _, c := range b {
		sum += uint64(c)
	}
	copy(b[148:154], []byte(oct6(sum)))
	b[154] = 0
	b[155] = ' '
	return b
}

func putOct(field []byte, v uint64) {
	s := ""
	for v > 0 {
		s = string(rune('0'+(v&7))) + s
		v >>= 3
	}
	if s == "" {
		s = "0"
	}
	w := len(field) - 1
	for i := range field {
		field[i] = '0'
	}
	copy(field[w-len(s):w], s)
	field[w] = 0
}

func oct6(v uint64) string {
	s := ""
	for i := 0; i < 6; i++ {
		s = string(rune('0'+(v&7))) + s
		v >>= 3
	}
	return s
}

func wrap(t *testing.T, tarBytes []byte, files []rawFile, mutate func(*manifest.Manifest)) []byte {
	t.Helper()
	var gzBuf bytes.Buffer
	zw, err := archive.NewDeterministicGzipWriter(&gzBuf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := zw.Write(tarBytes); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	gz := gzBuf.Bytes()
	sum := sha256.Sum256(gz)

	m := &manifest.Manifest{
		Format: manifest.Format, Version: manifest.Version,
		Generator: "slidepack/test", Entrypoint: "index.html",
		Payload: manifest.Payload{
			Archive: "tar", Compression: "gzip", Encoding: "base64",
			SHA256:         hex.EncodeToString(sum[:]),
			CompressedSize: int64(len(gz)),
			ArchiveSize:    int64(len(tarBytes)),
		},
	}
	for _, f := range files {
		h := sha256.Sum256([]byte(f.data))
		m.Files = append(m.Files, manifest.File{
			Path: f.path, Size: int64(len(f.data)), SHA256: hex.EncodeToString(h[:]),
			MIME: mimes.ForPath(f.path), Mode: "0644",
		})
	}
	manifest.SortFiles(m.Files)
	if mutate != nil {
		mutate(m)
	}

	var out bytes.Buffer
	if err := envelope.Write(&out, envelope.WriteOptions{
		Manifest: m, Title: "Test", Payload: bytes.NewReader(gz),
	}); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func goodFiles() []rawFile {
	return []rawFile{
		{"index.html", 0o644, "<!doctype html><title>T</title>"},
		{"css/a.css", 0o644, "body{}"},
	}
}

/* ------------------------------------------------------------------ */
/* Happy path                                                          */
/* ------------------------------------------------------------------ */

func TestOpenAndExtract(t *testing.T) {
	doc := buildDoc(t, goodFiles(), nil)
	pkg, err := Open(doc, Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(pkg.Files) != 2 {
		t.Fatalf("got %d files", len(pkg.Files))
	}

	dest := filepath.Join(t.TempDir(), "out")
	res, err := Extract(pkg, dest, ExtractOptions{})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !res.Staged {
		t.Error("a fresh destination should be built in a staging directory and renamed")
	}
	body, err := os.ReadFile(filepath.Join(dest, "css", "a.css"))
	if err != nil || string(body) != "body{}" {
		t.Errorf("css/a.css = %q, %v", body, err)
	}
}

/* ------------------------------------------------------------------ */
/* Corruption and integrity                                            */
/* ------------------------------------------------------------------ */

func codeOf(t *testing.T, err error) diag.Code {
	t.Helper()
	var ue *Error
	if !errors.As(err, &ue) {
		t.Fatalf("want *unpack.Error, got %T: %v", err, err)
	}
	return ue.Code
}

func TestOpenRejectsNonSlidepackDocument(t *testing.T) {
	_, err := Open([]byte("<!doctype html><p>hello</p>"), Options{})
	if got := codeOf(t, err); got != diag.NotSlidepack {
		t.Errorf("code = %s, want %s", got, diag.NotSlidepack)
	}
}

func TestOpenRejectsUnsupportedVersion(t *testing.T) {
	doc := buildDoc(t, goodFiles(), func(m *manifest.Manifest) { m.Version = 7 })
	_, err := Open(doc, Options{})
	if got := codeOf(t, err); got != diag.UnsupportedVersion {
		t.Errorf("code = %s, want %s", got, diag.UnsupportedVersion)
	}
	if !strings.Contains(err.Error(), "version 7") {
		t.Errorf("error should name the version: %v", err)
	}
}

func TestOpenRejectsCorruptBase64(t *testing.T) {
	doc := buildDoc(t, goodFiles(), nil)
	doc = injectIntoPayload(t, doc, "!!!!")
	if got := codeOf(t, mustFail(t, doc)); got != diag.CorruptBase64 {
		t.Errorf("code = %s, want %s", got, diag.CorruptBase64)
	}
}

func TestOpenRejectsPayloadHashMismatch(t *testing.T) {
	doc := buildDoc(t, goodFiles(), nil)
	// Swap a run of payload characters for different, still-valid base64.
	doc = mutatePayload(t, doc, func(b []byte) {
		for i := len(b) / 2; i < len(b)/2+16; i++ {
			if b[i] == 'A' {
				b[i] = 'B'
			} else {
				b[i] = 'A'
			}
		}
	})
	err := mustFail(t, doc)
	code := codeOf(t, err)
	// Altered bytes usually break the digest first; on the rare occasion the
	// gzip stream itself becomes unreadable that is an equally correct report.
	if code != diag.PayloadHashMismatch && code != diag.CorruptGzip {
		t.Errorf("code = %s, want PAYLOAD_HASH_MISMATCH or CORRUPT_GZIP", code)
	}
}

func TestOpenRejectsCorruptGzip(t *testing.T) {
	// A payload whose digest matches but which is not a gzip stream at all.
	notGzip := []byte("this is definitely not gzip data")
	sum := sha256.Sum256(notGzip)
	m := &manifest.Manifest{
		Format: manifest.Format, Version: manifest.Version, Entrypoint: "index.html",
		Payload: manifest.Payload{
			Archive: "tar", Compression: "gzip", Encoding: "base64",
			SHA256: hex.EncodeToString(sum[:]), CompressedSize: int64(len(notGzip)),
		},
		Files: []manifest.File{{Path: "index.html", Size: 1, SHA256: strings.Repeat("0", 64), MIME: "text/html", Mode: "0644"}},
	}
	var buf bytes.Buffer
	if err := envelope.Write(&buf, envelope.WriteOptions{Manifest: m, Payload: bytes.NewReader(notGzip)}); err != nil {
		t.Fatal(err)
	}
	if got := codeOf(t, mustFail(t, buf.Bytes())); got != diag.CorruptGzip {
		t.Errorf("code = %s, want %s", got, diag.CorruptGzip)
	}
}

func TestOpenRejectsCorruptTar(t *testing.T) {
	tarBytes := buildTar(t, goodFiles(), false)
	copy(tarBytes[257:263], "xxxxx\x00") // destroy the USTAR magic
	doc := wrap(t, tarBytes, goodFiles(), nil)
	if got := codeOf(t, mustFail(t, doc)); got != diag.CorruptTar {
		t.Errorf("code = %s, want %s", got, diag.CorruptTar)
	}
}

func TestOpenRejectsFileHashMismatch(t *testing.T) {
	doc := buildDoc(t, goodFiles(), func(m *manifest.Manifest) {
		for i := range m.Files {
			if m.Files[i].Path == "css/a.css" {
				m.Files[i].SHA256 = strings.Repeat("f", 64)
			}
		}
	})
	err := mustFail(t, doc)
	if got := codeOf(t, err); got != diag.FileHashMismatch {
		t.Errorf("code = %s, want %s", got, diag.FileHashMismatch)
	}
	if !strings.Contains(err.Error(), "css/a.css") {
		t.Errorf("error should name the file: %v", err)
	}
}

func TestOpenRejectsManifestArchiveDisagreement(t *testing.T) {
	// Manifest claims a size the archive does not have.
	doc := buildDoc(t, goodFiles(), func(m *manifest.Manifest) {
		m.Files[0].Size = 9999
	})
	if got := codeOf(t, mustFail(t, doc)); got != diag.ManifestMismatch {
		t.Errorf("code = %s, want %s", got, diag.ManifestMismatch)
	}

	// Manifest lists a file the archive does not contain.
	doc = buildDoc(t, goodFiles(), func(m *manifest.Manifest) {
		m.Files = append(m.Files, manifest.File{
			Path: "zzz.txt", Size: 0, SHA256: strings.Repeat("0", 64), MIME: "text/plain", Mode: "0644",
		})
		manifest.SortFiles(m.Files)
	})
	if got := codeOf(t, mustFail(t, doc)); got != diag.ManifestMismatch {
		t.Errorf("code = %s, want %s", got, diag.ManifestMismatch)
	}
}

func TestOpenRejectsAnArchiveEntryTheManifestDoesNotList(t *testing.T) {
	files := append(goodFiles(), rawFile{"stowaway.js", 0o644, "evil()"})
	tarBytes := buildTar(t, files, false)
	doc := wrap(t, tarBytes, goodFiles(), nil) // manifest omits the stowaway
	err := mustFail(t, doc)
	if got := codeOf(t, err); got != diag.ManifestMismatch {
		t.Errorf("code = %s, want %s", got, diag.ManifestMismatch)
	}
	if !strings.Contains(err.Error(), "stowaway.js") {
		t.Errorf("error should name the extra file: %v", err)
	}
}

/* ------------------------------------------------------------------ */
/* Extraction safety                                                   */
/* ------------------------------------------------------------------ */

func TestTraversalPathsNeverEscape(t *testing.T) {
	// AC-022 / AC-023. These archives are built by hand because WriteTar
	// refuses to produce them.
	evilPaths := []string{
		"../../evil.txt",
		"../evil.txt",
		"/etc/slidepack-evil.txt",
		`..\..\evil.txt`,
		"a/../../evil.txt",
		`C:/Windows/evil.txt`,
	}
	for _, p := range evilPaths {
		t.Run(p, func(t *testing.T) {
			files := []rawFile{{p, 0o644, "pwned"}}
			tarBytes := buildTar(t, files, true)
			doc := wrap(t, tarBytes, files, nil)

			_, err := Open(doc, Options{})
			if err == nil {
				t.Fatalf("Open accepted an archive entry named %q", p)
			}

			// Nothing outside the destination may exist afterwards, and since
			// Open refused, nothing was written at all.
			sandbox := t.TempDir()
			dest := filepath.Join(sandbox, "dest")
			if entries, _ := os.ReadDir(sandbox); len(entries) != 0 {
				t.Errorf("files appeared in the sandbox: %v", entries)
			}
			if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
				t.Error("the destination was created despite the failure")
			}
		})
	}
}

func TestExtractRefusesToWriteThroughASymlink(t *testing.T) {
	pkg, err := Open(buildDoc(t, goodFiles(), nil), Options{})
	if err != nil {
		t.Fatal(err)
	}

	sandbox := t.TempDir()
	outside := filepath.Join(sandbox, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(sandbox, "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	// Plant a symlink where the archive wants to create a directory.
	if err := os.Symlink(outside, filepath.Join(dest, "css")); err != nil {
		t.Skipf("this platform cannot create symlinks: %v", err)
	}

	_, err = Extract(pkg, dest, ExtractOptions{Force: true})
	if err == nil {
		t.Fatal("Extract wrote through a symbolic link")
	}
	if !strings.Contains(err.Error(), "symbolic link") {
		t.Errorf("error should explain the refusal: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(outside, "a.css")); statErr == nil {
		t.Fatal("a file was written outside the destination")
	}
}

func TestExtractRefusesANonEmptyDestinationWithoutForce(t *testing.T) {
	pkg, err := Open(buildDoc(t, goodFiles(), nil), Options{})
	if err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	if err := os.WriteFile(filepath.Join(dest, "existing.txt"), []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Extract(pkg, dest, ExtractOptions{}); err == nil {
		t.Fatal("Extract overwrote a non-empty destination without --force")
	}
	if _, err := os.Stat(filepath.Join(dest, "index.html")); !os.IsNotExist(err) {
		t.Error("Extract wrote files despite refusing")
	}
	if body, _ := os.ReadFile(filepath.Join(dest, "existing.txt")); string(body) != "keep me" {
		t.Error("the pre-existing file was disturbed")
	}

	if _, err := Extract(pkg, dest, ExtractOptions{Force: true}); err != nil {
		t.Fatalf("Extract with --force: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "index.html")); err != nil {
		t.Errorf("--force did not write the files: %v", err)
	}
}

func TestExtractAcceptsAnEmptyExistingDirectory(t *testing.T) {
	pkg, err := Open(buildDoc(t, goodFiles(), nil), Options{})
	if err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	if _, err := Extract(pkg, dest, ExtractOptions{}); err != nil {
		t.Fatalf("an empty directory should be usable without --force: %v", err)
	}
}

func TestExtractRestoresModes(t *testing.T) {
	files := []rawFile{
		{"index.html", 0o644, "<p>x</p>"},
		{"bin/run.sh", 0o755, "#!/bin/sh\n"},
		{"private.txt", 0o600, "secret"},
	}
	pkg, err := Open(buildDoc(t, files, nil), Options{})
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "out")
	if _, err := Extract(pkg, dest, ExtractOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		info, err := os.Stat(filepath.Join(dest, filepath.FromSlash(f.path)))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != f.mode {
			t.Errorf("%s mode = %o, want %o", f.path, info.Mode().Perm(), f.mode)
		}
	}
}

func TestReadManifestDoesNotTouchThePayload(t *testing.T) {
	// A document whose payload is garbage but whose manifest is well-formed
	// must still be inspectable.
	doc := buildDoc(t, goodFiles(), nil)
	doc = injectIntoPayload(t, doc, "!!!!")
	m, _, err := ReadManifest(doc)
	if err != nil {
		t.Fatalf("ReadManifest should not decode the payload: %v", err)
	}
	if m.Entrypoint != "index.html" {
		t.Errorf("entrypoint = %q", m.Entrypoint)
	}
}

func TestTreeExposesTheRecoveredSource(t *testing.T) {
	pkg, err := Open(buildDoc(t, goodFiles(), nil), Options{})
	if err != nil {
		t.Fatal(err)
	}
	tree := pkg.Tree()
	if !tree.Has("css/a.css") {
		t.Error("Tree is missing a file")
	}
	if got, _ := tree.Read("css/a.css"); string(got) != "body{}" {
		t.Errorf("Tree.Read = %q", got)
	}
}

/* ------------------------------------------------------------------ */
/* Helpers for mutating a packed document                              */
/* ------------------------------------------------------------------ */

func mustFail(t *testing.T, doc []byte) error {
	t.Helper()
	_, err := Open(doc, Options{})
	if err == nil {
		t.Fatal("Open accepted a document it should have rejected")
	}
	return err
}

func payloadBounds(t *testing.T, doc []byte) (int, int) {
	t.Helper()
	idAt := bytes.Index(doc, []byte(`id="`+envelope.PayloadID+`"`))
	if idAt < 0 {
		t.Fatal("no payload element")
	}
	start := bytes.IndexByte(doc[idAt:], '>') + idAt + 2
	end := bytes.Index(doc[start:], []byte("</script")) + start
	return start, end
}

func injectIntoPayload(t *testing.T, doc []byte, s string) []byte {
	t.Helper()
	start, end := payloadBounds(t, doc)
	at := (start + end) / 2
	out := append([]byte{}, doc[:at]...)
	out = append(out, s...)
	return append(out, doc[at+len(s):]...)
}

func mutatePayload(t *testing.T, doc []byte, fn func([]byte)) []byte {
	t.Helper()
	start, end := payloadBounds(t, doc)
	out := append([]byte{}, doc...)
	fn(out[start:end])
	return out
}

func TestOpenRefusesADecompressionBomb(t *testing.T) {
	// A payload that expands far beyond what the manifest declares must be
	// rejected before the expansion completes, not after. The manifest here
	// claims a small archive while the gzip stream holds 64 MiB of zeros.
	bomb := make([]byte, 64<<20)
	var gzBuf bytes.Buffer
	zw, err := archive.NewDeterministicGzipWriter(&gzBuf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := zw.Write(bomb); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	gz := gzBuf.Bytes()
	if len(gz) > 1<<20 {
		t.Fatalf("test setup: compressed bomb is %d bytes, expected it to be tiny", len(gz))
	}
	sum := sha256.Sum256(gz)

	m := &manifest.Manifest{
		Format: manifest.Format, Version: manifest.Version, Entrypoint: "index.html",
		Payload: manifest.Payload{
			Archive: "tar", Compression: "gzip", Encoding: "base64",
			SHA256:         hex.EncodeToString(sum[:]),
			CompressedSize: int64(len(gz)),
			ArchiveSize:    2048, // a lie
		},
		Files: []manifest.File{
			{Path: "index.html", Size: 1, SHA256: strings.Repeat("0", 64), MIME: "text/html", Mode: "0644"},
		},
	}
	var doc bytes.Buffer
	if err := envelope.Write(&doc, envelope.WriteOptions{Manifest: m, Payload: bytes.NewReader(gz)}); err != nil {
		t.Fatal(err)
	}

	err = mustFail(t, doc.Bytes())
	if got := codeOf(t, err); got != diag.CorruptTar {
		t.Errorf("code = %s, want %s", got, diag.CorruptTar)
	}
	if !strings.Contains(err.Error(), "expands to more than") {
		t.Errorf("the error should say the expansion was cut short: %v", err)
	}
}

func TestOpenRefusesAnImplausibleDeclaredArchiveSize(t *testing.T) {
	doc := buildDoc(t, goodFiles(), func(m *manifest.Manifest) {
		m.Payload.ArchiveSize = MaxArchiveBytes + 1
	})
	err := mustFail(t, doc)
	if got := codeOf(t, err); got != diag.CorruptTar {
		t.Errorf("code = %s, want %s", got, diag.CorruptTar)
	}
}
