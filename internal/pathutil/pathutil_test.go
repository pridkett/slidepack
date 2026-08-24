package pathutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckAcceptsOrdinaryPaths(t *testing.T) {
	good := []string{
		"index.html",
		"css/style.css",
		"a/b/c/d/e.png",
		"assets/Revenue Chart – Europe.webp",
		"fonts/日本語.woff2",
		"file-without-extension",
		"a b/c d.txt",
		"weird#name.png",
		"percent%20literal.png",
	}
	for _, p := range good {
		if err := Check(p); err != nil {
			t.Errorf("Check(%q) = %v, want nil", p, err)
		}
	}
}

func TestCheckRejectsUnsafePaths(t *testing.T) {
	bad := map[string]string{
		"":                                  "empty",
		"/etc/passwd":                       "absolute",
		"../outside":                        "..",
		"a/../../outside":                   "..",
		"./a":                               "canonical",
		"a//b":                              "empty segment",
		"a/":                                "trailing slash",
		`a\b`:                               "backslash",
		`C:/Windows/x`:                      "drive letter",
		`c:x`:                               "drive letter",
		"a/b/../c":                          "..",
		"nul\x00byte":                       "NUL",
		"bell\x07":                          "control",
		"newline\nname":                     "control",
		strings.Repeat("a", MaxPathBytes+1): "too long",
	}
	for p, why := range bad {
		if err := Check(p); err == nil {
			t.Errorf("Check(%q) = nil, want an error (%s)", p, why)
		}
	}
}

func TestCheckRejectsInvalidUTF8(t *testing.T) {
	if err := Check("bad\xff\xfename.png"); err == nil {
		t.Fatal("Check accepted invalid UTF-8")
	}
}

func TestCheckUSTAR(t *testing.T) {
	seg := strings.Repeat("d", 40)

	// Short enough for the 100-byte name field.
	if err := CheckUSTAR("a/b/c.png"); err != nil {
		t.Errorf("short path rejected: %v", err)
	}

	// Long, but splittable into prefix + name.
	long := strings.Join([]string{seg, seg, seg, "file.png"}, "/")
	if len(long) <= 100 {
		t.Fatalf("test setup: expected a path longer than 100 bytes, got %d", len(long))
	}
	if err := CheckUSTAR(long); err != nil {
		t.Errorf("splittable path %d bytes rejected: %v", len(long), err)
	}

	// A single name longer than 100 bytes cannot be represented.
	if err := CheckUSTAR("dir/" + strings.Repeat("n", 120) + ".png"); err == nil {
		t.Error("CheckUSTAR accepted a name longer than the 100-byte field")
	}

	// A prefix longer than 155 bytes with no usable split point.
	deep := strings.Repeat(strings.Repeat("x", 30)+"/", 8) + strings.Repeat("y", 90)
	if len(deep) <= 256 {
		t.Logf("deep path is %d bytes", len(deep))
	}
	if err := CheckUSTAR(deep); err == nil {
		t.Error("CheckUSTAR accepted a path that exceeds prefix+name")
	}
}

func TestSafeJoinContainment(t *testing.T) {
	root := t.TempDir()
	got, err := SafeJoin(root, "a/b.txt")
	if err != nil {
		t.Fatalf("SafeJoin: %v", err)
	}
	want := filepath.Join(root, "a", "b.txt")
	if got != want {
		t.Errorf("SafeJoin = %q, want %q", got, want)
	}

	for _, p := range []string{"../escape", "/etc/passwd", `..\escape`, "a/../../escape", `C:/escape`} {
		if _, err := SafeJoin(root, p); err == nil {
			t.Errorf("SafeJoin(%q) succeeded; it must never escape the root", p)
		}
	}
}

func TestSafeJoinNeverLeavesRootEvenForOddNames(t *testing.T) {
	root := t.TempDir()
	// Names that look dangerous but are legal single components.
	for _, p := range []string{"...", "..foo", "foo..", ".hidden"} {
		got, err := SafeJoin(root, p)
		if err != nil {
			t.Fatalf("SafeJoin(%q): %v", p, err)
		}
		if !strings.HasPrefix(got, root+string(os.PathSeparator)) {
			t.Errorf("SafeJoin(%q) = %q, which is outside %q", p, got, root)
		}
	}
}

func TestResolveRef(t *testing.T) {
	cases := []struct {
		base, ref, want string
		ok              bool
	}{
		{"index.html", "css/style.css", "css/style.css", true},
		{"css/deck.css", "../fonts/f.woff2", "fonts/f.woff2", true},
		{"css/theme/theme.css", "../../assets/t.png", "assets/t.png", true},
		{"index.html", "/assets/logo.svg", "assets/logo.svg", true},
		// Root-relative paths clamp at the package root, as a browser does.
		{"index.html", "/../../assets/logo.svg", "assets/logo.svg", true},
		{"a/b/c.css", "./d.png", "a/b/d.png", true},
		{"index.html", "../outside.png", "", false},
		{"css/x.css", "../../outside.png", "", false},
	}
	for _, c := range cases {
		got, ok := ResolveRef(c.base, c.ref)
		if ok != c.ok || got != c.want {
			t.Errorf("ResolveRef(%q, %q) = (%q, %v), want (%q, %v)", c.base, c.ref, got, ok, c.want, c.ok)
		}
	}
}

func TestFromFS(t *testing.T) {
	if got := FromFS(filepath.Join("a", "b", "c.png")); got != "a/b/c.png" {
		t.Errorf("FromFS = %q, want a/b/c.png", got)
	}
}
