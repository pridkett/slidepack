package pack

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pwagstro/slidepack/internal/diag"
)

// tinyDeck writes a minimal valid presentation into dir.
func tinyDeck(t *testing.T, dir string) {
	t.Helper()
	write(t, filepath.Join(dir, "index.html"),
		"<!doctype html><html><head><meta charset=\"utf-8\"><title>Tiny</title>"+
			"<link rel=\"stylesheet\" href=\"a.css\"></head><body><p>hi</p></body></html>")
	write(t, filepath.Join(dir, "a.css"), "body{color:#123}")
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPackWritesOneFileAndNothingElse(t *testing.T) {
	src := t.TempDir()
	tinyDeck(t, src)
	outDir := t.TempDir()
	out := filepath.Join(outDir, "deck.html")

	res, err := Run(Options{SourceDir: src, Output: out, Generator: "slidepack/test"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.OutputSize == 0 {
		t.Error("OutputSize was not recorded")
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "deck.html" {
		t.Errorf("pack produced %v; it must create exactly one file and no companion directory", names(entries))
	}
}

func TestPackRefusesToOverwriteWithoutForce(t *testing.T) {
	src := t.TempDir()
	tinyDeck(t, src)
	out := filepath.Join(t.TempDir(), "deck.html")
	write(t, out, "existing content")

	if _, err := Run(Options{SourceDir: src, Output: out}); err == nil {
		t.Fatal("pack overwrote an existing file without --force")
	}
	if body, _ := os.ReadFile(out); string(body) != "existing content" {
		t.Error("the existing file was modified despite the refusal")
	}

	if _, err := Run(Options{SourceDir: src, Output: out, Force: true}); err != nil {
		t.Fatalf("--force should allow the overwrite: %v", err)
	}
	if body, _ := os.ReadFile(out); string(body) == "existing content" {
		t.Error("--force did not replace the file")
	}
}

func TestPackRefusesAnOutputInsideTheSourceDirectory(t *testing.T) {
	src := t.TempDir()
	tinyDeck(t, src)
	for _, out := range []string{
		filepath.Join(src, "deck.html"),
		filepath.Join(src, "build", "deck.html"),
	} {
		_, err := Run(Options{SourceDir: src, Output: out})
		if err == nil {
			t.Errorf("pack accepted output %s inside the source tree", out)
			continue
		}
		if !strings.Contains(err.Error(), "inside the source directory") {
			t.Errorf("error should explain the refusal: %v", err)
		}
	}
}

func TestPackFailsValidationWithoutWritingAnything(t *testing.T) {
	// AC-036: a failed pack leaves no misleading artifact behind.
	src := t.TempDir()
	write(t, filepath.Join(src, "index.html"), `<img src="missing.png">`)
	outDir := t.TempDir()
	out := filepath.Join(outDir, "deck.html")

	res, err := Run(Options{SourceDir: src, Output: out})
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want a ValidationError, got %T: %v", err, err)
	}
	if !errors.Is(err, ErrValidation) {
		t.Error("ValidationError should unwrap to ErrValidation")
	}
	if res == nil || res.Validation == nil {
		t.Fatal("the caller needs the diagnostics to show the user")
	}
	found := false
	for _, d := range res.Validation.Errors {
		if d.Code == diag.MissingResource {
			found = true
		}
	}
	if !found {
		t.Errorf("want MISSING_RESOURCE, got %+v", res.Validation.Errors)
	}

	entries, _ := os.ReadDir(outDir)
	if len(entries) != 0 {
		t.Errorf("a failed pack left files behind: %v", names(entries))
	}
}

func TestPackFailsOnAMissingEntrypoint(t *testing.T) {
	src := t.TempDir()
	write(t, filepath.Join(src, "slides.html"), "<p>x</p>")
	out := filepath.Join(t.TempDir(), "deck.html")

	if _, err := Run(Options{SourceDir: src, Output: out}); err == nil {
		t.Fatal("pack succeeded with no index.html")
	}
	if _, err := Run(Options{SourceDir: src, Output: out, Entrypoint: "slides.html"}); err != nil {
		t.Fatalf("--entry slides.html should work: %v", err)
	}
}

func TestPackFailsOnASymlink(t *testing.T) {
	// AC-024.
	src := t.TempDir()
	tinyDeck(t, src)
	if err := os.Symlink("a.css", filepath.Join(src, "b.css")); err != nil {
		t.Skipf("this platform cannot create symlinks: %v", err)
	}
	out := filepath.Join(t.TempDir(), "deck.html")
	_, err := Run(Options{SourceDir: src, Output: out})
	if err == nil {
		t.Fatal("pack accepted a source tree containing a symlink")
	}
	if !strings.Contains(err.Error(), "symbolic link") {
		t.Errorf("the error should say what is wrong: %v", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Error("a failed pack wrote an output file")
	}
}

func TestPackCreatesParentDirectories(t *testing.T) {
	src := t.TempDir()
	tinyDeck(t, src)
	out := filepath.Join(t.TempDir(), "a", "b", "deck.html")
	if _, err := Run(Options{SourceDir: src, Output: out}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("output not created: %v", err)
	}
}

func TestPackRecordsAManifestSortedByPath(t *testing.T) {
	src := t.TempDir()
	tinyDeck(t, src)
	write(t, filepath.Join(src, "zzz", "last.txt"), "z")
	write(t, filepath.Join(src, "000-first.txt"), "a")
	out := filepath.Join(t.TempDir(), "deck.html")

	res, err := Run(Options{SourceDir: src, Output: out, Generator: "slidepack/test"})
	if err != nil {
		t.Fatal(err)
	}
	var prev string
	for _, f := range res.Manifest.Files {
		if f.Path <= prev {
			t.Fatalf("manifest is not sorted: %q follows %q", f.Path, prev)
		}
		prev = f.Path
	}
	if res.Manifest.Entrypoint != "index.html" {
		t.Errorf("entrypoint = %q", res.Manifest.Entrypoint)
	}
	if res.Manifest.Payload.CompressedSize == 0 || res.Manifest.Payload.ArchiveSize == 0 {
		t.Error("payload sizes were not recorded")
	}
}

func TestPackTakesTheTitleFromTheEntrypoint(t *testing.T) {
	src := t.TempDir()
	tinyDeck(t, src)
	out := filepath.Join(t.TempDir(), "deck.html")
	if _, err := Run(Options{SourceDir: src, Output: out}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "<title>Tiny</title>") {
		t.Error("the outer document should carry the presentation's title")
	}
}

func TestPackSkipsOSJunk(t *testing.T) {
	src := t.TempDir()
	tinyDeck(t, src)
	write(t, filepath.Join(src, ".DS_Store"), "junk")
	write(t, filepath.Join(src, ".git", "config"), "[core]")
	out := filepath.Join(t.TempDir(), "deck.html")

	res, err := Run(Options{SourceDir: src, Output: out})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range res.Manifest.Files {
		if strings.Contains(f.Path, ".DS_Store") || strings.HasPrefix(f.Path, ".git/") {
			t.Errorf("packed a file that should be excluded: %s", f.Path)
		}
	}
}

func names(entries []os.DirEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name()
	}
	return out
}
