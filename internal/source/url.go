package source

import (
	"net/url"
	"strings"
)

// RefClass says how a reference should be treated by the packer.
type RefClass int

const (
	// RefLocal is a package-relative or package-root-relative path.
	RefLocal RefClass = iota
	// RefRemote requires the network to resolve.
	RefRemote
	// RefIgnorable resolves without the package and without the network:
	// data:, blob:, fragment-only, mailto:, tel:, javascript: and friends.
	RefIgnorable
	// RefEmpty is an absent or blank reference.
	RefEmpty
)

// ignorableSchemes never denote a packaged resource and never need the network.
var ignorableSchemes = map[string]bool{
	"data":       true,
	"blob":       true,
	"javascript": true,
	"mailto":     true,
	"tel":        true,
	"sms":        true,
	"about":      true,
	"geo":        true,
	"cid":        true,
}

// ParsedRef is a reference broken into the parts the resolver needs.
type ParsedRef struct {
	Class RefClass
	// Path is the percent-decoded, query- and fragment-free portion. Only
	// meaningful when Class is RefLocal.
	Path string
	// RawPath is the same portion before percent-decoding, used as a fallback
	// for the (unusual) source tree whose file names contain literal % signs.
	RawPath string
	// Fragment is the "#..." suffix without the hash, preserved because SVG
	// sprite references such as icons.svg#warning depend on it.
	Fragment string
	// Query is the "?..." suffix without the question mark. Blob URLs cannot
	// carry one, so it is dropped when rewriting, but it is kept here so
	// diagnostics can echo what the author wrote.
	Query string
}

// ClassifyRef splits and classifies a raw reference taken from HTML or CSS.
func ClassifyRef(raw string) ParsedRef {
	s := strings.TrimSpace(raw)
	// HTML attribute values may contain stray newlines and tabs; the URL parser
	// strips them, so do the same before looking for a scheme.
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return -1
		}
		return r
	}, s)

	if s == "" {
		return ParsedRef{Class: RefEmpty}
	}
	if strings.HasPrefix(s, "#") {
		return ParsedRef{Class: RefIgnorable, Fragment: s[1:]}
	}
	// Protocol-relative: //cdn.example.com/x.js inherits the page scheme, which
	// from file:// is meaningless, but it is unmistakably an off-package
	// dependency.
	if strings.HasPrefix(s, "//") {
		return ParsedRef{Class: RefRemote}
	}
	if scheme, ok := splitScheme(s); ok {
		if ignorableSchemes[scheme] {
			return ParsedRef{Class: RefIgnorable}
		}
		return ParsedRef{Class: RefRemote}
	}

	rest := s
	var frag, query string
	if i := strings.IndexByte(rest, '#'); i >= 0 {
		frag = rest[i+1:]
		rest = rest[:i]
	}
	if i := strings.IndexByte(rest, '?'); i >= 0 {
		query = rest[i+1:]
		rest = rest[:i]
	}
	if rest == "" {
		// e.g. "?x=1" or a bare "#frag" already handled above.
		return ParsedRef{Class: RefIgnorable, Fragment: frag, Query: query}
	}
	decoded, err := url.PathUnescape(rest)
	if err != nil {
		decoded = rest
	}
	return ParsedRef{Class: RefLocal, Path: decoded, RawPath: rest, Fragment: frag, Query: query}
}

// splitScheme reports the URL scheme of s, if it has one.
//
// A scheme is [a-zA-Z][a-zA-Z0-9+.-]* followed by ":". The leading-letter rule
// is what stops "C:/x" from parsing as a scheme in a browser, and it is also
// what keeps a Windows-style path from being mistaken for one here.
func splitScheme(s string) (string, bool) {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == ':':
			if i == 0 {
				return "", false
			}
			return strings.ToLower(s[:i]), true
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
			continue
		case c >= '0' && c <= '9', c == '+', c == '.', c == '-':
			if i == 0 {
				return "", false
			}
			continue
		default:
			return "", false
		}
	}
	return "", false
}

// ParseSrcset splits a srcset attribute into its candidate URLs.
//
// This follows the HTML "parse a srcset attribute" algorithm closely enough for
// real documents: a candidate's URL runs to the next whitespace, and a URL that
// itself ends in a comma terminates the candidate immediately. That subtlety is
// why splitting the attribute on "," is wrong -- data: URLs contain commas.
func ParseSrcset(attr string) []string {
	var out []string
	i, n := 0, len(attr)
	for i < n {
		for i < n && (isSpace(attr[i]) || attr[i] == ',') {
			i++
		}
		if i >= n {
			break
		}
		start := i
		for i < n && !isSpace(attr[i]) {
			i++
		}
		candidate := attr[start:i]
		trimmed := strings.TrimRight(candidate, ",")
		hadComma := len(trimmed) != len(candidate)
		if trimmed != "" {
			out = append(out, trimmed)
		}
		if hadComma {
			continue
		}
		// Skip the descriptor ("2x", "640w") up to the next comma.
		for i < n && attr[i] != ',' {
			i++
		}
	}
	return out
}
