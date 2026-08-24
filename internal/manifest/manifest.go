// Package manifest defines the machine-readable index embedded in a packed
// slidepack presentation.
//
// The manifest is what makes inspect and validate cheap: file names, sizes,
// hashes and MIME types can all be answered without touching the compressed
// payload. It contains no timestamps and no generated identifiers, because
// anything of that sort would break byte-for-byte reproducible packing.
package manifest

import (
	"encoding/json"
	"fmt"
	"sort"
)

// Format is the value of the "format" field. It exists so that a consumer can
// reject a document that merely happens to have an element with our ID.
const Format = "slidepack"

// Version is the format version this build reads and writes.
const Version = 1

// Payload describes the single archive blob embedded in the document.
type Payload struct {
	// Archive is always "tar" in v1.
	Archive string `json:"archive"`
	// Compression is always "gzip" in v1.
	Compression string `json:"compression"`
	// Encoding is always "base64" in v1.
	Encoding string `json:"encoding"`
	// SHA256 is the hex digest of the gzip stream, i.e. of the bytes obtained
	// after base64-decoding and before decompressing. This is an integrity
	// check against corruption and truncation. It is NOT a signature and
	// proves nothing about who produced the file.
	SHA256 string `json:"sha256"`
	// CompressedSize is the length in bytes of the gzip stream.
	CompressedSize int64 `json:"compressedSize"`
	// ArchiveSize is the length in bytes of the uncompressed tar stream.
	ArchiveSize int64 `json:"archiveSize"`
}

// File is one regular file in the source tree.
type File struct {
	// Path is the canonical, slash-separated package path.
	Path string `json:"path"`
	// Size is the file length in bytes.
	Size int64 `json:"size"`
	// SHA256 is the hex digest of the file contents.
	SHA256 string `json:"sha256"`
	// MIME is the type the browser runtime assigns to the Blob it creates.
	MIME string `json:"mime"`
	// Mode is the permission bits, formatted as four octal digits ("0644").
	Mode string `json:"mode"`
}

// Manifest is the complete embedded index.
type Manifest struct {
	Format     string  `json:"format"`
	Version    int     `json:"version"`
	Generator  string  `json:"generator"`
	Entrypoint string  `json:"entrypoint"`
	Payload    Payload `json:"payload"`
	Files      []File  `json:"files"`
}

// TotalSize returns the summed size of every source file.
func (m *Manifest) TotalSize() int64 {
	var n int64
	for _, f := range m.Files {
		n += f.Size
	}
	return n
}

// Lookup returns the file entry for a package path.
func (m *Manifest) Lookup(p string) (File, bool) {
	// Files are sorted by path, so a binary search is correct and keeps
	// validation of a large package linear rather than quadratic.
	i := sort.Search(len(m.Files), func(i int) bool { return m.Files[i].Path >= p })
	if i < len(m.Files) && m.Files[i].Path == p {
		return m.Files[i], true
	}
	return File{}, false
}

// SortFiles puts entries into the canonical order: byte-wise ascending path.
// Sorting by raw bytes rather than by any locale-aware collation is what makes
// the ordering identical on every machine.
func SortFiles(files []File) {
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
}

// Marshal renders the manifest as indented JSON with a trailing newline.
// Go's encoder emits struct fields in declaration order and map keys sorted,
// so the output is stable.
func (m *Manifest) Marshal() ([]byte, error) {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Unmarshal parses and structurally checks a manifest document.
func Unmarshal(data []byte) (*Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(newTrimReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("manifest is not valid slidepack JSON: %w", err)
	}
	return &m, nil
}

// ErrUnsupportedVersion reports a manifest this build cannot read.
type ErrUnsupportedVersion struct {
	Got int
}

func (e *ErrUnsupportedVersion) Error() string {
	return fmt.Sprintf("unsupported slidepack format version %d; this build understands version %d", e.Got, Version)
}

// Validate checks the invariants every v1 manifest must satisfy. It does not
// look at the payload; that is validate's job.
func (m *Manifest) Validate() error {
	if m.Format != Format {
		return fmt.Errorf("manifest format is %q, expected %q", m.Format, Format)
	}
	if m.Version != Version {
		return &ErrUnsupportedVersion{Got: m.Version}
	}
	if m.Payload.Archive != "tar" {
		return fmt.Errorf("unsupported archive format %q", m.Payload.Archive)
	}
	if m.Payload.Compression != "gzip" {
		return fmt.Errorf("unsupported compression %q", m.Payload.Compression)
	}
	if m.Payload.Encoding != "base64" {
		return fmt.Errorf("unsupported payload encoding %q", m.Payload.Encoding)
	}
	if len(m.Payload.SHA256) != 64 {
		return fmt.Errorf("payload sha256 is not a 64-character hex digest")
	}
	if m.Entrypoint == "" {
		return fmt.Errorf("manifest has no entrypoint")
	}
	if _, ok := m.Lookup(m.Entrypoint); !ok {
		return fmt.Errorf("entrypoint %q is not listed in the manifest", m.Entrypoint)
	}
	var prev string
	for i, f := range m.Files {
		if f.Path == "" {
			return fmt.Errorf("manifest file %d has an empty path", i)
		}
		if i > 0 && f.Path <= prev {
			return fmt.Errorf("manifest files are not sorted by path: %q follows %q", f.Path, prev)
		}
		prev = f.Path
		if len(f.SHA256) != 64 {
			return fmt.Errorf("manifest entry %q has a malformed sha256", f.Path)
		}
	}
	return nil
}
