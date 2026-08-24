// Package diag defines slidepack's stable diagnostic vocabulary.
//
// Codes are part of the tool's public contract: agents and CI scripts match on
// them, so they are never renamed or repurposed. Adding a code is a compatible
// change; changing what an existing code means is not.
package diag

import "sort"

// Code is a stable machine-readable diagnostic identifier.
type Code string

const (
	// Structural source problems.
	MissingEntrypoint Code = "MISSING_ENTRYPOINT"
	InvalidEntrypoint Code = "INVALID_ENTRYPOINT"
	EmptySource       Code = "EMPTY_SOURCE"
	InvalidPath       Code = "INVALID_PATH"
	PathTooLong       Code = "PATH_TOO_LONG"
	Unreadable        Code = "UNREADABLE_FILE"

	// Resource resolution.
	MissingResource Code = "MISSING_RESOURCE"
	RemoteResource  Code = "REMOTE_RESOURCE"
	EscapingRef     Code = "ESCAPING_REFERENCE"

	// Constructs outside the format v1 resource model.
	ESModule       Code = "ES_MODULE"
	ImportMap      Code = "IMPORT_MAP"
	BaseElement    Code = "BASE_ELEMENT"
	ServiceWorker  Code = "SERVICE_WORKER"
	LocalIframe    Code = "LOCAL_IFRAME"
	DynamicFetch   Code = "DYNAMIC_LOCAL_FETCH"
	WebWorker      Code = "WEB_WORKER"
	DynamicImport  Code = "DYNAMIC_IMPORT"
	LocalNavLink   Code = "LOCAL_NAVIGATION_LINK"
	MetaRefresh    Code = "META_REFRESH"
	UnknownDynamic Code = "POSSIBLE_DYNAMIC_RESOURCE"

	// Packed-document problems.
	NotSlidepack        Code = "NOT_SLIDEPACK"
	MalformedEnvelope   Code = "MALFORMED_ENVELOPE"
	MalformedManifest   Code = "MALFORMED_MANIFEST"
	UnsupportedVersion  Code = "UNSUPPORTED_VERSION"
	CorruptBase64       Code = "CORRUPT_BASE64"
	PayloadHashMismatch Code = "PAYLOAD_HASH_MISMATCH"
	CorruptGzip         Code = "CORRUPT_GZIP"
	CorruptTar          Code = "CORRUPT_TAR"
	ManifestMismatch    Code = "MANIFEST_MISMATCH"
	FileHashMismatch    Code = "FILE_HASH_MISMATCH"

	// Filesystem object types.
	Symlink     Code = "SYMLINK"
	SpecialFile Code = "SPECIAL_FILE"
)

// Diagnostic is one validation finding.
type Diagnostic struct {
	// Code is the stable identifier.
	Code Code `json:"code"`
	// Path is the package path of the file the finding is about, if any.
	Path string `json:"path,omitempty"`
	// Line is a 1-based line number within Path, or 0 when not applicable.
	Line int `json:"line,omitempty"`
	// Detail names the specific reference or construct, e.g. "img[src]".
	Detail string `json:"detail,omitempty"`
	// Message is the human-readable explanation.
	Message string `json:"message"`
}

// Result accumulates diagnostics for one validation run.
type Result struct {
	Valid    bool         `json:"valid"`
	Target   string       `json:"target,omitempty"`
	Kind     string       `json:"kind,omitempty"`
	Errors   []Diagnostic `json:"errors"`
	Warnings []Diagnostic `json:"warnings"`
}

// NewResult returns an empty, valid result with non-nil slices so that JSON
// output always contains arrays rather than nulls.
func NewResult(target, kind string) *Result {
	return &Result{Valid: true, Target: target, Kind: kind, Errors: []Diagnostic{}, Warnings: []Diagnostic{}}
}

// Errorf records an error and marks the result invalid.
func (r *Result) Errorf(code Code, path string, line int, detail, format string, args ...any) {
	r.Errors = append(r.Errors, Diagnostic{Code: code, Path: path, Line: line, Detail: detail, Message: sprintf(format, args...)})
	r.Valid = false
}

// Warnf records a warning. Warnings never make a result invalid.
func (r *Result) Warnf(code Code, path string, line int, detail, format string, args ...any) {
	r.Warnings = append(r.Warnings, Diagnostic{Code: code, Path: path, Line: line, Detail: detail, Message: sprintf(format, args...)})
}

// Sort orders diagnostics by path, then line, then code, so that output is
// stable regardless of the order in which files happened to be walked.
func (r *Result) Sort() {
	less := func(s []Diagnostic) func(i, j int) bool {
		return func(i, j int) bool {
			a, b := s[i], s[j]
			if a.Path != b.Path {
				return a.Path < b.Path
			}
			if a.Line != b.Line {
				return a.Line < b.Line
			}
			if a.Code != b.Code {
				return a.Code < b.Code
			}
			return a.Message < b.Message
		}
	}
	sort.SliceStable(r.Errors, less(r.Errors))
	sort.SliceStable(r.Warnings, less(r.Warnings))
}

// Merge folds another result's diagnostics into r.
func (r *Result) Merge(other *Result) {
	r.Errors = append(r.Errors, other.Errors...)
	r.Warnings = append(r.Warnings, other.Warnings...)
	if !other.Valid {
		r.Valid = false
	}
}
