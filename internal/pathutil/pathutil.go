package pathutil

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// MaxPathBytes bounds the length of a package path. USTAR itself allows up to
// 255 bytes across prefix+name; this cap is applied before the stricter
// CheckUSTAR split test so that error messages stay comprehensible.
const MaxPathBytes = 255

// ErrEmpty is returned for a path that is empty after normalization.
var ErrEmpty = errors.New("empty path")

// FromFS converts a host filesystem path, relative to a source root, into a
// package path. It does not validate; call Check on the result.
func FromFS(rel string) string {
	return path.Clean(filepath.ToSlash(rel))
}

// Check reports whether p is a valid, safe package path.
//
// A valid package path:
//   - is valid UTF-8 and contains no NUL or other control characters;
//   - uses "/" separators only (backslashes are rejected outright, because a
//     backslash is a separator on Windows and a legal filename byte on Unix);
//   - is relative: no leading "/", no "//" UNC prefix, no "C:" drive letter;
//   - contains no "." or ".." segment and no empty segment;
//   - is already in cleaned form, so what we validate is what gets used;
//   - has no trailing slash and is at most MaxPathBytes long.
func Check(p string) error {
	if p == "" {
		return ErrEmpty
	}
	if len(p) > MaxPathBytes {
		return fmt.Errorf("path is %d bytes, exceeding the %d byte limit", len(p), MaxPathBytes)
	}
	if !utf8.ValidString(p) {
		return errors.New("path is not valid UTF-8")
	}
	for _, r := range p {
		if r == 0 {
			return errors.New("path contains a NUL byte")
		}
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("path contains control character U+%04X", r)
		}
	}
	if strings.Contains(p, `\`) {
		return errors.New(`path contains a backslash; package paths must use "/" separators`)
	}
	if strings.HasPrefix(p, "/") {
		return errors.New("path is absolute")
	}
	if hasDriveLetter(p) {
		return errors.New("path contains a Windows drive letter")
	}
	if strings.HasSuffix(p, "/") {
		return errors.New("path has a trailing slash")
	}
	for _, seg := range strings.Split(p, "/") {
		switch seg {
		case "":
			return errors.New("path contains an empty segment")
		case ".":
			return errors.New(`path contains a "." segment`)
		case "..":
			return errors.New(`path contains a ".." segment`)
		}
	}
	if path.Clean(p) != p {
		return errors.New("path is not in canonical form")
	}
	return nil
}

// hasDriveLetter reports whether p starts with a DOS drive specifier such as
// "C:" or "c:foo". Checked independently of the backslash rule because
// "C:/evil" is slash-separated but still absolute on Windows.
func hasDriveLetter(p string) bool {
	if len(p) < 2 || p[1] != ':' {
		return false
	}
	c := p[0]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// CheckUSTAR reports whether p can be represented in a plain USTAR header.
//
// USTAR stores a name of up to 100 bytes and an optional prefix of up to 155
// bytes, joined with "/". If a path does not fit, archive/tar would silently
// upgrade the record to PAX, which the browser runtime's minimal TAR reader
// does not understand, so slidepack rejects it instead.
func CheckUSTAR(p string) error {
	if len(p) <= 100 {
		return nil
	}
	if len(p) > 256 {
		return ustarErr(p)
	}
	// Find a split point: prefix + "/" + name, prefix <= 155, name <= 100.
	for i := len(p) - 101; i < len(p); i++ {
		if i < 0 {
			continue
		}
		if i >= len(p) || p[i] != '/' {
			continue
		}
		prefix, name := p[:i], p[i+1:]
		if len(prefix) <= 155 && len(prefix) > 0 && len(name) <= 100 && name != "" {
			return nil
		}
	}
	return ustarErr(p)
}

func ustarErr(p string) error {
	return fmt.Errorf("path is %d bytes and cannot be split into a USTAR prefix (<=155) and name (<=100); shorten a directory or file name", len(p))
}

// SafeJoin resolves a package path against a destination root and guarantees
// the result stays inside that root. root must already be absolute and, ideally,
// symlink-resolved by the caller.
func SafeJoin(root, p string) (string, error) {
	if err := Check(p); err != nil {
		return "", err
	}
	joined := filepath.Join(root, filepath.FromSlash(p))
	// Belt and braces: Check already forbids "..", but re-verify containment so
	// that a future change to Check cannot silently open a traversal hole.
	cleanRoot := filepath.Clean(root)
	rel, err := filepath.Rel(cleanRoot, joined)
	if err != nil {
		return "", fmt.Errorf("cannot resolve %q against extraction root: %w", p, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path %q escapes the extraction root", p)
	}
	return joined, nil
}

// ResolveRef resolves a reference found inside the file at base (a package
// path) to a package path. Returns ok=false when the reference escapes the
// package root.
func ResolveRef(base, ref string) (string, bool) {
	if strings.HasPrefix(ref, "/") {
		// Root-relative: relative to the package root, not the host filesystem.
		ref = strings.TrimPrefix(ref, "/")
		cleaned := path.Clean("/" + ref)
		return strings.TrimPrefix(cleaned, "/"), true
	}
	dir := path.Dir(base)
	if dir == "." {
		dir = ""
	}
	joined := path.Join(dir, ref)
	if joined == ".." || strings.HasPrefix(joined, "../") {
		return "", false
	}
	return joined, true
}
