package main

import (
	"errors"
	"fmt"

	"github.com/pwagstro/slidepack/internal/cli"
	"github.com/pwagstro/slidepack/internal/inspect"
	"github.com/pwagstro/slidepack/internal/pack"
	"github.com/pwagstro/slidepack/internal/source"
)

func packCommand() *cli.Command {
	return withGlobals(&cli.Command{
		Name:    "pack",
		Summary: "Build a single self-contained HTML file from a source directory",
		Description: `Validates the source directory, then compiles it into one .html file containing every asset.

The result opens directly from the filesystem in current Firefox and Chromium: no server, no browser extension, no companion directory and no network. JavaScript must be enabled.

Nothing is written if validation fails. A pack that succeeds is a presentation that renders.`,
		Usage: []string{
			"<directory> --output <file.html> [options]",
		},
		Arguments: []cli.Argument{
			{Name: "directory", Summary: "The presentation source directory.", Required: true},
		},
		Options: []cli.Option{
			{
				Name: "output", Short: "o", Type: cli.TypeString, Placeholder: "file.html",
				Required: true,
				Summary:  "Path of the HTML file to write",
				Details:  "Parent directories are created as needed. The path may not be inside the directory being packed.",
			},
			{
				Name: "entry", Type: cli.TypeString, Placeholder: "path",
				Default: source.DefaultEntrypoint,
				Summary: "Package path of the entry document",
				Details: "Must be a .html or .htm file inside the source directory.",
			},
			{
				Name: "force", Type: cli.TypeBoolean,
				Summary: "Replace the output file if it already exists",
			},
			{
				Name: "quiet", Short: "q", Type: cli.TypeBoolean,
				Summary: "Print nothing on success",
			},
		},
		Notes: []cli.Note{
			{
				Title: "What makes packing fail",
				Body: `Packing refuses, without writing anything, when:

  the entry document does not exist
  a statically referenced local file is missing
  a rendering resource is loaded over http or https
  the tree contains a symlink, device, FIFO or socket
  a path cannot be represented in a USTAR header
  the source uses a construct format v1 cannot serve

Run validate for the same checks without producing a file. Every finding carries a stable diagnostic code; see slidepack help --json.`,
			},
			{
				Title: "Reproducible output",
				Body: `Given the same source paths, bytes, permission bits and entrypoint, and the same slidepack version, packing produces a byte-identical file. Filesystem modification times have no effect.

Two caveats: permission bits are an input, and Git records only the executable bit, so prefer 0644 and 0755 if you want identical output after a fresh clone. The generator string is recorded in the manifest, so output changes across slidepack versions by design.`,
			},
			{
				Title: "Files that are never packed",
				Body:  "These names are skipped so that output cannot depend on which machine last opened the directory: " + source.DescribeExclusions() + ". Empty directories are not preserved.",
			},
		},
		Examples: []cli.Example{
			{Summary: "Build a deck for distribution",
				Command: "slidepack pack ./quarterly-review -o quarterly-review.html"},
			{Summary: "Use a different entry document",
				Command: "slidepack pack ./deck -o deck.html --entry slides.html"},
			{Summary: "Rebuild over an existing file, silently",
				Command: "slidepack pack ./deck -o dist/deck.html --force --quiet"},
		},
		SeeAlso: []string{"validate", "unpack", "inspect"},
		Run:     runPack,
	})
}

func runPack(env *cli.Env, v *cli.Values) int {
	applyColor(env, v)
	p := env.Style
	sourceDir := v.Args()[0]
	output := v.String("output")

	res, err := pack.Run(pack.Options{
		SourceDir:  sourceDir,
		Output:     output,
		Entrypoint: v.String("entry"),
		Force:      v.Bool("force"),
		Generator:  generatorString(),
	})
	if err != nil {
		var ve *pack.ValidationError
		if errors.As(err, &ve) {
			errorf(env, "%s is not a valid presentation source; nothing was written",
				env.ErrStyle.Path(sourceDir))
			fmt.Fprintln(env.Err)
			renderDiagnostics(env.Err, env.ErrStyle, ve.Result)
			return exitInvalid
		}
		return fail(env, err)
	}

	if v.Bool("quiet") {
		return exitOK
	}

	n := len(res.Manifest.Files)
	fmt.Fprintf(env.Out, "%s Packed %s file%s %s into %s %s\n",
		p.MarkOK(),
		p.Value(fmt.Sprint(n)), plural(n),
		p.Muted("("+inspect.HumanBytes(res.Manifest.TotalSize())+" of source)"),
		p.Path(output),
		p.Muted("("+inspect.HumanBytes(res.OutputSize)+")"),
	)

	if len(res.Validation.Warnings) > 0 {
		fmt.Fprintln(env.Err)
		renderDiagnostics(env.Err, env.ErrStyle, res.Validation)
	}
	return exitOK
}
