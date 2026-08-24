package source

import "strings"

// CSSRef is one URL reference discovered in a stylesheet.
type CSSRef struct {
	// Value is the URL exactly as written, with CSS escapes already resolved
	// but percent-encoding left intact (that is the URL layer's business).
	Value string
	// Start and End are byte offsets of the URL text within the stylesheet,
	// excluding any surrounding quotes. The browser runtime splices
	// replacements into exactly this span.
	Start int
	End   int
	// Line is the 1-based line the reference starts on.
	Line int
	// Import is true when the reference is the target of an @import rule.
	Import bool
}

// ScanCSS finds every url() token and @import target in a stylesheet.
//
// It is a small CSS Syntax Level 3 state machine rather than a regular
// expression. That matters: `content: "url(x)"`, `/* url(x) */` and
// `url(data:...)` must not be confused with real references, and a pattern
// broad enough to catch every genuine form is also broad enough to corrupt
// string literals.
func ScanCSS(src string) []CSSRef {
	var refs []CSSRef
	line := 1
	i := 0
	n := len(src)

	// pendingImport is set once "@import" has been seen and cleared as soon as
	// its target token is consumed, so only the first URL after the at-rule is
	// treated as an import.
	pendingImport := false

	advance := func(to int) {
		for ; i < to && i < n; i++ {
			if src[i] == '\n' {
				line++
			}
		}
	}

	for i < n {
		c := src[i]
		switch {
		case c == '/' && i+1 < n && src[i+1] == '*':
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				advance(n)
				continue
			}
			advance(i + 2 + end + 2)

		case c == '"' || c == '\'':
			startLine := line
			valStart := i + 1
			val, end := readString(src, i)
			if pendingImport {
				refs = append(refs, CSSRef{Value: val, Start: valStart, End: end - 1, Line: startLine, Import: true})
				pendingImport = false
			}
			advance(end)

		case c == '@':
			if hasFoldedPrefix(src[i:], "@import") && !isIdentChar(byteAt(src, i+7)) {
				pendingImport = true
				advance(i + 7)
				continue
			}
			// Any other at-rule clears a stale pending import.
			pendingImport = false
			advance(i + 1)

		case (c == 'u' || c == 'U') && hasFoldedPrefix(src[i:], "url"):
			// url( is only a function token when "url" is not the tail of a
			// longer identifier, e.g. `background: myurl(x)` is not a URL.
			if isIdentChar(byteAt(src, i-1)) || byteAt(src, i-1) == '\\' {
				advance(i + 3)
				continue
			}
			j := i + 3
			for j < n && isSpace(src[j]) {
				j++
			}
			if j >= n || src[j] != '(' {
				advance(i + 3)
				continue
			}
			startLine := line
			ref, end, ok := readURLToken(src, j+1, startLine)
			if !ok {
				advance(i + 3)
				continue
			}
			ref.Import = pendingImport
			pendingImport = false
			refs = append(refs, ref)
			advance(end)

		case c == ';' || c == '{' || c == '}':
			pendingImport = false
			advance(i + 1)

		default:
			advance(i + 1)
		}
	}
	return refs
}

// readURLToken parses the inside of url(...) starting at the byte after "(".
// It returns the reference and the offset just past the closing parenthesis.
func readURLToken(src string, i int, line int) (CSSRef, int, bool) {
	n := len(src)
	for i < n && isSpace(src[i]) {
		i++
	}
	if i >= n {
		return CSSRef{}, i, false
	}
	if src[i] == '"' || src[i] == '\'' {
		valStart := i + 1
		val, end := readString(src, i)
		ref := CSSRef{Value: val, Start: valStart, End: end - 1, Line: line}
		// Skip trailing whitespace and the closing parenthesis.
		for end < n && isSpace(src[end]) {
			end++
		}
		if end < n && src[end] == ')' {
			end++
		}
		return ref, end, true
	}
	// Unquoted url-token: runs to whitespace or ")", with backslash escapes.
	valStart := i
	var sb strings.Builder
	for i < n {
		ch := src[i]
		if ch == ')' || isSpace(ch) {
			break
		}
		if ch == '\\' && i+1 < n {
			sb.WriteByte(src[i+1])
			i += 2
			continue
		}
		sb.WriteByte(ch)
		i++
	}
	valEnd := i
	for i < n && isSpace(src[i]) {
		i++
	}
	if i < n && src[i] == ')' {
		i++
	}
	return CSSRef{Value: sb.String(), Start: valStart, End: valEnd, Line: line}, i, true
}

// readString reads a quoted CSS string beginning at the quote character and
// returns the unescaped contents plus the offset just past the closing quote.
func readString(src string, i int) (string, int) {
	quote := src[i]
	i++
	var sb strings.Builder
	n := len(src)
	for i < n {
		ch := src[i]
		if ch == '\\' && i+1 < n {
			// A backslash-newline is a line continuation and contributes nothing.
			if src[i+1] == '\n' {
				i += 2
				continue
			}
			sb.WriteByte(src[i+1])
			i += 2
			continue
		}
		if ch == quote {
			return sb.String(), i + 1
		}
		if ch == '\n' {
			// Unterminated string: CSS ends it at the newline.
			return sb.String(), i
		}
		sb.WriteByte(ch)
		i++
	}
	return sb.String(), n
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

func isIdentChar(c byte) bool {
	return c == '-' || c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

func byteAt(s string, i int) byte {
	if i < 0 || i >= len(s) {
		return 0
	}
	return s[i]
}

// hasFoldedPrefix reports whether s begins with prefix, ASCII case-insensitively.
func hasFoldedPrefix(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		a, b := s[i], prefix[i]
		if 'A' <= a && a <= 'Z' {
			a += 'a' - 'A'
		}
		if 'A' <= b && b <= 'Z' {
			b += 'a' - 'A'
		}
		if a != b {
			return false
		}
	}
	return true
}
