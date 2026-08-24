package validate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pwagstro/slidepack/internal/diag"
	"github.com/pwagstro/slidepack/internal/source"
)

func fixture(t *testing.T, name string) source.Tree {
	t.Helper()
	root := filepath.Join("..", "..", "testdata", name)
	tree, err := source.LoadDiskTree(root)
	if err != nil {
		t.Fatalf("loading %s: %v", root, err)
	}
	return tree
}

func codes(ds []diag.Diagnostic) map[diag.Code]int {
	out := map[diag.Code]int{}
	for _, d := range ds {
		out[d.Code]++
	}
	return out
}

func TestValidFixturesPass(t *testing.T) {
	for _, name := range []string{"basic", "nested", "browser"} {
		t.Run(name, func(t *testing.T) {
			res := Tree(fixture(t, name), Options{})
			if !res.Valid {
				t.Errorf("%s should be valid, got errors: %+v", name, res.Errors)
			}
		})
	}
}

func TestInvalidFixturesProduceTheDocumentedCodes(t *testing.T) {
	// These code names are part of the tool's public contract; a change here
	// breaks every agent and CI script that matches on them.
	cases := map[string]diag.Code{
		"invalid/missing-resource":   diag.MissingResource,
		"invalid/remote-resource":    diag.RemoteResource,
		"invalid/remote-css":         diag.RemoteResource,
		"invalid/module-script":      diag.ESModule,
		"invalid/base-element":       diag.BaseElement,
		"invalid/import-map":         diag.ImportMap,
		"invalid/service-worker":     diag.ServiceWorker,
		"invalid/local-iframe":       diag.LocalIframe,
		"invalid/dynamic-fetch":      diag.DynamicFetch,
		"invalid/no-entrypoint":      diag.MissingEntrypoint,
		"invalid/escaping-reference": diag.EscapingRef,
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			res := Tree(fixture(t, name), Options{})
			if res.Valid {
				t.Fatalf("%s should not be valid", name)
			}
			if codes(res.Errors)[want] == 0 {
				t.Errorf("%s: want error %s, got %v", name, want, codes(res.Errors))
			}
		})
	}
}

func TestExternalHyperlinksAreAllowed(t *testing.T) {
	// The remote-resource fixture also contains a plain <a href="https://...">.
	// That must not contribute an error of its own.
	res := Tree(fixture(t, "invalid/remote-resource"), Options{})
	for _, d := range res.Errors {
		if d.Detail == "a[href]" {
			t.Errorf("an ordinary external hyperlink was reported: %+v", d)
		}
	}
}

func TestAlternateEntrypoint(t *testing.T) {
	tree := fixture(t, "invalid/no-entrypoint")
	if res := Tree(tree, Options{}); res.Valid {
		t.Fatal("index.html is absent, so the default entrypoint must fail")
	}
	if res := Tree(tree, Options{Entrypoint: "slides.html"}); !res.Valid {
		t.Errorf("--entry slides.html should validate, got %+v", res.Errors)
	}
}

// mem builds an in-memory source tree from a path -> content map.
func mem(files map[string]string) *source.MemTree {
	t := source.NewMemTree()
	for p, c := range files {
		t.Add(p, 0o644, []byte(c))
	}
	t.Sort()
	return t
}

func TestEmptySourceIsReported(t *testing.T) {
	res := Tree(source.NewMemTree(), Options{})
	if res.Valid || codes(res.Errors)[diag.EmptySource] == 0 {
		t.Errorf("want EMPTY_SOURCE, got %+v", res.Errors)
	}
}

func TestNonHTMLEntrypointIsReported(t *testing.T) {
	tree := mem(map[string]string{"index.html": "<p>ok</p>", "deck.txt": "x"})
	res := Tree(tree, Options{Entrypoint: "deck.txt"})
	if codes(res.Errors)[diag.InvalidEntrypoint] == 0 {
		t.Errorf("want INVALID_ENTRYPOINT, got %+v", res.Errors)
	}
}

func TestCSSReferencesResolveRelativeToTheStylesheet(t *testing.T) {
	ok := mem(map[string]string{
		"index.html":      `<link rel="stylesheet" href="css/theme/a.css">`,
		"css/theme/a.css": `.x { background: url(../../assets/bg.png) }`,
		"assets/bg.png":   "png",
	})
	if res := Tree(ok, Options{}); !res.Valid {
		t.Errorf("nested stylesheet reference should resolve, got %+v", res.Errors)
	}

	// The same reference resolved against the document instead of the
	// stylesheet would look for "assets/bg.png" from the wrong depth.
	bad := mem(map[string]string{
		"index.html":      `<link rel="stylesheet" href="css/theme/a.css">`,
		"css/theme/a.css": `.x { background: url(../assets/bg.png) }`,
		"assets/bg.png":   "png",
	})
	res := Tree(bad, Options{})
	if res.Valid {
		t.Error("a stylesheet reference pointing at a nonexistent file should fail")
	}
	if codes(res.Errors)[diag.MissingResource] == 0 {
		t.Errorf("want MISSING_RESOURCE, got %v", codes(res.Errors))
	}
}

func TestCSSImportIsSupported(t *testing.T) {
	tree := mem(map[string]string{
		"index.html":           `<link rel="stylesheet" href="css/main.css">`,
		"css/main.css":         `@import url("shared/reset.css");`,
		"css/shared/reset.css": `body { margin: 0 }`,
	})
	if res := Tree(tree, Options{}); !res.Valid {
		t.Errorf("@import of a packaged stylesheet should validate, got %+v", res.Errors)
	}

	missing := mem(map[string]string{
		"index.html":   `<link rel="stylesheet" href="css/main.css">`,
		"css/main.css": `@import "absent.css";`,
	})
	res := Tree(missing, Options{})
	if res.Valid || codes(res.Errors)[diag.MissingResource] == 0 {
		t.Errorf("a missing @import target should fail, got %+v", res.Errors)
	}
}

func TestRootRelativePathsResolveAgainstThePackage(t *testing.T) {
	tree := mem(map[string]string{
		"index.html":      `<img src="/assets/logo.svg">`,
		"assets/logo.svg": "<svg/>",
	})
	if res := Tree(tree, Options{}); !res.Valid {
		t.Errorf("a root-relative reference should resolve, got %+v", res.Errors)
	}
}

func TestPercentEncodedUnicodePathsResolve(t *testing.T) {
	tree := source.NewMemTree()
	tree.Add("index.html", 0o644, []byte(`<img src="assets/Revenue%20Chart%20%E2%80%93%20Europe.webp">`))
	tree.Add("assets/Revenue Chart – Europe.webp", 0o644, []byte("webp"))
	tree.Sort()
	if res := Tree(tree, Options{}); !res.Valid {
		t.Errorf("percent-encoded unicode path should resolve, got %+v", res.Errors)
	}
}

func TestLocalNavigationLinkIsOnlyAWarning(t *testing.T) {
	tree := mem(map[string]string{
		"index.html":    `<a href="appendix.html">Appendix</a>`,
		"appendix.html": `<p>Appendix</p>`,
	})
	res := Tree(tree, Options{})
	if !res.Valid {
		t.Errorf("a local navigation link must not fail validation: %+v", res.Errors)
	}
	if codes(res.Warnings)[diag.LocalNavLink] == 0 {
		t.Errorf("want a LOCAL_NAVIGATION_LINK warning, got %v", codes(res.Warnings))
	}
}

func TestFragmentAndDataURLsAreNotResources(t *testing.T) {
	tree := mem(map[string]string{
		"index.html": `<a href="#slide-2">Next</a>
			<img src="data:image/png;base64,AAAA">
			<a href="mailto:a@b.c">Mail</a>
			<a href="tel:+1">Call</a>
			<img src="blob:https://example.com/x">`,
	})
	if res := Tree(tree, Options{}); !res.Valid {
		t.Errorf("non-package URL schemes must not be treated as resources: %+v", res.Errors)
	}
}

func TestDiagnosticsAreSortedStably(t *testing.T) {
	tree := mem(map[string]string{
		"index.html": "<img src=\"b.png\">\n<img src=\"a.png\">\n<base href=\"x\">",
	})
	first := Tree(tree, Options{})
	second := Tree(tree, Options{})
	if len(first.Errors) != len(second.Errors) {
		t.Fatal("validation is not deterministic")
	}
	for i := range first.Errors {
		if first.Errors[i] != second.Errors[i] {
			t.Fatalf("diagnostic %d differs between runs", i)
		}
	}
	// Sorted by line, so the first error is the one on line 1.
	if first.Errors[0].Line > first.Errors[len(first.Errors)-1].Line {
		t.Error("diagnostics are not sorted by line")
	}
}

func TestSymlinkInSourceTreeIsRejected(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<p>x</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("index.html", filepath.Join(root, "alias.html")); err != nil {
		t.Skipf("this platform cannot create symlinks: %v", err)
	}
	_, err := source.LoadDiskTree(root)
	if err == nil {
		t.Fatal("LoadDiskTree accepted a tree containing a symlink")
	}
	var sf *source.ErrSpecialFile
	if !asError(err, &sf) {
		t.Fatalf("want ErrSpecialFile, got %T: %v", err, err)
	}
	if sf.Path != "alias.html" {
		t.Errorf("error names %q, want alias.html", sf.Path)
	}
}

func asError[T error](err error, target *T) bool {
	for err != nil {
		if v, ok := err.(T); ok {
			*target = v
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
