// Package integration exercises the whole pipeline: a source tree is created
// programmatically, packed, validated, unpacked and compared byte for byte.
//
// These are the tests that would notice if two individually correct components
// disagreed about, say, path normalization or mode handling.
package integration

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/pridkett/slidepack/internal/inspect"
	"github.com/pridkett/slidepack/internal/pack"
	"github.com/pridkett/slidepack/internal/unpack"
	"github.com/pridkett/slidepack/internal/validate"
)

/* ------------------------------------------------------------------ */
/* Source tree construction                                            */
/* ------------------------------------------------------------------ */

type file struct {
	path string
	mode os.FileMode
	data []byte
}

// kitchenSink returns a source tree covering every content and naming case the
// format is required to survive.
func kitchenSink() []file {
	index := `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Kitchen Sink</title>
<link rel="stylesheet" href="css/deck.css">
</head>
<body>
<h1 class="hero">Kitchen sink</h1>
<img src="assets/pixel.png" alt="Pixel">
<img src="assets/logo.svg" alt="Logo">
<img src="assets/Chart%20%E2%80%93%20Q1.png" alt="Chart">
<p class="fonted">Fonted</p>
<a href="#top">Top</a>
<a href="https://example.com">External</a>
<script src="js/deck.js"></script>
</body>
</html>
`
	deckCSS := `@import url("theme/base.css");

@font-face {
  font-family: "Sink";
  src: url("../fonts/sink.ttf") format("truetype");
}

.fonted { font-family: "Sink", serif; }
.hero { background-image: url("../assets/pixel.png"); }
`
	baseCSS := `/* url(decoy.png) in a comment */
body { margin: 0; content: "url(also-decoy.png)"; }
.tile { background: url(../../assets/logo.svg); }
`
	return []file{
		{"index.html", 0o644, []byte(index)},
		{"css/deck.css", 0o644, []byte(deckCSS)},
		{"css/theme/base.css", 0o644, []byte(baseCSS)},
		{"js/deck.js", 0o755, []byte("(function(){window.__sink=true;})();\n")},
		{"assets/logo.svg", 0o644, []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="8" height="8"><rect width="8" height="8"/></svg>`)},
		{"assets/pixel.png", 0o644, onePixelPNG()},
		{"assets/Chart – Q1.png", 0o644, onePixelPNG()},
		{"fonts/sink.ttf", 0o644, pseudoBinary(512)},
		{"data/empty", 0o644, nil},
		{"data/binary.bin", 0o600, pseudoBinary(4096)},
		{"LICENSE", 0o644, []byte("MIT\n")},
		{"deeply/nested/directory/tree/with/several/levels/note.txt", 0o644, []byte("deep\n")},
	}
}

// onePixelPNG is a valid 1x1 PNG, small enough to inline.
func onePixelPNG() []byte {
	return []byte{
		0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
		0, 0, 0, 0x0d, 'I', 'H', 'D', 'R',
		0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0,
		0x1f, 0x15, 0xc4, 0x89,
		0, 0, 0, 0x0a, 'I', 'D', 'A', 'T',
		0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05, 0x00, 0x01,
		0x0d, 0x0a, 0x2d, 0xb4,
		0, 0, 0, 0, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
	}
}

// pseudoBinary produces deterministic bytes spanning the full 0-255 range,
// including NULs, so that any accidental text handling shows up immediately.
func pseudoBinary(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte((i*7 + i/256*13) % 256)
	}
	return b
}

func writeTree(t *testing.T, root string, files []file) {
	t.Helper()
	for _, f := range files {
		p := filepath.Join(root, filepath.FromSlash(f.path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, f.data, f.mode); err != nil {
			t.Fatal(err)
		}
		// WriteFile is subject to umask, so set the mode explicitly.
		if err := os.Chmod(p, f.mode); err != nil {
			t.Fatal(err)
		}
	}
}

// snapshot walks a directory into a comparable map of path -> content+mode.
func snapshot(t *testing.T, root string) map[string]struct {
	Data []byte
	Mode os.FileMode
} {
	t.Helper()
	out := map[string]struct {
		Data []byte
		Mode os.FileMode
	}{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = struct {
			Data []byte
			Mode os.FileMode
		}{data, info.Mode().Perm()}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func packTree(t *testing.T, src, out string, opts ...func(*pack.Options)) *pack.Result {
	t.Helper()
	o := pack.Options{SourceDir: src, Output: out, Force: true, Generator: "slidepack/test"}
	for _, f := range opts {
		f(&o)
	}
	res, err := pack.Run(o)
	if err != nil {
		if res != nil && res.Validation != nil {
			t.Fatalf("pack failed: %v\ndiagnostics: %+v", err, res.Validation.Errors)
		}
		t.Fatalf("pack failed: %v", err)
	}
	return res
}

/* ------------------------------------------------------------------ */
/* Round trip                                                          */
/* ------------------------------------------------------------------ */

func TestRoundTripPreservesEveryFileExactly(t *testing.T) {
	src := t.TempDir()
	files := kitchenSink()
	writeTree(t, src, files)

	work := t.TempDir()
	out := filepath.Join(work, "deck.html")
	res := packTree(t, src, out)

	if got, want := len(res.Manifest.Files), len(files); got != want {
		t.Fatalf("manifest lists %d files, source has %d", got, want)
	}

	// The packed file validates as a package.
	doc, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if vres := validate.Packed(doc, out); !vres.Valid {
		t.Fatalf("packed file failed validation: %+v", vres.Errors)
	}

	dest := filepath.Join(work, "restored")
	pkg, err := unpack.OpenFile(out, unpack.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := unpack.Extract(pkg, dest, unpack.ExtractOptions{}); err != nil {
		t.Fatal(err)
	}

	before, after := snapshot(t, src), snapshot(t, dest)
	if len(before) != len(after) {
		t.Fatalf("restored %d files, source had %d\nbefore=%v\nafter=%v",
			len(after), len(before), sortedKeys(before), sortedKeys(after))
	}
	for path, want := range before {
		got, ok := after[path]
		if !ok {
			t.Errorf("path %q was not restored", path) // AC-014
			continue
		}
		if !bytes.Equal(got.Data, want.Data) { // AC-013
			t.Errorf("%q: content differs (%d vs %d bytes)", path, len(got.Data), len(want.Data))
		}
		if runtime.GOOS != "windows" && got.Mode != want.Mode { // AC-015
			t.Errorf("%q: mode %o restored as %o", path, want.Mode, got.Mode)
		}
	}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestRoundTripOfTheCommittedFixtures(t *testing.T) {
	for _, name := range []string{"basic", "nested", "browser"} {
		t.Run(name, func(t *testing.T) {
			src := filepath.Join("..", "..", "testdata", name)
			work := t.TempDir()
			out := filepath.Join(work, "deck.html")
			packTree(t, src, out)

			dest := filepath.Join(work, "restored")
			pkg, err := unpack.OpenFile(out, unpack.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := unpack.Extract(pkg, dest, unpack.ExtractOptions{}); err != nil {
				t.Fatal(err)
			}
			before, after := snapshot(t, src), snapshot(t, dest)
			for path, want := range before {
				got, ok := after[path]
				if !ok {
					t.Errorf("%q missing after round trip", path)
					continue
				}
				if !bytes.Equal(got.Data, want.Data) {
					t.Errorf("%q content differs", path)
				}
			}
			if len(before) != len(after) {
				t.Errorf("file count changed: %d -> %d", len(before), len(after))
			}
		})
	}
}

/* ------------------------------------------------------------------ */
/* Reproducibility                                                     */
/* ------------------------------------------------------------------ */

func TestPackingIsReproducible(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, kitchenSink())
	work := t.TempDir()

	one := filepath.Join(work, "one.html")
	two := filepath.Join(work, "two.html")
	packTree(t, src, one)
	packTree(t, src, two)

	a, b := read(t, one), read(t, two)
	if !bytes.Equal(a, b) { // AC-016
		t.Fatalf("packing the same source twice produced different output (%d vs %d bytes)", len(a), len(b))
	}
}

func TestMtimesDoNotAffectOutput(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, kitchenSink())
	work := t.TempDir()

	one := filepath.Join(work, "one.html")
	packTree(t, src, one)

	// Move every timestamp far into the past; nothing about the output may move.
	past := time.Date(1999, 3, 4, 5, 6, 7, 0, time.UTC)
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chtimes(p, past, past)
	})
	if err != nil {
		t.Fatal(err)
	}

	two := filepath.Join(work, "two.html")
	packTree(t, src, two)

	if !bytes.Equal(read(t, one), read(t, two)) { // AC-017
		t.Fatal("changing only modification times changed the packed output")
	}
}

func TestOneChangedByteChangesTheOutput(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, kitchenSink())
	work := t.TempDir()

	one := filepath.Join(work, "one.html")
	res1 := packTree(t, src, one)

	target := filepath.Join(src, "css", "deck.css")
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	body[len(body)-2] ^= 0x01
	if err := os.WriteFile(target, body, 0o644); err != nil {
		t.Fatal(err)
	}

	two := filepath.Join(work, "two.html")
	res2 := packTree(t, src, two)

	if bytes.Equal(read(t, one), read(t, two)) { // AC-018
		t.Fatal("changing a source byte did not change the packed output")
	}
	h1 := digestOf(res1, "css/deck.css")
	h2 := digestOf(res2, "css/deck.css")
	if h1 == h2 {
		t.Errorf("the manifest digest for css/deck.css did not change: %s", h1)
	}
	if res1.Manifest.Payload.SHA256 == res2.Manifest.Payload.SHA256 {
		t.Error("the payload digest did not change")
	}
}

func digestOf(res *pack.Result, path string) string {
	f, _ := res.Manifest.Lookup(path)
	return f.SHA256
}

func TestManifestDigestsMatchTheSourceFiles(t *testing.T) {
	src := t.TempDir()
	files := kitchenSink()
	writeTree(t, src, files)
	res := packTree(t, src, filepath.Join(t.TempDir(), "deck.html"))

	for _, f := range files {
		entry, ok := res.Manifest.Lookup(f.path)
		if !ok {
			t.Errorf("manifest omits %q", f.path)
			continue
		}
		sum := sha256.Sum256(f.data)
		if want := hex.EncodeToString(sum[:]); entry.SHA256 != want {
			t.Errorf("%q digest = %s, want %s", f.path, entry.SHA256, want)
		}
		if entry.Size != int64(len(f.data)) {
			t.Errorf("%q size = %d, want %d", f.path, entry.Size, len(f.data))
		}
		if runtime.GOOS != "windows" {
			if want := fmt.Sprintf("%04o", f.mode); entry.Mode != want {
				t.Errorf("%q mode = %s, want %s", f.path, entry.Mode, want)
			}
		}
	}
}

/* ------------------------------------------------------------------ */
/* Inspection                                                          */
/* ------------------------------------------------------------------ */

func TestInspectReportsWithoutExtracting(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, kitchenSink())
	out := filepath.Join(t.TempDir(), "deck.html")
	res := packTree(t, src, out)

	report, err := inspect.Inspect(out)
	if err != nil {
		t.Fatal(err)
	}
	if report.Entrypoint != "index.html" {
		t.Errorf("entrypoint = %q", report.Entrypoint)
	}
	if report.FileCount != len(res.Manifest.Files) {
		t.Errorf("fileCount = %d, want %d", report.FileCount, len(res.Manifest.Files))
	}
	if report.PayloadSHA256 != res.Manifest.Payload.SHA256 {
		t.Error("payload digest mismatch")
	}
	if report.SourceSize == 0 || report.ArchiveSize == 0 || report.CompressedSize == 0 {
		t.Errorf("sizes not populated: %+v", report)
	}

	var buf bytes.Buffer
	if err := report.WriteText(&buf); err != nil {
		t.Fatal(err)
	}
	text := buf.String()
	for _, want := range []string{"index.html", "Payload SHA-256", "not signatures"} {
		if !strings.Contains(text, want) {
			t.Errorf("human-readable report is missing %q", want)
		}
	}

	buf.Reset()
	if err := report.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(bytes.TrimSpace(buf.Bytes()), []byte("{")) {
		t.Error("JSON report is not a JSON object")
	}
}

/* ------------------------------------------------------------------ */
/* Scale                                                               */
/* ------------------------------------------------------------------ */

// TestLargePresentationRoundTrips builds a multi-megabyte tree with many files.
//
// The point is not wall-clock time, which would be a flaky assertion, but to
// catch the mistakes that only appear at size: repeated full-payload copies,
// quadratic archive handling, an int32 truncation, or a helper that assumed
// everything fits comfortably in memory.
func TestLargePresentationRoundTrips(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the multi-megabyte round trip in -short mode")
	}
	src := t.TempDir()

	const (
		assetCount = 240
		assetSize  = 24 * 1024 // ~5.6 MiB of incompressible payload in total
	)

	var html strings.Builder
	html.WriteString("<!doctype html>\n<html lang=\"en\"><head><meta charset=\"utf-8\">\n")
	html.WriteString("<title>Large Deck</title>\n<link rel=\"stylesheet\" href=\"css/deck.css\">\n</head>\n<body>\n")

	var css strings.Builder
	css.WriteString("body { margin: 0 }\n")

	for i := 0; i < assetCount; i++ {
		name := fmt.Sprintf("assets/blob-%03d.bin", i)
		// Random bytes so gzip cannot collapse the payload to nothing; the
		// point is to move real volume through every stage.
		data := make([]byte, assetSize)
		if _, err := rand.Read(data); err != nil {
			t.Fatal(err)
		}
		writeTree(t, src, []file{{name, 0o644, data}})
		fmt.Fprintf(&html, "<img src=\"%s\" alt=\"a%d\">\n", name, i)
		fmt.Fprintf(&css, ".b%03d { background-image: url(\"../%s\"); }\n", i, name)
	}
	html.WriteString("</body></html>\n")

	writeTree(t, src, []file{
		{"index.html", 0o644, []byte(html.String())},
		{"css/deck.css", 0o644, []byte(css.String())},
	})

	work := t.TempDir()
	out := filepath.Join(work, "large.html")
	res := packTree(t, src, out)

	if res.Manifest.TotalSize() < 5<<20 {
		t.Fatalf("test setup produced only %d bytes of source", res.Manifest.TotalSize())
	}
	if res.Manifest.Payload.ArchiveSize <= res.Manifest.TotalSize() {
		t.Errorf("archive (%d) should be at least as large as its contents (%d)",
			res.Manifest.Payload.ArchiveSize, res.Manifest.TotalSize())
	}

	doc := read(t, out)
	if vres := validate.Packed(doc, out); !vres.Valid {
		t.Fatalf("large package failed validation: %+v", vres.Errors)
	}

	dest := filepath.Join(work, "restored")
	pkg, err := unpack.OpenFile(out, unpack.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := unpack.Extract(pkg, dest, unpack.ExtractOptions{}); err != nil {
		t.Fatal(err)
	}

	before, after := snapshot(t, src), snapshot(t, dest)
	if len(before) != len(after) {
		t.Fatalf("restored %d of %d files", len(after), len(before))
	}
	for path, want := range before {
		if !bytes.Equal(after[path].Data, want.Data) {
			t.Fatalf("%q differs after a large round trip", path)
		}
	}
}

func BenchmarkPackTypicalDeck(b *testing.B) {
	src := b.TempDir()
	for _, f := range kitchenSink() {
		p := filepath.Join(src, filepath.FromSlash(f.path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(p, f.data, f.mode); err != nil {
			b.Fatal(err)
		}
	}
	out := filepath.Join(b.TempDir(), "deck.html")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := pack.Run(pack.Options{SourceDir: src, Output: out, Force: true, Generator: "bench"}); err != nil {
			b.Fatal(err)
		}
	}
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
