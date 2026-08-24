package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/pwagstro/slidepack/internal/diag"
	"github.com/pwagstro/slidepack/internal/source"
	"github.com/pwagstro/slidepack/internal/validate"
)

func runValidate(args []string) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		asJSON bool
		entry  string
		strict bool
	)
	fs.BoolVar(&asJSON, "json", false, "emit machine-readable JSON on stdout")
	fs.StringVar(&entry, "entry", source.DefaultEntrypoint, "package path of the entry document (source directories only)")
	fs.BoolVar(&strict, "strict", false, "treat warnings as failures")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `slidepack validate - check a presentation against the format v1 contract

USAGE
  slidepack validate <directory|presentation.html> [options]

Validating a directory checks the entrypoint, path legality, filesystem object
types, missing local resources, remote rendering dependencies and unsupported
HTML/CSS/JavaScript constructs.

Validating a packed file checks all of that, plus the envelope, the manifest,
the format version, the base64 payload, the payload digest, the gzip stream,
the tar structure, manifest/archive agreement and every per-file digest. The
recovered tree is then validated in memory, exactly as a directory would be.

No presentation code is ever executed.

OPTIONS
`)
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
OUTPUT
  Exit %d when valid, %d when not, %d on a usage error and %d when the target
  could not be read at all. With --json, stdout carries only the JSON document
  and every human-facing message goes to stderr.

DIAGNOSTIC CODES
  Codes are stable and documented in docs/format-v1.md.
`, exitOK, exitInvalid, exitUsage, exitError)
	}
	if err := fs.Parse(permute(fs, args)); err != nil {
		return exitUsage
	}
	rest := fs.Args()
	if len(rest) != 1 {
		errorf("validate needs exactly one directory or packed file")
		return exitUsage
	}
	target := rest[0]

	info, err := os.Stat(target)
	if err != nil {
		return fail(err)
	}

	var res *diag.Result
	if info.IsDir() {
		tree, err := source.LoadDiskTree(target)
		if err != nil {
			// A symlink or special file makes the whole tree unpackable; report
			// it as a diagnostic rather than a bare error so --json still works.
			res = diag.NewResult(target, "source")
			res.Errorf(specialFileCode(err), "", 0, "", "%v", err)
		} else {
			res = validate.Tree(tree, validate.Options{Entrypoint: entry})
			res.Target = target
		}
	} else {
		data, err := os.ReadFile(target)
		if err != nil {
			return fail(err)
		}
		res = validate.Packed(data, target)
	}

	valid := res.Valid && (!strict || len(res.Warnings) == 0)
	res.Valid = valid

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			return fail(err)
		}
	} else {
		printResult(os.Stdout, res)
	}
	if !valid {
		return exitInvalid
	}
	return exitOK
}

func specialFileCode(err error) diag.Code {
	var sf *source.ErrSpecialFile
	if asErr(err, &sf) {
		if strings.Contains(sf.Kind, "symbolic link") {
			return diag.Symlink
		}
		return diag.SpecialFile
	}
	return diag.Unreadable
}

// asErr is errors.As specialised to a concrete pointer type, so callers do not
// need to spell out the two-line dance every time.
func asErr[T error](err error, target *T) bool {
	for err != nil {
		if v, ok := err.(T); ok {
			*target = v
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func printResult(w io.Writer, res *diag.Result) {
	kind := res.Kind
	if kind == "" {
		kind = "target"
	}
	if res.Valid && len(res.Warnings) == 0 {
		fmt.Fprintf(w, "%s: valid slidepack %s\n", res.Target, kind)
		return
	}
	if res.Valid {
		fmt.Fprintf(w, "%s: valid slidepack %s, with %d warning%s\n\n", res.Target, kind, len(res.Warnings), plural(len(res.Warnings)))
	} else {
		fmt.Fprintf(w, "%s: not a valid slidepack %s\n\n", res.Target, kind)
	}
	printDiagnostics(w, res)
}

// printDiagnostics renders errors then warnings, one block each.
func printDiagnostics(w io.Writer, res *diag.Result) {
	section := func(label string, items []diag.Diagnostic) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(w, "%s (%d)\n", label, len(items))
		for _, d := range items {
			loc := d.Path
			if loc == "" {
				loc = "(package)"
			}
			if d.Line > 0 {
				loc = fmt.Sprintf("%s:%d", loc, d.Line)
			}
			fmt.Fprintf(w, "  %-24s %s\n", d.Code, loc)
			for _, line := range wrap(d.Message, 72) {
				fmt.Fprintf(w, "      %s\n", line)
			}
			fmt.Fprintln(w)
		}
	}
	section("ERRORS", res.Errors)
	section("WARNINGS", res.Warnings)
}

// wrap breaks a message into lines no longer than width, on word boundaries.
func wrap(s string, width int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	cur := words[0]
	for _, wd := range words[1:] {
		if len(cur)+1+len(wd) > width {
			lines = append(lines, cur)
			cur = wd
			continue
		}
		cur += " " + wd
	}
	return append(lines, cur)
}
