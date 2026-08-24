package source

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCanonicalModeOnAPOSIXFilesystem(t *testing.T) {
	// Where the filesystem carries permissions, they are recorded verbatim.
	for _, m := range []os.FileMode{0o644, 0o755, 0o600, 0o444, 0o640, 0o700} {
		if got := CanonicalMode(m, true); got != m {
			t.Errorf("CanonicalMode(%o, true) = %o, want %o", m, got, m)
		}
	}
	// Type bits never reach the manifest.
	if got := CanonicalMode(os.ModeDir|0o755, true); got != 0o755 {
		t.Errorf("CanonicalMode kept non-permission bits: %o", got)
	}
}

func TestCanonicalModeWithoutPOSIXPermissions(t *testing.T) {
	// The two values Go can report on Windows, plus the values a POSIX host
	// would report for the same files, all collapse onto the same canonical
	// pair. That collapse is what makes output match across platforms.
	cases := map[os.FileMode]os.FileMode{
		0o666: ModeFile,     // Windows: writable
		0o444: ModeReadOnly, // Windows: read-only
		0o644: ModeFile,
		0o755: ModeFile,
		0o600: ModeFile,
		0o400: ModeReadOnly,
		0o000: ModeReadOnly,
	}
	for in, want := range cases {
		if got := CanonicalMode(in, false); got != want {
			t.Errorf("CanonicalMode(%o, false) = %o, want %o", in, got, want)
		}
	}
}

// withoutPOSIXModes makes the walker behave as it does on a filesystem with no
// POSIX permissions, so the Windows path can be exercised anywhere.
func withoutPOSIXModes(t *testing.T) {
	t.Helper()
	previous := hostPreservesModes
	hostPreservesModes = false
	t.Cleanup(func() { hostPreservesModes = previous })
}

func writeAt(t *testing.T, path string, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	// WriteFile is subject to umask, so set the mode explicitly.
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDiskTreeCanonicalisesModesWhenTheHostCannotExpressThem(t *testing.T) {
	withoutPOSIXModes(t)

	root := t.TempDir()
	// The two modes Go reports on Windows.
	writeAt(t, filepath.Join(root, "index.html"), "<p>x</p>", 0o666)
	writeAt(t, filepath.Join(root, "frozen.txt"), "read only", 0o444)

	tree, err := LoadDiskTree(root)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]os.FileMode{
		"index.html": ModeFile,
		"frozen.txt": ModeReadOnly,
	}
	for _, e := range tree.Entries() {
		if want[e.Path] != e.Mode {
			t.Errorf("%s recorded as %o, want %o", e.Path, e.Mode, want[e.Path])
		}
	}
}

func TestModesRecordedIdenticallyAcrossPlatforms(t *testing.T) {
	// The same presentation, as two filesystems present it: a POSIX host
	// reporting 0644/0444, and a Windows host reporting 0666/0444.
	//
	// Everything downstream -- the manifest entries, the tar headers, the gzip
	// stream, the base64 payload -- is derived purely from these entries and
	// the file bytes. So if the entries match, the packed documents match, and
	// packing is reproducible across platforms.
	files := []struct{ path, body string }{
		{"index.html", "<!doctype html><title>T</title><p>x</p>"},
		{"css/deck.css", "body{margin:0}"},
		{"assets/frozen.bin", "\x00\x01\x02"},
	}

	load := func(writable, readonly os.FileMode) []Entry {
		root := t.TempDir()
		for i, f := range files {
			mode := writable
			if i == len(files)-1 {
				mode = readonly
			}
			writeAt(t, filepath.Join(root, filepath.FromSlash(f.path)), f.body, mode)
		}
		tree, err := LoadDiskTree(root)
		if err != nil {
			t.Fatal(err)
		}
		// FSPath is a host detail that never reaches the archive; clear it so
		// the comparison is about what gets recorded.
		entries := append([]Entry(nil), tree.Entries()...)
		for i := range entries {
			entries[i].FSPath = ""
		}
		return entries
	}

	posix := load(0o644, 0o444)

	withoutPOSIXModes(t)
	windows := load(0o666, 0o444)

	if !reflect.DeepEqual(posix, windows) {
		t.Errorf("entries differ between platforms:\n posix   = %+v\n windows = %+v", posix, windows)
	}
}
