package source

import (
	"bytes"
	"strings"

	"golang.org/x/net/html"

	"github.com/pwagstro/slidepack/internal/diag"
)

// RefContext distinguishes references the browser must resolve in order to
// render the page from references it only follows when the user clicks.
type RefContext int

const (
	// CtxRendering is a subresource: the page is broken without it.
	CtxRendering RefContext = iota
	// CtxHyperlink is navigation. A remote hyperlink is perfectly fine.
	CtxHyperlink
)

// Ref is a URL reference found in a document.
type Ref struct {
	Raw     string
	Detail  string
	Line    int
	Context RefContext
}

// Issue is a construct-level finding that is not about a URL.
type Issue struct {
	Code    diag.Code
	Line    int
	Detail  string
	Message string
	Warning bool
}

// HTMLScan is everything the validator needs from one HTML document.
type HTMLScan struct {
	Refs      []Ref
	InlineCSS []InlineText
	InlineJS  []InlineText
	Issues    []Issue
	Title     string
}

// InlineText is embedded CSS or JavaScript together with where it started.
type InlineText struct {
	Text   string
	Line   int
	Detail string
}

// renderingAttrs lists element/attribute pairs whose value is a subresource.
var renderingAttrs = map[string][]string{
	"img":    {"src", "srcset"},
	"source": {"src", "srcset"},
	"video":  {"src", "poster"},
	"audio":  {"src"},
	"track":  {"src"},
	"object": {"data"},
	"embed":  {"src"},
	"input":  {"src"},
	"image":  {"href", "xlink:href"},
	"use":    {"href", "xlink:href"},
	"body":   {"background"},
	"table":  {"background"},
	"td":     {"background"},
	"th":     {"background"},
}

// linkRelResources are rel values whose href is loaded rather than navigated to.
var linkRelResources = map[string]bool{
	"stylesheet":                true,
	"icon":                      true,
	"shortcut":                  true,
	"apple-touch-icon":          true,
	"apple-touch-startup-image": true,
	"mask-icon":                 true,
	"preload":                   true,
	"prefetch":                  true,
	"manifest":                  true,
	"prerender":                 true,
}

// ScanHTML extracts references, inline CSS/JS and unsupported constructs.
//
// A tokenizer is used rather than a full tree parse because we care about what
// is written in the document, not about the tree the browser would build from
// it; a token stream also keeps line numbers honest for error messages.
// Nothing here executes or evaluates any part of the document.
func ScanHTML(src []byte) *HTMLScan {
	scan := &HTMLScan{}
	z := html.NewTokenizer(bytes.NewReader(src))
	line := 1

	// State carried across tokens for raw-text elements.
	var (
		inScript     bool
		scriptIsData bool
		scriptLine   int
		inStyle      bool
		styleLine    int
		inTitle      bool
		titleBuf     strings.Builder
	)

	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			// io.EOF, or malformed markup the tokenizer recovered from by
			// stopping. Either way there is nothing further to inspect.
			return scan
		}
		raw := z.Raw()
		startLine := line
		line += bytes.Count(raw, []byte{'\n'})

		switch tt {
		case html.TextToken:
			switch {
			case inScript:
				if !scriptIsData {
					scan.InlineJS = append(scan.InlineJS, InlineText{Text: string(raw), Line: scriptLine, Detail: "inline <script>"})
				}
			case inStyle:
				scan.InlineCSS = append(scan.InlineCSS, InlineText{Text: string(raw), Line: styleLine, Detail: "inline <style>"})
			case inTitle:
				titleBuf.Write(raw)
			}

		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			tag := string(name)
			attrs := map[string]string{}
			order := []string{}
			for hasAttr {
				var k, v []byte
				k, v, hasAttr = z.TagAttr()
				key := string(k)
				if _, seen := attrs[key]; !seen {
					attrs[key] = string(v)
					order = append(order, key)
				}
			}
			scanTag(scan, tag, attrs, order, startLine)

			if tt == html.StartTagToken {
				switch tag {
				case "script":
					inScript = true
					scriptLine = startLine
					scriptIsData = !isExecutableScriptType(attrs["type"])
				case "style":
					inStyle = true
					styleLine = startLine
				case "title":
					inTitle = true
				}
			}

		case html.EndTagToken:
			name, _ := z.TagName()
			switch string(name) {
			case "script":
				inScript = false
			case "style":
				inStyle = false
			case "title":
				if inTitle && scan.Title == "" {
					scan.Title = strings.TrimSpace(titleBuf.String())
				}
				inTitle = false
			}
		}
	}
}

func scanTag(scan *HTMLScan, tag string, attrs map[string]string, order []string, line int) {
	add := func(raw, detail string, ctx RefContext) {
		if strings.TrimSpace(raw) == "" {
			return
		}
		scan.Refs = append(scan.Refs, Ref{Raw: raw, Detail: detail, Line: line, Context: ctx})
	}
	// A srcset attribute holds several candidates, each of which is its own
	// reference; treating the whole attribute as one URL would report the
	// descriptors ("2x", "640w") as part of a file name.
	addSrcset := func(raw, detail string) {
		for _, cand := range ParseSrcset(raw) {
			add(cand, detail, CtxRendering)
		}
	}
	issue := func(code diag.Code, detail, msg string, warn bool) {
		scan.Issues = append(scan.Issues, Issue{Code: code, Line: line, Detail: detail, Message: msg, Warning: warn})
	}

	switch tag {
	case "base":
		issue(diag.BaseElement, "base", "the <base> element makes package-relative resource resolution ambiguous and is not supported by format v1; remove it and use paths relative to the document", false)

	case "script":
		typ := strings.ToLower(strings.TrimSpace(attrs["type"]))
		switch typ {
		case "module":
			issue(diag.ESModule, "script[type=module]", "ES modules are not supported by format v1; bundle the module graph into a single classic script and load it with <script src=...>", false)
		case "importmap":
			issue(diag.ImportMap, "script[type=importmap]", "import maps are not supported by format v1; bundle your JavaScript instead", false)
		}
		if _, ok := attrs["nomodule"]; ok && typ == "" {
			// nomodule on a classic script is harmless; nothing to report.
			_ = ok
		}
		if src, ok := attrs["src"]; ok && typ != "module" && typ != "importmap" {
			add(src, "script[src]", CtxRendering)
		}

	case "link":
		rel := strings.ToLower(strings.TrimSpace(attrs["rel"]))
		rels := strings.Fields(rel)
		isResource := false
		for _, r := range rels {
			if r == "modulepreload" {
				issue(diag.ESModule, "link[rel=modulepreload]", "modulepreload implies an ES module graph, which format v1 does not support", false)
			}
			if linkRelResources[r] {
				isResource = true
			}
		}
		href, has := attrs["href"]
		if has {
			if isResource {
				add(href, "link[rel="+rel+"][href]", CtxRendering)
			} else {
				add(href, "link[href]", CtxHyperlink)
			}
		}
		if imgsrcset, ok := attrs["imagesrcset"]; ok {
			addSrcset(imgsrcset, "link[imagesrcset]")
		}

	case "iframe", "frame":
		if src, ok := attrs["src"]; ok {
			p := ClassifyRef(src)
			if p.Class == RefLocal {
				issue(diag.LocalIframe, tag+"[src]", "embedding a package-local document in an <"+tag+"> is not supported by format v1; inline the content instead", false)
			} else {
				add(src, tag+"[src]", CtxHyperlink)
			}
		}

	case "meta":
		if strings.EqualFold(strings.TrimSpace(attrs["http-equiv"]), "refresh") {
			issue(diag.MetaRefresh, "meta[http-equiv=refresh]", "a refresh/redirect meta tag will not resolve inside a packed presentation", true)
		}
		if strings.EqualFold(strings.TrimSpace(attrs["property"]), "og:image") {
			// Social metadata is never fetched when viewing a local file.
			if c, ok := attrs["content"]; ok {
				_ = c
			}
		}

	case "a", "area":
		if href, ok := attrs["href"]; ok {
			add(href, tag+"[href]", CtxHyperlink)
		}

	case "form":
		if action, ok := attrs["action"]; ok {
			add(action, "form[action]", CtxHyperlink)
		}
	}

	// Generic subresource attributes.
	if list, ok := renderingAttrs[tag]; ok {
		for _, a := range list {
			v, present := attrs[a]
			if !present {
				continue
			}
			if a == "srcset" || a == "imagesrcset" {
				addSrcset(v, tag+"["+a+"]")
				continue
			}
			add(v, tag+"["+a+"]", CtxRendering)
		}
	}

	// The style attribute is a declaration list, not a full stylesheet, but the
	// same url() rules apply.
	if v, ok := attrs["style"]; ok && strings.TrimSpace(v) != "" {
		scan.InlineCSS = append(scan.InlineCSS, InlineText{Text: v, Line: line, Detail: tag + "[style]"})
	}
	_ = order
}

// isExecutableScriptType reports whether a script type attribute names a
// classic script the browser will run. Anything else (application/json,
// text/template, ...) is inert data and must not be scanned as JavaScript.
func isExecutableScriptType(t string) bool {
	t = strings.ToLower(strings.TrimSpace(t))
	if i := strings.IndexByte(t, ';'); i >= 0 {
		t = strings.TrimSpace(t[:i])
	}
	switch t {
	case "", "text/javascript", "application/javascript", "module",
		"text/ecmascript", "application/ecmascript", "text/jscript":
		return true
	}
	return false
}
