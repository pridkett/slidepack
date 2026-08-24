// Package mimes provides a deterministic, host-independent MIME type table.
//
// slidepack deliberately does NOT consult the operating system MIME database
// (mime.TypeByExtension consults /etc/mime.types and the Windows registry),
// because packing the same source tree on macOS, Linux and Windows must
// produce byte-identical output. See docs/format-v1.md.
package mimes

import (
	"path"
	"strings"
)

// Default is returned for extensions that are not in the table.
const Default = "application/octet-stream"

// table maps a lower-case extension (including the leading dot) to a MIME type.
var table = map[string]string{
	// Documents
	".html": "text/html; charset=utf-8",
	".htm":  "text/html; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".js":   "text/javascript; charset=utf-8",
	".mjs":  "text/javascript; charset=utf-8",
	".json": "application/json",
	".map":  "application/json",
	".txt":  "text/plain; charset=utf-8",
	".md":   "text/markdown; charset=utf-8",
	".xml":  "application/xml",
	".csv":  "text/csv; charset=utf-8",

	// Images
	".svg":  "image/svg+xml",
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".avif": "image/avif",
	".bmp":  "image/bmp",
	".ico":  "image/x-icon",
	".tif":  "image/tiff",
	".tiff": "image/tiff",

	// Fonts
	".woff":  "font/woff",
	".woff2": "font/woff2",
	".ttf":   "font/ttf",
	".otf":   "font/otf",
	".eot":   "application/vnd.ms-fontobject",

	// Audio / video
	".mp3":  "audio/mpeg",
	".m4a":  "audio/mp4",
	".oga":  "audio/ogg",
	".ogg":  "audio/ogg",
	".wav":  "audio/wav",
	".flac": "audio/flac",
	".aac":  "audio/aac",
	".opus": "audio/ogg",
	".mp4":  "video/mp4",
	".m4v":  "video/mp4",
	".webm": "video/webm",
	".ogv":  "video/ogg",
	".mov":  "video/quicktime",

	// Subtitles
	".vtt": "text/vtt; charset=utf-8",
	".srt": "application/x-subrip",

	// Misc
	".pdf":  "application/pdf",
	".wasm": "application/wasm",
}

// ForPath returns the MIME type for a package path. Lookup is by lower-cased
// extension only; file contents are never sniffed, so the result is stable.
func ForPath(p string) string {
	ext := strings.ToLower(path.Ext(p))
	if m, ok := table[ext]; ok {
		return m
	}
	return Default
}

// IsHTML reports whether the path names an HTML document.
func IsHTML(p string) bool {
	ext := strings.ToLower(path.Ext(p))
	return ext == ".html" || ext == ".htm"
}

// IsCSS reports whether the path names a stylesheet.
func IsCSS(p string) bool {
	return strings.ToLower(path.Ext(p)) == ".css"
}

// IsJS reports whether the path names a JavaScript file.
func IsJS(p string) bool {
	ext := strings.ToLower(path.Ext(p))
	return ext == ".js" || ext == ".mjs"
}

// Table returns a copy of the MIME table, for documentation and tests.
func Table() map[string]string {
	out := make(map[string]string, len(table))
	for k, v := range table {
		out[k] = v
	}
	return out
}
