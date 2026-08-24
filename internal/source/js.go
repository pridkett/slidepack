package source

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/pridkett/slidepack/internal/diag"
)

// maskJS returns a copy of src, the same length, in which the bodies of
// comments and string/template literals have been replaced by spaces.
//
// Masking is what makes the pattern matching below trustworthy: without it a
// comment saying "we used to fetch('./data.json')" or a string containing the
// word "import" would produce a false error. Newlines are preserved so byte
// offsets, and therefore line numbers, stay aligned with the original.
//
// This is a lexical approximation, not a JavaScript parser. It is deliberately
// conservative and is documented as such: format v1 asks presentations not to
// resolve package paths at runtime, and this check catches the obvious cases.
func maskJS(src string) string {
	out := []byte(src)
	blank := func(i int) {
		if out[i] != '\n' {
			out[i] = ' '
		}
	}
	n := len(src)
	i := 0
	// prevSignificant tracks the last non-space character, which is how a
	// division operator is told apart from the start of a regex literal.
	prevSignificant := byte(0)

	for i < n {
		c := src[i]
		switch {
		case c == '/' && i+1 < n && src[i+1] == '/':
			for i < n && src[i] != '\n' {
				blank(i)
				i++
			}
		case c == '/' && i+1 < n && src[i+1] == '*':
			blank(i)
			blank(i + 1)
			i += 2
			for i < n && !(src[i] == '*' && i+1 < n && src[i+1] == '/') {
				blank(i)
				i++
			}
			if i < n {
				blank(i)
				if i+1 < n {
					blank(i + 1)
				}
				i += 2
			}
		case c == '"' || c == '\'' || c == '`':
			quote := c
			i++
			for i < n {
				if src[i] == '\\' && i+1 < n {
					blank(i)
					blank(i + 1)
					i += 2
					continue
				}
				if src[i] == quote {
					i++
					break
				}
				blank(i)
				i++
			}
			prevSignificant = quote
		case c == '/' && regexAllowedAfter(prevSignificant):
			// Regex literal: skip it so that a "/" inside cannot be mistaken
			// for the start of a comment on the next pass.
			i++
			for i < n && src[i] != '\n' {
				if src[i] == '\\' && i+1 < n {
					blank(i)
					blank(i + 1)
					i += 2
					continue
				}
				if src[i] == '[' {
					for i < n && src[i] != ']' && src[i] != '\n' {
						blank(i)
						i++
					}
					continue
				}
				if src[i] == '/' {
					i++
					break
				}
				blank(i)
				i++
			}
			prevSignificant = '/'
		default:
			if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
				prevSignificant = c
			}
			i++
		}
	}
	return string(out)
}

// regexAllowedAfter reports whether a "/" following this character begins a
// regular expression literal rather than a division.
func regexAllowedAfter(prev byte) bool {
	switch prev {
	case 0, '(', ',', '=', ':', '[', '!', '&', '|', '?', '{', '}', ';', '+', '-', '*', '~', '^', '%', '<', '>', 'n' /* return/in */ :
		return true
	}
	return false
}

var (
	reStaticImport = regexp.MustCompile(`(?m)^[ \t]*import[ \t]+(?:[^\n(]*?from[ \t]*["'` + "`" + `]|["'` + "`" + `])`)
	reBareImport   = regexp.MustCompile(`(?m)^[ \t]*import[ \t]*\{`)
	reExport       = regexp.MustCompile(`(?m)^[ \t]*export[ \t]+(?:default|const|let|var|function|class|\{|\*)`)
	reDynImport    = regexp.MustCompile(`\bimport[ \t]*\(`)
	reFetch        = regexp.MustCompile(`\bfetch[ \t]*\(`)
	reWorker       = regexp.MustCompile(`\bnew[ \t]+(Shared)?Worker[ \t]*\(`)
	reImportScript = regexp.MustCompile(`\bimportScripts[ \t]*\(`)
	reSW           = regexp.MustCompile(`navigator[ \t]*\.[ \t]*serviceWorker`)
	reXHR          = regexp.MustCompile(`\bnew[ \t]+XMLHttpRequest[ \t]*\(`)
	reSocket       = regexp.MustCompile(`\bnew[ \t]+(WebSocket|EventSource)[ \t]*\(`)
	reStringArg    = regexp.MustCompile(`^[ \t]*(["'` + "`" + `])([^"'` + "`" + `\n]*)`)
)

// ScanJS looks for resource-loading constructs that format v1 cannot serve.
func ScanJS(src string, detail string) []Issue {
	masked := maskJS(src)
	var issues []Issue
	at := func(off int) int { return 1 + strings.Count(src[:off], "\n") }

	report := func(code diag.Code, off int, msg string, warn bool) {
		issues = append(issues, Issue{Code: code, Line: at(off), Detail: detail, Message: msg, Warning: warn})
	}

	for _, m := range reStaticImport.FindAllStringIndex(masked, -1) {
		report(diag.ESModule, m[0], "static ES module import; format v1 loads classic scripts only, so bundle the module graph before packing", false)
	}
	for _, m := range reBareImport.FindAllStringIndex(masked, -1) {
		report(diag.ESModule, m[0], "static ES module import; format v1 loads classic scripts only, so bundle the module graph before packing", false)
	}
	for _, m := range reExport.FindAllStringIndex(masked, -1) {
		report(diag.ESModule, m[0], "ES module export; a classic script cannot contain export declarations", false)
	}
	for _, m := range reDynImport.FindAllStringIndex(masked, -1) {
		report(diag.DynamicImport, m[0], "dynamic import(); format v1 cannot resolve package paths at runtime", true)
	}
	for _, m := range reWorker.FindAllStringIndex(masked, -1) {
		report(diag.WebWorker, m[0], "Worker construction; workers load their script from a URL that format v1 cannot provide", false)
	}
	for _, m := range reImportScript.FindAllStringIndex(masked, -1) {
		report(diag.WebWorker, m[0], "importScripts(); worker script loading is outside the format v1 resource model", false)
	}
	for _, m := range reSW.FindAllStringIndex(masked, -1) {
		report(diag.ServiceWorker, m[0], "service worker registration; service workers cannot be registered from a file:// document and are not supported", false)
	}
	for _, m := range reXHR.FindAllStringIndex(masked, -1) {
		report(diag.UnknownDynamic, m[0], "XMLHttpRequest; requests for package-local paths will not resolve at runtime", true)
	}
	for _, m := range reSocket.FindAllStringIndex(masked, -1) {
		report(diag.UnknownDynamic, m[0], "network connection constructor; a packed presentation must render without the network", true)
	}

	// fetch() is classified by its literal first argument when there is one.
	for _, m := range reFetch.FindAllStringIndex(masked, -1) {
		argOff := m[1]
		lit := reStringArg.FindStringSubmatch(safeSlice(src, argOff, argOff+512))
		if lit == nil {
			report(diag.UnknownDynamic, m[0], "fetch() with a computed URL; if it resolves a package-local path it will fail at runtime", true)
			continue
		}
		p := ClassifyRef(lit[2])
		switch p.Class {
		case RefLocal:
			report(diag.DynamicFetch, m[0], fmt.Sprintf("fetch(%q) requests a package-local path; format v1 serves resources as blob URLs, so runtime fetches of source paths cannot resolve. Inline the data into the script or embed it as a data: URL.", lit[2]), false)
		case RefRemote:
			report(diag.UnknownDynamic, m[0], fmt.Sprintf("fetch(%q) is a network request; a packed presentation must render without the network", lit[2]), true)
		}
	}
	return issues
}

func safeSlice(s string, a, b int) string {
	if a > len(s) {
		return ""
	}
	if b > len(s) {
		b = len(s)
	}
	return s[a:b]
}
