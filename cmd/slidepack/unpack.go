package main

import (
	"errors"
	"fmt"

	"github.com/pwagstro/slidepack/internal/cli"
	"github.com/pwagstro/slidepack/internal/inspect"
	"github.com/pwagstro/slidepack/internal/unpack"
)

func unpackCommand() *cli.Command {
	return withGlobals(&cli.Command{
		Name:    "unpack",
		Summary: "Recover the source directory from a packed HTML file",
		Description: `Expands a packed presentation back into the ordinary directory it was built from.

Every regular file, its relative path, its exact bytes and its Unix permission bits are restored. Modification times, ownership and empty directories are deliberately not restored: they are not part of the canonical source representation.

This is the intended way to edit a presentation you only have as a packed file. Unpack it, change the source, pack it again.`,
		Usage: []string{
			"<file.html> --output <directory> [options]",
		},
		Arguments: []cli.Argument{
			{Name: "file.html", Summary: "The packed presentation to expand.", Required: true},
		},
		Options: []cli.Option{
			{
				Name: "output", Short: "o", Type: cli.TypeString, Placeholder: "directory",
				Required: true,
				Summary:  "Directory to write the source tree into",
				Details:  "Created if it does not exist. Parent directories are created as needed.",
			},
			{
				Name: "force", Type: cli.TypeBoolean,
				Summary: "Write into the destination even if it already contains files",
				Details: "Existing files with the same names are overwritten in place; unrelated files are left alone.",
			},
			{
				Name: "quiet", Short: "q", Type: cli.TypeBoolean,
				Summary: "Print nothing on success",
			},
		},
		Notes: []cli.Note{
			{
				Title: "Verification",
				Body: `The payload digest and every per-file digest are checked before a single byte is written. A truncated or altered file is reported rather than partially extracted.

These digests detect corruption. They are not signatures and say nothing about who produced the file.`,
			},
			{
				Title: "Safety",
				Body: `Archive paths are treated as untrusted throughout. Absolute paths, "..", Windows drive letters, backslashes, NUL bytes and control characters are all rejected, resolved destinations are re-checked for containment, and slidepack refuses to write through a symbolic link.

When the destination does not exist, files are built in a staging directory and moved into place, so a failure leaves nothing behind. Extracting into an existing directory with --force does not have that protection; do not use it on a directory untrusted local processes can write to.`,
			},
		},
		Examples: []cli.Example{
			{Summary: "Recover the source of a deck you were sent",
				Command: "slidepack unpack quarterly-review.html -o ./quarterly-review"},
			{Summary: "Re-extract over a previous checkout",
				Command: "slidepack unpack deck.html -o ./deck --force"},
			{Summary: "Round-trip a deck to confirm it is intact",
				Command: "slidepack unpack deck.html -o /tmp/check && diff -r ./deck /tmp/check"},
		},
		SeeAlso: []string{"pack", "inspect", "validate"},
		Run:     runUnpack,
	})
}

func runUnpack(env *cli.Env, v *cli.Values) int {
	applyColor(env, v)
	p := env.Style
	input := v.Args()[0]
	output := v.String("output")

	pkg, err := unpack.OpenFile(input, unpack.Options{})
	if err != nil {
		var ue *unpack.Error
		if errors.As(err, &ue) {
			reportPackageError(env, input, ue)
			return exitInvalid
		}
		return fail(env, err)
	}

	res, err := unpack.Extract(pkg, output, unpack.ExtractOptions{Force: v.Bool("force")})
	if err != nil {
		return fail(env, err)
	}

	if v.Bool("quiet") {
		return exitOK
	}
	fmt.Fprintf(env.Out, "%s Restored %s file%s %s to %s\n",
		p.MarkOK(),
		p.Value(fmt.Sprint(res.FileCount)), plural(res.FileCount),
		p.Muted("("+inspect.HumanBytes(res.TotalBytes)+")"),
		p.Path(output),
	)
	fmt.Fprintf(env.Out, "  %s %s\n", p.Muted("entrypoint"), p.Path(pkg.Manifest.Entrypoint))
	return exitOK
}

// reportPackageError renders a failure to read a packed file, including the
// stable code and, where one exists, the catalogued remedy.
func reportPackageError(env *cli.Env, target string, ue *unpack.Error) {
	p := env.ErrStyle
	fmt.Fprintf(env.Err, "%s %s\n", p.Error("slidepack:"), p.Path(target))
	fmt.Fprintf(env.Err, "  %s %s\n", p.Error(string(ue.Code)), ue.Error())
	if info, ok := describeCode(ue.Code); ok && info.Remedy != "" {
		fmt.Fprintf(env.Err, "  %s %s\n", p.Muted("remedy:"), p.Muted(info.Remedy))
	}
}
