package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/pwagstro/slidepack/internal/inspect"
	"github.com/pwagstro/slidepack/internal/unpack"
)

func runInspect(args []string) int {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var asJSON bool
	fs.BoolVar(&asJSON, "json", false, "emit machine-readable JSON on stdout")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `slidepack inspect - report on a packed file without expanding it

USAGE
  slidepack inspect <presentation.html> [--json]

Reads only the envelope and the embedded manifest, so inspecting a large
presentation costs a file read and a JSON parse rather than a decompression.
Nothing is extracted and no presentation code is executed.

Reported: format and version, entrypoint, file count, compressed payload size,
uncompressed archive size, total source size, the payload SHA-256, and the
full file listing with per-file size, MIME type and mode.

OPTIONS
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(permute(fs, args)); err != nil {
		return exitUsage
	}
	rest := fs.Args()
	if len(rest) != 1 {
		errorf("inspect needs exactly one packed file")
		return exitUsage
	}

	report, err := inspect.Inspect(rest[0])
	if err != nil {
		var ue *unpack.Error
		if asErr(err, &ue) {
			errorf("%s: %s [%s]", rest[0], ue.Error(), ue.Code)
			return exitInvalid
		}
		return fail(err)
	}

	if asJSON {
		if err := report.WriteJSON(os.Stdout); err != nil {
			return fail(err)
		}
		return exitOK
	}
	if err := report.WriteText(os.Stdout); err != nil {
		return fail(err)
	}
	return exitOK
}
