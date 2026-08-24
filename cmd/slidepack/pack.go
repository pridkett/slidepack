package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/pwagstro/slidepack/internal/inspect"
	"github.com/pwagstro/slidepack/internal/pack"
	"github.com/pwagstro/slidepack/internal/source"
)

func runPack(args []string) int {
	fs := flag.NewFlagSet("pack", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		output string
		entry  string
		force  bool
		quiet  bool
	)
	fs.StringVar(&output, "o", "", "path of the HTML file to write (required)")
	fs.StringVar(&output, "output", "", "path of the HTML file to write (required)")
	fs.StringVar(&entry, "entry", source.DefaultEntrypoint, "package path of the entry document")
	fs.BoolVar(&force, "force", false, "replace the output file if it already exists")
	fs.BoolVar(&quiet, "quiet", false, "print nothing on success")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `slidepack pack - build a single self-contained HTML file

USAGE
  slidepack pack <directory> -o <presentation.html> [options]

The source directory is validated first. Packing fails, without writing
anything, if the entrypoint is missing, a referenced local file does not
exist, a rendering resource is loaded over the network, or the source uses a
construct format v1 cannot serve (ES modules, <base>, service workers,
local iframes, import maps).

Output is deterministic: the same source bytes, paths, modes and entrypoint
produce a byte-identical file. Filesystem timestamps are not recorded.

These names are never packed: %s

OPTIONS
`, source.DescribeExclusions())
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
EXAMPLES
  slidepack pack ./deck -o deck.html
  slidepack pack ./deck -o deck.html --entry slides.html --force
`)
	}
	if err := fs.Parse(permute(fs, args)); err != nil {
		return exitUsage
	}
	rest := fs.Args()
	if len(rest) == 0 {
		errorf("pack needs a source directory\n\nUsage: slidepack pack <directory> -o <presentation.html>")
		return exitUsage
	}
	if len(rest) > 1 {
		errorf("pack takes exactly one source directory (got %d)", len(rest))
		return exitUsage
	}
	if output == "" {
		errorf("pack needs an output path\n\nUsage: slidepack pack <directory> -o <presentation.html>")
		return exitUsage
	}

	res, err := pack.Run(pack.Options{
		SourceDir:  rest[0],
		Output:     output,
		Entrypoint: entry,
		Force:      force,
		Generator:  generatorString(),
	})
	if err != nil {
		var ve *pack.ValidationError
		if errors.As(err, &ve) {
			errorf("%s is not a valid presentation source; nothing was written", rest[0])
			fmt.Fprintln(os.Stderr)
			printDiagnostics(os.Stderr, ve.Result)
			return exitInvalid
		}
		return fail(err)
	}

	if !quiet {
		fmt.Printf("Packed %d file%s (%s of source) into %s (%s)\n",
			len(res.Manifest.Files), plural(len(res.Manifest.Files)),
			inspect.HumanBytes(res.Manifest.TotalSize()),
			output, inspect.HumanBytes(res.OutputSize))
		if len(res.Validation.Warnings) > 0 {
			fmt.Fprintln(os.Stderr)
			printDiagnostics(os.Stderr, res.Validation)
		}
	}
	return exitOK
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
