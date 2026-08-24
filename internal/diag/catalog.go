package diag

import "sort"

// Severity is how a diagnostic affects the outcome.
type Severity string

const (
	// SeverityError makes a target invalid.
	SeverityError Severity = "error"
	// SeverityWarning is advisory and never makes a target invalid, unless
	// --strict is in effect.
	SeverityWarning Severity = "warning"
)

// Category groups related diagnostics for presentation.
type Category string

const (
	CatStructure   Category = "structure"
	CatResources   Category = "resources"
	CatUnsupported Category = "unsupported"
	CatFilesystem  Category = "filesystem"
	CatPackage     Category = "package"
)

// Info documents one diagnostic code.
//
// This catalogue is what `slidepack help --json` publishes, so an agent can
// learn the full diagnostic vocabulary — and what to do about each entry —
// without scraping prose. It is deliberately data rather than documentation:
// a code that exists but is not described here fails the interface check.
type Info struct {
	Code     Code     `json:"code"`
	Severity Severity `json:"severity"`
	Category Category `json:"category"`
	Summary  string   `json:"summary"`
	// Remedy says what to change. Every entry has one; a diagnostic that
	// cannot tell you what to do is not worth reporting.
	Remedy string `json:"remedy"`
}

var catalog = []Info{
	// ---- structure ----
	{MissingEntrypoint, SeverityError, CatStructure,
		"The entry document does not exist in the source tree.",
		"Create index.html, or name a different entry document with --entry."},
	{InvalidEntrypoint, SeverityError, CatStructure,
		"The entry document is not an HTML file.",
		"Point --entry at a .html or .htm file."},
	{EmptySource, SeverityError, CatStructure,
		"The source directory contains no files.",
		"Add at least an entry document."},
	{InvalidPath, SeverityError, CatStructure,
		"A path is not a legal package path.",
		"Package paths are relative, slash-separated, UTF-8, and contain no \"..\" segment, backslash, drive letter or control character."},
	{PathTooLong, SeverityError, CatStructure,
		"A path does not fit a USTAR header.",
		"Shorten a directory or file name: a path over 100 bytes must split at a \"/\" into a prefix of at most 155 bytes and a name of at most 100."},
	{Unreadable, SeverityError, CatStructure,
		"A file could not be read.",
		"Check the file's permissions and that it still exists."},

	// ---- resources ----
	{MissingResource, SeverityError, CatResources,
		"A statically referenced local file does not exist.",
		"Add the file to the source tree, or correct the reference. Paths resolve relative to the file containing them."},
	{RemoteResource, SeverityError, CatResources,
		"A rendering dependency is loaded over the network.",
		"Download the resource into the source tree and reference it by path. Ordinary hyperlinks are fine; only loaded subresources are rejected."},
	{EscapingRef, SeverityError, CatResources,
		"A reference resolves outside the package root.",
		"Move the resource inside the presentation directory and reference it from there."},

	// ---- unsupported constructs ----
	{ESModule, SeverityError, CatUnsupported,
		"ES module syntax or a module script.",
		"Bundle the module graph into a single classic script and load it with <script src=\"...\">."},
	{ImportMap, SeverityError, CatUnsupported,
		"An import map.",
		"Bundle your JavaScript instead; format v1 loads classic scripts only."},
	{BaseElement, SeverityError, CatUnsupported,
		"A <base> element.",
		"Remove it and use paths relative to the document. <base> makes resource resolution ambiguous and conflicts with the frame's own base URL."},
	{ServiceWorker, SeverityError, CatUnsupported,
		"Service worker registration.",
		"Remove it. Service workers cannot be registered from a file:// document in any case."},
	{LocalIframe, SeverityError, CatUnsupported,
		"An iframe whose src is a package-local document.",
		"Inline the embedded content into the entry document."},
	{DynamicFetch, SeverityError, CatUnsupported,
		"fetch() of a literal package-local path.",
		"Resources exist only as blob: URLs at runtime, so a source path cannot resolve. Inline the data in a <script type=\"application/json\"> block and read it from the DOM."},
	{WebWorker, SeverityError, CatUnsupported,
		"Worker construction or importScripts().",
		"Move the work onto the main thread, or bundle it; a worker's script URL cannot be provided by format v1."},
	{DynamicImport, SeverityWarning, CatUnsupported,
		"A dynamic import() call.",
		"If it resolves a package-local path it will fail at runtime. Bundle the imported code instead."},
	{LocalNavLink, SeverityWarning, CatUnsupported,
		"A link to a package-local HTML document.",
		"Format v1 renders one entrypoint, so this link will not navigate. Fold the page into the entry document and link to it by #fragment."},
	{MetaRefresh, SeverityWarning, CatUnsupported,
		"A refresh or redirect meta tag.",
		"Remove it; it will not resolve inside a packed presentation."},
	{UnknownDynamic, SeverityWarning, CatUnsupported,
		"A construct that may load a resource at runtime.",
		"Confirm it does not resolve a package-local path. Static analysis of arbitrary JavaScript cannot prove this either way."},

	// ---- filesystem object types ----
	{Symlink, SeverityError, CatFilesystem,
		"The source tree contains a symbolic link.",
		"Replace the link with a copy of the file. Following a link could pull in data from outside the source tree."},
	{SpecialFile, SeverityError, CatFilesystem,
		"The source tree contains a device, FIFO or socket.",
		"Remove it. Format v1 archives regular files only."},

	// ---- packed documents ----
	{NotSlidepack, SeverityError, CatPackage,
		"The file is not a packed slidepack presentation.",
		"Check that you passed a file produced by `slidepack pack`."},
	{MalformedEnvelope, SeverityError, CatPackage,
		"The HTML envelope or its payload attributes are wrong.",
		"The file has been edited or truncated. Obtain a fresh copy, or re-pack from source."},
	{MalformedManifest, SeverityError, CatPackage,
		"The embedded manifest is invalid.",
		"The file has been edited. Re-pack from source."},
	{UnsupportedVersion, SeverityError, CatPackage,
		"The file uses a format version this build cannot read.",
		"Upgrade slidepack, or re-pack the source with this version."},
	{CorruptBase64, SeverityError, CatPackage,
		"The payload is not valid base64.",
		"The file was corrupted in transit, most often by a text-mode transfer. Obtain a fresh copy."},
	{PayloadHashMismatch, SeverityError, CatPackage,
		"The payload digest or length does not match the manifest.",
		"The file is truncated or has been modified. Obtain a fresh copy."},
	{CorruptGzip, SeverityError, CatPackage,
		"The payload is not a decompressible gzip stream.",
		"The file is corrupted. Obtain a fresh copy."},
	{CorruptTar, SeverityError, CatPackage,
		"The archive structure is invalid.",
		"The file is corrupted or was not produced by slidepack. Obtain a fresh copy."},
	{ManifestMismatch, SeverityError, CatPackage,
		"The manifest and the archive describe different trees.",
		"The file has been tampered with or assembled incorrectly. Re-pack from source."},
	{FileHashMismatch, SeverityError, CatPackage,
		"A file's digest does not match the manifest.",
		"The file has been modified since packing. Re-pack from source."},
}

// Catalog returns every documented diagnostic, ordered by code.
func Catalog() []Info {
	out := make([]Info, len(catalog))
	copy(out, catalog)
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// Describe returns the catalogue entry for a code.
func Describe(c Code) (Info, bool) {
	for _, i := range catalog {
		if i.Code == c {
			return i, true
		}
	}
	return Info{}, false
}

// SeverityOf reports the documented severity of a code, defaulting to error
// for anything undocumented so that an unknown finding is never silently
// downgraded.
func SeverityOf(c Code) Severity {
	if info, ok := Describe(c); ok {
		return info.Severity
	}
	return SeverityError
}
