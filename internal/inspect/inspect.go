// Package inspect reports on a packed presentation without expanding it.
//
// Everything shown comes from the manifest, which is why inspecting a 40 MB
// presentation costs a file read and a JSON parse rather than a decompression.
package inspect

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/pridkett/slidepack/internal/manifest"
	"github.com/pridkett/slidepack/internal/unpack"
)

// Report is the machine-readable inspection result.
type Report struct {
	File           string          `json:"file"`
	Format         string          `json:"format"`
	Version        int             `json:"version"`
	Generator      string          `json:"generator,omitempty"`
	Entrypoint     string          `json:"entrypoint"`
	FileCount      int             `json:"fileCount"`
	DocumentSize   int64           `json:"documentSize"`
	CompressedSize int64           `json:"compressedSize"`
	ArchiveSize    int64           `json:"archiveSize"`
	SourceSize     int64           `json:"sourceSize"`
	PayloadSHA256  string          `json:"payloadSha256"`
	Files          []manifest.File `json:"files"`
}

// Inspect builds a report for a packed presentation on disk.
func Inspect(path string) (*Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	man, _, err := unpack.ReadManifest(data)
	if err != nil {
		return nil, err
	}
	return &Report{
		File:           path,
		Format:         man.Format,
		Version:        man.Version,
		Generator:      man.Generator,
		Entrypoint:     man.Entrypoint,
		FileCount:      len(man.Files),
		DocumentSize:   int64(len(data)),
		CompressedSize: man.Payload.CompressedSize,
		ArchiveSize:    man.Payload.ArchiveSize,
		SourceSize:     man.TotalSize(),
		PayloadSHA256:  man.Payload.SHA256,
		Files:          man.Files,
	}, nil
}

// WriteJSON emits the report as indented JSON.
func (r *Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// WriteText emits a human-readable report.
func (r *Report) WriteText(w io.Writer) error {
	fmt.Fprintf(w, "%s\n\n", r.File)
	fmt.Fprintf(w, "  Format          %s v%d\n", r.Format, r.Version)
	if r.Generator != "" {
		fmt.Fprintf(w, "  Generator       %s\n", r.Generator)
	}
	fmt.Fprintf(w, "  Entrypoint      %s\n", r.Entrypoint)
	fmt.Fprintf(w, "  Files           %d\n", r.FileCount)
	fmt.Fprintf(w, "  Source content  %s\n", HumanBytes(r.SourceSize))
	fmt.Fprintf(w, "  Archive (tar)   %s\n", HumanBytes(r.ArchiveSize))
	fmt.Fprintf(w, "  Payload (gzip)  %s%s\n", HumanBytes(r.CompressedSize), ratio(r.SourceSize, r.CompressedSize))
	fmt.Fprintf(w, "  Document        %s\n", HumanBytes(r.DocumentSize))
	fmt.Fprintf(w, "  Payload SHA-256 %s\n", r.PayloadSHA256)
	fmt.Fprintf(w, "\n")

	files := make([]manifest.File, len(r.Files))
	copy(files, r.Files)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "  MODE\tSIZE\tMIME\tPATH")
	for _, f := range files {
		marker := ""
		if f.Path == r.Entrypoint {
			marker = "  (entrypoint)"
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s%s\n", f.Mode, HumanBytes(f.Size), shortMIME(f.MIME), f.Path, marker)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(w, "\nSHA-256 digests provide integrity checking only; they are not signatures\nand say nothing about who produced this file.\n")
	return nil
}

func shortMIME(m string) string {
	if i := strings.IndexByte(m, ';'); i >= 0 {
		return strings.TrimSpace(m[:i])
	}
	return m
}

func ratio(source, compressed int64) string {
	if source <= 0 || compressed <= 0 {
		return ""
	}
	return fmt.Sprintf("  (%.0f%% of source)", 100*float64(compressed)/float64(source))
}

// HumanBytes renders a byte count with a binary unit suffix.
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 4; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTP"[exp])
}
