package mimes

import "testing"

func TestForPathCoversTheFormatsTheRuntimeNeeds(t *testing.T) {
	// The browser assigns these types to the Blob it creates for each file.
	// Firefox refuses a stylesheet served as anything but text/css in standards
	// mode, and a script needs a JavaScript type, so these are load-bearing.
	cases := map[string]string{
		"index.html":      "text/html; charset=utf-8",
		"deck.htm":        "text/html; charset=utf-8",
		"css/style.css":   "text/css; charset=utf-8",
		"js/app.js":       "text/javascript; charset=utf-8",
		"data.json":       "application/json",
		"assets/logo.svg": "image/svg+xml",
		"a.png":           "image/png",
		"a.jpg":           "image/jpeg",
		"a.jpeg":          "image/jpeg",
		"a.gif":           "image/gif",
		"a.webp":          "image/webp",
		"a.avif":          "image/avif",
		"f.woff":          "font/woff",
		"f.woff2":         "font/woff2",
		"f.ttf":           "font/ttf",
		"f.otf":           "font/otf",
		"clip.mp4":        "video/mp4",
		"clip.webm":       "video/webm",
		"sound.mp3":       "audio/mpeg",
		"subs.vtt":        "text/vtt; charset=utf-8",
	}
	for path, want := range cases {
		if got := ForPath(path); got != want {
			t.Errorf("ForPath(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestForPathIsCaseInsensitive(t *testing.T) {
	if got := ForPath("IMAGE.PNG"); got != "image/png" {
		t.Errorf("ForPath(IMAGE.PNG) = %q", got)
	}
}

func TestForPathFallsBackToOctetStream(t *testing.T) {
	for _, p := range []string{"README", "data.unknown", "archive.tar.zst", "noext"} {
		if got := ForPath(p); got != Default {
			t.Errorf("ForPath(%q) = %q, want %q", p, got, Default)
		}
	}
}

func TestForPathIgnoresDotsInDirectories(t *testing.T) {
	// "v1.2/README" has no extension even though the path contains a dot.
	if got := ForPath("v1.2/README"); got != Default {
		t.Errorf("ForPath = %q, want %q", got, Default)
	}
}

func TestClassifiers(t *testing.T) {
	if !IsHTML("a/b.HTML") || !IsHTML("x.htm") || IsHTML("x.txt") {
		t.Error("IsHTML is wrong")
	}
	if !IsCSS("a.CSS") || IsCSS("a.scss") {
		t.Error("IsCSS is wrong")
	}
	if !IsJS("a.js") || !IsJS("a.mjs") || IsJS("a.json") {
		t.Error("IsJS is wrong")
	}
}

func TestTableIsACopy(t *testing.T) {
	tbl := Table()
	tbl[".png"] = "tampered"
	if ForPath("x.png") != "image/png" {
		t.Error("Table() exposed the live map")
	}
}
