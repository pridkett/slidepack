// Package validate checks that a presentation obeys the format v1 source
// contract, and that a packed document is internally consistent.
//
// Nothing in this package executes presentation code. HTML is tokenized, CSS is
// scanned lexically and JavaScript is only pattern-matched; no JavaScript
// engine is involved at any point.
package validate

import (
	"errors"
	"fmt"
	"strings"

	"github.com/pridkett/slidepack/internal/diag"
	"github.com/pridkett/slidepack/internal/mimes"
	"github.com/pridkett/slidepack/internal/pathutil"
	"github.com/pridkett/slidepack/internal/source"
)

// Options tunes a source validation run.
type Options struct {
	// Entrypoint is the package path of the document to render.
	Entrypoint string
}

// Tree validates a source tree against the format v1 contract.
func Tree(t source.Tree, opts Options) *diag.Result {
	res := diag.NewResult("", "source")
	entry := opts.Entrypoint
	if entry == "" {
		entry = source.DefaultEntrypoint
	}

	entries := t.Entries()
	if len(entries) == 0 {
		res.Errorf(diag.EmptySource, "", 0, "", "the source directory contains no files")
		return res
	}

	// Path legality first: everything downstream assumes canonical paths.
	for _, e := range entries {
		if err := pathutil.Check(e.Path); err != nil {
			res.Errorf(diag.InvalidPath, e.Path, 0, "", "%v", err)
			continue
		}
		if err := pathutil.CheckUSTAR(e.Path); err != nil {
			res.Errorf(diag.PathTooLong, e.Path, 0, "", "%v", err)
		}
	}

	if !t.Has(entry) {
		res.Errorf(diag.MissingEntrypoint, entry, 0, "",
			"entrypoint %q does not exist in the source tree; create it or pass --entry", entry)
		res.Sort()
		return res
	}
	if !mimes.IsHTML(entry) {
		res.Errorf(diag.InvalidEntrypoint, entry, 0, "",
			"entrypoint %q is not an HTML document; the entrypoint must be .html or .htm", entry)
	}

	for _, e := range entries {
		switch {
		case mimes.IsHTML(e.Path):
			validateHTMLFile(res, t, e.Path)
		case mimes.IsCSS(e.Path):
			validateCSSFile(res, t, e.Path)
		case mimes.IsJS(e.Path):
			data, err := t.Read(e.Path)
			if err != nil {
				res.Errorf(diag.Unreadable, e.Path, 0, "", "cannot read file: %v", err)
				continue
			}
			addIssues(res, e.Path, source.ScanJS(string(data), "script"))
		}
	}

	res.Sort()
	return res
}

func validateHTMLFile(res *diag.Result, t source.Tree, p string) {
	data, err := t.Read(p)
	if err != nil {
		res.Errorf(diag.Unreadable, p, 0, "", "cannot read file: %v", err)
		return
	}
	scan := source.ScanHTML(data)
	addIssues(res, p, scan.Issues)
	for _, ref := range scan.Refs {
		checkRef(res, t, p, ref)
	}
	for _, css := range scan.InlineCSS {
		// Inline CSS resolves relative to the containing document, exactly as
		// a browser would resolve it.
		checkCSSText(res, t, p, css.Text, css.Line, css.Detail)
	}
	for _, js := range scan.InlineJS {
		issues := source.ScanJS(js.Text, js.Detail)
		for i := range issues {
			// ScanJS numbers lines within the fragment; shift into document space.
			issues[i].Line += js.Line - 1
		}
		addIssues(res, p, issues)
	}
}

func validateCSSFile(res *diag.Result, t source.Tree, p string) {
	data, err := t.Read(p)
	if err != nil {
		res.Errorf(diag.Unreadable, p, 0, "", "cannot read file: %v", err)
		return
	}
	checkCSSText(res, t, p, string(data), 0, "css url()")
}

func checkCSSText(res *diag.Result, t source.Tree, base, text string, lineOffset int, detail string) {
	for _, r := range source.ScanCSS(text) {
		d := detail
		if r.Import {
			d = "@import"
		}
		line := r.Line
		if lineOffset > 0 {
			line += lineOffset - 1
		}
		checkRef(res, t, base, source.Ref{Raw: r.Value, Detail: d, Line: line, Context: source.CtxRendering})
	}
}

// checkRef resolves one reference and records whatever is wrong with it.
func checkRef(res *diag.Result, t source.Tree, base string, ref source.Ref) {
	parsed := source.ClassifyRef(ref.Raw)
	switch parsed.Class {
	case source.RefEmpty, source.RefIgnorable:
		return
	case source.RefRemote:
		if ref.Context == source.CtxHyperlink {
			// A link the user may click is not a rendering dependency.
			return
		}
		res.Errorf(diag.RemoteResource, base, ref.Line, ref.Detail,
			"loads %q over the network; a packed presentation must render offline, so add the resource to the source tree and reference it by path", strings.TrimSpace(ref.Raw))
		return
	}

	resolved, ok := pathutil.ResolveRef(base, parsed.Path)
	if !ok {
		res.Errorf(diag.EscapingRef, base, ref.Line, ref.Detail,
			"references %q, which resolves outside the presentation directory", strings.TrimSpace(ref.Raw))
		return
	}

	if ref.Context == source.CtxHyperlink {
		if mimes.IsHTML(resolved) {
			res.Warnf(diag.LocalNavLink, base, ref.Line, ref.Detail,
				"links to the package-local document %q; format v1 renders a single entrypoint, so this link will not navigate. Use a #fragment within the presentation instead.", resolved)
		}
		return
	}

	if t.Has(resolved) {
		return
	}
	// Fall back to the undecoded spelling, for the rare source tree whose file
	// names contain literal percent signs.
	if alt, ok2 := pathutil.ResolveRef(base, parsed.RawPath); ok2 && t.Has(alt) {
		return
	}
	res.Errorf(diag.MissingResource, base, ref.Line, ref.Detail,
		"references %q, which resolves to %q, but no such file exists in the source tree", strings.TrimSpace(ref.Raw), resolved)
}

func addIssues(res *diag.Result, p string, issues []source.Issue) {
	for _, is := range issues {
		if is.Warning {
			res.Warnf(is.Code, p, is.Line, is.Detail, "%s", is.Message)
		} else {
			res.Errorf(is.Code, p, is.Line, is.Detail, "%s", is.Message)
		}
	}
}

// ErrInvalid is returned by helpers that turn a Result into an error.
var ErrInvalid = errors.New("presentation is not valid")

// Summarize renders a one-line summary of a result.
func Summarize(r *diag.Result) string {
	return fmt.Sprintf("%d error(s), %d warning(s)", len(r.Errors), len(r.Warnings))
}
