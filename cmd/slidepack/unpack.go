package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/pwagstro/slidepack/internal/inspect"
	"github.com/pwagstro/slidepack/internal/unpack"
)

func runUnpack(args []string) int {
	fs := flag.NewFlagSet("unpack", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		output string
		force  bool
		quiet  bool
	)
	fs.StringVar(&output, "o", "", "directory to write the source tree into (required)")
	fs.StringVar(&output, "output", "", "directory to write the source tree into (required)")
	fs.BoolVar(&force, "force", false, "write into the destination even if it already contains files")
	fs.BoolVar(&quiet, "quiet", false, "print nothing on success")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `slidepack unpack - recover the source directory from a packed file

USAGE
  slidepack unpack <presentation.html> -o <directory> [options]

The payload digest and every per-file digest are verified before anything is
written. Archive paths are treated as untrusted: absolute paths, "..", drive
letters and NUL bytes are rejected, and slidepack refuses to write through a
symbolic link. When the destination does not exist, files are built in a
staging directory and moved into place, so a failure leaves nothing behind.

Source timestamps and ownership are deliberately not restored; they are not
part of the canonical source representation. Permission bits are.

OPTIONS
`)
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, `
EXAMPLES
  slidepack unpack deck.html -o ./deck
  slidepack unpack deck.html -o ./deck --force
`)
	}
	if err := fs.Parse(permute(fs, args)); err != nil {
		return exitUsage
	}
	rest := fs.Args()
	if len(rest) == 0 {
		errorf("unpack needs a packed HTML file\n\nUsage: slidepack unpack <presentation.html> -o <directory>")
		return exitUsage
	}
	if len(rest) > 1 {
		errorf("unpack takes exactly one packed file (got %d)", len(rest))
		return exitUsage
	}
	if output == "" {
		errorf("unpack needs an output directory\n\nUsage: slidepack unpack <presentation.html> -o <directory>")
		return exitUsage
	}

	pkg, err := unpack.OpenFile(rest[0], unpack.Options{})
	if err != nil {
		var ue *unpack.Error
		if errors.As(err, &ue) {
			errorf("%s: %s [%s]", rest[0], ue.Error(), ue.Code)
			return exitInvalid
		}
		return fail(err)
	}

	res, err := unpack.Extract(pkg, output, unpack.ExtractOptions{Force: force})
	if err != nil {
		return fail(err)
	}

	if !quiet {
		fmt.Printf("Restored %d file%s (%s) to %s\n",
			res.FileCount, plural(res.FileCount), inspect.HumanBytes(res.TotalBytes), output)
		fmt.Printf("Entrypoint: %s\n", pkg.Manifest.Entrypoint)
	}
	return exitOK
}
