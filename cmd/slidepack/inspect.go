package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/pwagstro/slidepack/internal/cli"
	"github.com/pwagstro/slidepack/internal/inspect"
	"github.com/pwagstro/slidepack/internal/manifest"
	"github.com/pwagstro/slidepack/internal/unpack"
)

func inspectCommand() *cli.Command {
	return withGlobals(&cli.Command{
		Name:    "inspect",
		Summary: "Report on a packed file's contents without expanding it",
		Description: `Shows what is inside a packed presentation: the format version, the entry document, every file with its size, MIME type and permission bits, the compressed and uncompressed sizes, and the payload digest.

Only the envelope and the embedded manifest are read, so inspecting a 40 MB presentation costs a file read and a JSON parse rather than a decompression. Nothing is extracted and no presentation code is executed.

This is the right way for an agent to learn what a packed file contains. Reading the base64 payload directly yields nothing useful.`,
		Usage: []string{
			"<file.html> [options]",
			"--json <file.html>",
		},
		Arguments: []cli.Argument{
			{Name: "file.html", Summary: "The packed presentation to report on.", Required: true},
		},
		Options: []cli.Option{
			{
				Name: "json", Type: cli.TypeBoolean,
				Summary: "Emit machine-readable JSON on stdout",
			},
			{
				Name: "files", Type: cli.TypeBoolean,
				Summary: "Print only the file paths, one per line",
				Details: "Convenient for piping into other tools.",
			},
			{
				Name: "digests", Type: cli.TypeBoolean,
				Summary: "Show each file's full SHA-256 in the listing",
			},
		},
		Notes: []cli.Note{
			{
				Title: "On the digests",
				Body:  "The SHA-256 values are integrity checks. They detect truncation and corruption; they are not signatures and say nothing about who produced the file. Use validate to actually verify them against the payload.",
			},
		},
		Examples: []cli.Example{
			{Summary: "See what a deck contains", Command: "slidepack inspect quarterly-review.html"},
			{Summary: "List the packaged files", Command: "slidepack inspect --files deck.html"},
			{Summary: "Check every digest in the listing", Command: "slidepack inspect --digests deck.html"},
			{Summary: "Read the manifest programmatically", Command: "slidepack inspect --json deck.html"},
			{Summary: "Find the largest assets",
				Command: "slidepack inspect --json deck.html | jq -r '.files | sort_by(-.size)[:5][] | \"\\(.size)\\t\\(.path)\"'"},
		},
		SeeAlso: []string{"validate", "unpack"},
		Run:     runInspect,
	})
}

func runInspect(env *cli.Env, v *cli.Values) int {
	applyColor(env, v)
	target := v.Args()[0]

	report, err := inspect.Inspect(target)
	if err != nil {
		var ue *unpack.Error
		if errors.As(err, &ue) {
			reportPackageError(env, target, ue)
			return exitInvalid
		}
		return fail(env, err)
	}

	switch {
	case v.Bool("json"):
		if err := report.WriteJSON(env.Out); err != nil {
			return fail(env, err)
		}
	case v.Bool("files"):
		files := sortedFiles(report.Files)
		for _, f := range files {
			fmt.Fprintln(env.Out, f.Path)
		}
	default:
		renderReport(env, report, v.Bool("digests"))
	}
	return exitOK
}

func sortedFiles(files []manifest.File) []manifest.File {
	out := make([]manifest.File, len(files))
	copy(out, files)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// renderReport writes the human-readable inspection.
func renderReport(env *cli.Env, r *inspect.Report, digests bool) {
	p := env.Style
	w := env.Out

	fmt.Fprintf(w, "%s\n\n", p.Title(r.File))

	fields := &cli.FieldList{Indent: "  ", LabelStyle: p.Muted}
	fields.Add("format", fmt.Sprintf("%s v%d", r.Format, r.Version), "", p.Value)
	if r.Generator != "" {
		fields.Add("generator", r.Generator, "", p.Muted)
	}
	fields.Add("entrypoint", r.Entrypoint, "", p.Path)
	fields.Add("files", fmt.Sprint(r.FileCount), "", p.Value)
	fields.Blank()
	fields.Add("source content", inspect.HumanBytes(r.SourceSize), "", p.Value)
	fields.Add("archive (tar)", inspect.HumanBytes(r.ArchiveSize), "", p.Value)
	fields.Add("payload (gzip)", inspect.HumanBytes(r.CompressedSize), compressionNote(r), p.Value)
	fields.Add("document", inspect.HumanBytes(r.DocumentSize), "", p.Value)
	fields.Blank()
	fields.Add("payload sha256", r.PayloadSHA256, "", p.Muted)
	fields.Render(w, p)
	fmt.Fprintln(w)

	table := &cli.Table{Indent: "  ", Gap: 2}
	header := []cli.Cell{
		cli.Styled("MODE", p.Muted),
		cli.Cell{Text: "SIZE", Style: p.Muted, Right: true},
		cli.Styled("TYPE", p.Muted),
	}
	if digests {
		header = append(header, cli.Styled("SHA-256", p.Muted))
	}
	table.Header = append(header, cli.Styled("PATH", p.Muted))

	for _, f := range sortedFiles(r.Files) {
		row := []cli.Cell{
			cli.Styled(f.Mode, p.Muted),
			cli.RightCell(inspect.HumanBytes(f.Size), nil),
			cli.Styled(shortMIME(f.MIME), p.Muted),
		}
		if digests {
			row = append(row, cli.Styled(f.SHA256, p.Muted))
		}
		if f.Path == r.Entrypoint {
			row = append(row, cli.Styled(f.Path, func(s string) string {
				return p.Value(s) + "  " + p.Cyan("← entrypoint")
			}))
		} else {
			row = append(row, cli.Styled(f.Path, p.Path))
		}
		table.AddRow(row...)
	}
	table.Render(w)

	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s\n", p.Muted("SHA-256 digests provide integrity checking only; they are not signatures"))
	fmt.Fprintf(w, "%s\n", p.Muted("and say nothing about who produced this file."))
}

func compressionNote(r *inspect.Report) string {
	if r.SourceSize <= 0 || r.CompressedSize <= 0 {
		return ""
	}
	return fmt.Sprintf("(%.0f%% of source)", 100*float64(r.CompressedSize)/float64(r.SourceSize))
}

func shortMIME(m string) string {
	if i := strings.IndexByte(m, ';'); i >= 0 {
		return strings.TrimSpace(m[:i])
	}
	return m
}
