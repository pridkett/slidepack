package source

import (
	"reflect"
	"strings"
	"testing"

	"github.com/pwagstro/slidepack/internal/diag"
)

func cssValues(src string) []string {
	var out []string
	for _, r := range ScanCSS(src) {
		out = append(out, r.Value)
	}
	return out
}

func TestScanCSSFindsEveryURLForm(t *testing.T) {
	src := `
@import "reset.css";
@import url(theme.css);
@import url( "spaced.css" ) screen;
.a { background: url(plain.png); }
.b { background: url('single.png'); }
.c { background: url("double.png"); }
.d { background: url(   padded.png   ); }
.e { background-image: url("with space.png"); }
.f { src: url("../fonts/f.woff2") format("woff2"); }
`
	want := []string{
		"reset.css", "theme.css", "spaced.css",
		"plain.png", "single.png", "double.png", "padded.png",
		"with space.png", "../fonts/f.woff2",
	}
	if got := cssValues(src); !reflect.DeepEqual(got, want) {
		t.Errorf("ScanCSS = %q\nwant %q", got, want)
	}
}

func TestScanCSSIgnoresLookalikes(t *testing.T) {
	src := `
/* url(commented-out.png) is only a comment */
.a::after { content: "url(in-a-string.png)"; }
.b::before { content: 'url(single-quoted.png)'; }
.c { background: myurl(not-a-function.png); }
.d { background: url(data:image/png;base64,AAAA); }
.e { --custom: "url(custom-property.png)"; }
`
	got := cssValues(src)
	// Only the data: URL is a genuine url() token; everything else is a decoy.
	want := []string{"data:image/png;base64,AAAA"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ScanCSS = %q, want %q", got, want)
	}
}

func TestScanCSSSpansAllowSplicing(t *testing.T) {
	src := `.a { background: url("assets/x.png"); }`
	refs := ScanCSS(src)
	if len(refs) != 1 {
		t.Fatalf("got %d refs, want 1", len(refs))
	}
	r := refs[0]
	if src[r.Start:r.End] != "assets/x.png" {
		t.Errorf("span %d:%d covers %q, want the URL text", r.Start, r.End, src[r.Start:r.End])
	}
	spliced := src[:r.Start] + "blob:xyz" + src[r.End:]
	if spliced != `.a { background: url("blob:xyz"); }` {
		t.Errorf("splice produced %q", spliced)
	}
}

func TestScanCSSLineNumbers(t *testing.T) {
	src := "a{}\nb{}\n.c { background: url(x.png); }\n"
	refs := ScanCSS(src)
	if len(refs) != 1 || refs[0].Line != 3 {
		t.Errorf("got %+v, want a single ref on line 3", refs)
	}
}

func TestScanCSSMarksImports(t *testing.T) {
	refs := ScanCSS(`@import url(a.css); .x { background: url(b.png); }`)
	if len(refs) != 2 {
		t.Fatalf("got %d refs", len(refs))
	}
	if !refs[0].Import {
		t.Error("the @import target was not marked as an import")
	}
	if refs[1].Import {
		t.Error("an ordinary url() was marked as an import")
	}
}

func TestParseSrcset(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"a.png", []string{"a.png"}},
		{"a.png 1x, b.png 2x", []string{"a.png", "b.png"}},
		{"  a.png   1x ,  b.png 640w ", []string{"a.png", "b.png"}},
		// Per the HTML srcset algorithm a candidate URL runs to the next
		// whitespace, so this is one (badly written) URL, not two. Browsers
		// agree, and matching them matters more than matching intuition.
		{"a.png,b.png", []string{"a.png,b.png"}},
		{"a.png, b.png", []string{"a.png", "b.png"}},
		// A data: URL contains commas, which is why splitting on "," is wrong.
		{"data:image/gif;base64,AAA= 1x, b.png 2x", []string{"data:image/gif;base64,AAA=", "b.png"}},
	}
	for _, c := range cases {
		if got := ParseSrcset(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseSrcset(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestClassifyRef(t *testing.T) {
	cases := []struct {
		in    string
		class RefClass
		path  string
		frag  string
	}{
		{"assets/a.png", RefLocal, "assets/a.png", ""},
		{"/assets/a.png", RefLocal, "/assets/a.png", ""},
		{"icons.svg#warning", RefLocal, "icons.svg", "warning"},
		{"f.woff2?v=2", RefLocal, "f.woff2", ""},
		{"assets/Revenue%20Chart%20%E2%80%93%20Europe.webp", RefLocal, "assets/Revenue Chart – Europe.webp", ""},
		{"https://example.com/x.png", RefRemote, "", ""},
		{"http://example.com/x.png", RefRemote, "", ""},
		{"//cdn.example.com/x.js", RefRemote, "", ""},
		{"file:///etc/passwd", RefRemote, "", ""},
		{"data:image/png;base64,AAA", RefIgnorable, "", ""},
		{"blob:https://x/y", RefIgnorable, "", ""},
		{"#slide-2", RefIgnorable, "", "slide-2"},
		{"mailto:a@b.c", RefIgnorable, "", ""},
		{"tel:+1234", RefIgnorable, "", ""},
		{"javascript:void(0)", RefIgnorable, "", ""},
		{"", RefEmpty, "", ""},
		{"   ", RefEmpty, "", ""},
	}
	for _, c := range cases {
		got := ClassifyRef(c.in)
		if got.Class != c.class {
			t.Errorf("ClassifyRef(%q).Class = %v, want %v", c.in, got.Class, c.class)
			continue
		}
		if c.class == RefLocal && got.Path != c.path {
			t.Errorf("ClassifyRef(%q).Path = %q, want %q", c.in, got.Path, c.path)
		}
		if c.frag != "" && got.Fragment != c.frag {
			t.Errorf("ClassifyRef(%q).Fragment = %q, want %q", c.in, got.Fragment, c.frag)
		}
	}
}

func TestClassifyRefTreatsDriveLetterAsANonPackageURL(t *testing.T) {
	// A single letter followed by ":" is a syntactically valid URL scheme, and
	// browsers parse "C:/x.png" that way rather than as a relative path. We
	// classify it the same, so it is reported as an unpackageable reference
	// instead of being silently looked up as the package path "C:/x.png".
	for _, in := range []string{"C:/Windows/x.png", `c:\temp\x.png`} {
		if got := ClassifyRef(in).Class; got != RefRemote {
			t.Errorf("ClassifyRef(%q).Class = %v, want RefRemote", in, got)
		}
	}
}

func htmlRefs(t *testing.T, src string) map[string]string {
	t.Helper()
	scan := ScanHTML([]byte(src))
	out := map[string]string{}
	for _, r := range scan.Refs {
		out[r.Detail] = r.Raw
	}
	return out
}

func TestScanHTMLFindsSubresources(t *testing.T) {
	src := `<!doctype html><html><head>
<title>  Deck Title </title>
<link rel="stylesheet" href="css/a.css">
<link rel="icon" href="favicon.png">
<link rel="canonical" href="https://example.com/page">
</head><body>
<img src="a.png" srcset="b.png 1x, c.png 2x">
<video src="v.mp4" poster="p.jpg"><source src="v.webm"><track src="t.vtt"></video>
<audio src="a.mp3"></audio>
<object data="o.pdf"></object><embed src="e.svg">
<svg><image href="i.png"/><image xlink:href="j.png"/><use href="icons.svg#x"/></svg>
<div style="background: url(bg.png)"></div>
<style>.z { background: url(inline.png) }</style>
<script src="app.js"></script>
<a href="https://example.com">link</a>
</body></html>`
	refs := htmlRefs(t, src)
	want := map[string]string{
		"link[rel=stylesheet][href]": "css/a.css",
		"link[rel=icon][href]":       "favicon.png",
		"img[src]":                   "a.png",
		"video[src]":                 "v.mp4",
		"video[poster]":              "p.jpg",
		"source[src]":                "v.webm",
		"track[src]":                 "t.vtt",
		"audio[src]":                 "a.mp3",
		"object[data]":               "o.pdf",
		"embed[src]":                 "e.svg",
		"script[src]":                "app.js",
	}
	for detail, raw := range want {
		if refs[detail] != raw {
			t.Errorf("missing or wrong ref %s: got %q, want %q", detail, refs[detail], raw)
		}
	}
	// srcset expands into individual candidates rather than one bogus path.
	scan := ScanHTML([]byte(src))
	var srcsets []string
	for _, r := range scan.Refs {
		if strings.Contains(r.Detail, "srcset") {
			srcsets = append(srcsets, r.Raw)
		}
	}
	if !reflect.DeepEqual(srcsets, []string{"b.png", "c.png"}) {
		t.Errorf("srcset candidates = %q, want [b.png c.png]", srcsets)
	}
	if scan.Title != "Deck Title" {
		t.Errorf("Title = %q, want %q", scan.Title, "Deck Title")
	}
	if len(scan.InlineCSS) != 2 {
		t.Errorf("expected inline <style> and style attribute, got %d", len(scan.InlineCSS))
	}
}

func TestScanHTMLSVGXlinkHref(t *testing.T) {
	scan := ScanHTML([]byte(`<svg><image xlink:href="a.png"/><use xlink:href="icons.svg#w"/></svg>`))
	var raws []string
	for _, r := range scan.Refs {
		raws = append(raws, r.Raw)
	}
	if len(raws) != 2 {
		t.Fatalf("got refs %q, want two", raws)
	}
}

func TestScanHTMLHyperlinksAreNotRenderingDependencies(t *testing.T) {
	scan := ScanHTML([]byte(`<a href="https://example.com">x</a><img src="https://example.com/i.png">`))
	var link, img Ref
	for _, r := range scan.Refs {
		switch r.Detail {
		case "a[href]":
			link = r
		case "img[src]":
			img = r
		}
	}
	if link.Context != CtxHyperlink {
		t.Error("a[href] should be a hyperlink context")
	}
	if img.Context != CtxRendering {
		t.Error("img[src] should be a rendering context")
	}
}

func issueCodes(issues []Issue) []diag.Code {
	var out []diag.Code
	for _, i := range issues {
		out = append(out, i.Code)
	}
	return out
}

func TestScanHTMLUnsupportedConstructs(t *testing.T) {
	cases := map[string]diag.Code{
		`<base href="https://example.com/">`:                 diag.BaseElement,
		`<script type="module" src="a.js"></script>`:         diag.ESModule,
		`<script type="importmap">{}</script>`:               diag.ImportMap,
		`<link rel="modulepreload" href="a.js">`:             diag.ESModule,
		`<iframe src="appendix.html"></iframe>`:              diag.LocalIframe,
		`<meta http-equiv="refresh" content="0;url=x.html">`: diag.MetaRefresh,
	}
	for src, want := range cases {
		got := issueCodes(ScanHTML([]byte(src)).Issues)
		found := false
		for _, c := range got {
			if c == want {
				found = true
			}
		}
		if !found {
			t.Errorf("ScanHTML(%q) issues = %v, want to include %v", src, got, want)
		}
	}
}

func TestScanHTMLDoesNotScanNonExecutableScriptAsJS(t *testing.T) {
	// A JSON data block that happens to contain the word "import" must not be
	// reported as an ES module.
	scan := ScanHTML([]byte(`<script type="application/json" id="d">{"import":"x"}</script>`))
	if len(scan.InlineJS) != 0 {
		t.Errorf("inert data script was scanned as JavaScript: %+v", scan.InlineJS)
	}
}

func TestScanJSDetectsUnsupportedPatterns(t *testing.T) {
	cases := map[string]diag.Code{
		`import { a } from "./a.js";`:            diag.ESModule,
		`import "./side-effect.js";`:             diag.ESModule,
		"export default function () {}":          diag.ESModule,
		`fetch("./data.json")`:                   diag.DynamicFetch,
		`new Worker("./w.js")`:                   diag.WebWorker,
		`importScripts("a.js")`:                  diag.WebWorker,
		`navigator.serviceWorker.register("sw")`: diag.ServiceWorker,
		`import("./lazy.js")`:                    diag.DynamicImport,
		`new XMLHttpRequest()`:                   diag.UnknownDynamic,
	}
	for src, want := range cases {
		got := issueCodes(ScanJS(src, "script"))
		found := false
		for _, c := range got {
			if c == want {
				found = true
			}
		}
		if !found {
			t.Errorf("ScanJS(%q) = %v, want to include %v", src, got, want)
		}
	}
}

func TestScanJSIgnoresCommentsAndStrings(t *testing.T) {
	src := `
// we used to fetch("./old.json") here
/* import { x } from "./y.js"; */
var note = "call fetch('./nope.json') to reload";
var other = 'export default 1';
var re = /fetch\(/;
`
	if issues := ScanJS(src, "script"); len(issues) != 0 {
		t.Errorf("ScanJS reported %d issue(s) for comment/string content: %+v", len(issues), issues)
	}
}

func TestScanJSRemoteFetchIsOnlyAWarning(t *testing.T) {
	issues := ScanJS(`fetch("https://api.example.com/data")`, "script")
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1", len(issues))
	}
	if !issues[0].Warning {
		t.Error("a remote fetch should warn, not fail; it is not a package path problem")
	}
}

func TestScanJSLineNumbers(t *testing.T) {
	issues := ScanJS("var a = 1;\nvar b = 2;\nfetch(\"./x.json\");\n", "script")
	if len(issues) != 1 || issues[0].Line != 3 {
		t.Errorf("got %+v, want one issue on line 3", issues)
	}
}
