package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/pwagstro/slidepack/internal/cli"
	"github.com/pwagstro/slidepack/internal/diag"
	"github.com/pwagstro/slidepack/internal/source"
	"github.com/pwagstro/slidepack/internal/validate"
)

func validateCommand() *cli.Command {
	return withGlobals(&cli.Command{
		Name:    "validate",
		Summary: "Check a source directory or a packed file against the format v1 contract",
		Description: `Reports everything that would stop a presentation from rendering, before you distribute it.

Validating a directory checks the entry document, path legality, filesystem object types, missing local resources, remote rendering dependencies, and unsupported HTML, CSS and JavaScript constructs.

Validating a packed file checks all of that, plus the envelope, the manifest, the format version, the base64 payload, the payload digest, the gzip stream, the tar structure, manifest/archive agreement and every per-file digest. The recovered tree is then validated in memory, exactly as a directory would be.

No presentation code is ever executed. HTML is tokenized, CSS is scanned lexically and JavaScript is only pattern-matched; no JavaScript engine is involved at any point.`,
		Usage: []string{
			"<directory|file.html> [options]",
			"--json <directory|file.html>",
		},
		Arguments: []cli.Argument{
			{Name: "target", Summary: "A presentation source directory, or a packed .html file.", Required: true},
		},
		Options: []cli.Option{
			{
				Name: "json", Type: cli.TypeBoolean,
				Summary: "Emit machine-readable JSON on stdout",
				Details: "stdout carries only the JSON document; every human-facing message goes to stderr.",
			},
			{
				Name: "entry", Type: cli.TypeString, Placeholder: "path",
				Default: source.DefaultEntrypoint,
				Summary: "Package path of the entry document",
				Details: "Source directories only. A packed file names its own entrypoint in the manifest.",
			},
			{
				Name: "strict", Type: cli.TypeBoolean,
				Summary: "Treat warnings as failures",
			},
			{
				Name: "explain", Type: cli.TypeBoolean,
				Summary: "Print the remedy for each diagnostic",
				Details: "Adds a line saying what to change. The same text is in slidepack help --json.",
			},
		},
		Notes: []cli.Note{
			{
				Title: "Diagnostic codes",
				Body: `Every finding carries a stable code such as MISSING_RESOURCE or ES_MODULE. Codes never change meaning, so scripts and agents should match on the code rather than on the message.

The complete catalogue, with a remedy for each code, is published by:

  slidepack help --json`,
			},
			{
				Title: "Errors and warnings",
				Body:  `An error makes the target invalid and exits 3. A warning is advisory and exits 0, unless --strict is given. Local navigation links, dynamic import() and computed resource URLs are warnings, because they may be harmless depending on what the presentation does with them.`,
			},
		},
		Examples: []cli.Example{
			{Summary: "Check a directory before packing", Command: "slidepack validate ./quarterly-review"},
			{Summary: "Check a packed file end to end", Command: "slidepack validate quarterly-review.html"},
			{Summary: "See what to change", Command: "slidepack validate ./deck --explain"},
			{Summary: "Fail a build on warnings too", Command: "slidepack validate ./deck --strict"},
			{Summary: "Drive it from a script or an agent", Command: "slidepack validate --json ./deck"},
			{Summary: "List just the codes",
				Command: "slidepack validate --json ./deck | jq -r '.errors[].code'"},
		},
		SeeAlso: []string{"pack", "inspect"},
		Run:     runValidate,
	})
}

func runValidate(env *cli.Env, v *cli.Values) int {
	applyColor(env, v)
	target := v.Args()[0]

	info, err := os.Stat(target)
	if err != nil {
		return fail(env, err)
	}

	var res *diag.Result
	if info.IsDir() {
		tree, err := source.LoadDiskTree(target)
		if err != nil {
			// A symlink or special file makes the whole tree unpackable.
			// Reporting it as a diagnostic rather than a bare error keeps
			// --json usable for every failure mode.
			res = diag.NewResult(target, "source")
			res.Errorf(specialFileCode(err), "", 0, "", "%v", err)
		} else {
			res = validate.Tree(tree, validate.Options{Entrypoint: v.String("entry")})
			res.Target = target
		}
	} else {
		data, err := os.ReadFile(target)
		if err != nil {
			return fail(env, err)
		}
		res = validate.Packed(data, target)
	}

	strict := v.Bool("strict")
	valid := res.Valid && (!strict || len(res.Warnings) == 0)
	res.Valid = valid

	if v.Bool("json") {
		if err := cli.WriteJSON(env.Out, res); err != nil {
			return fail(env, err)
		}
	} else {
		renderResult(env.Out, env.Style, res, v.Bool("explain"), strict)
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

func describeCode(c diag.Code) (diag.Info, bool) { return diag.Describe(c) }

/* ------------------------------------------------------------------ */
/* Rendering                                                           */
/* ------------------------------------------------------------------ */

// renderResult writes the headline verdict and then the findings.
func renderResult(w io.Writer, p *cli.Palette, res *diag.Result, explain, strict bool) {
	kind := res.Kind
	if kind == "" {
		kind = "target"
	}
	nErr, nWarn := len(res.Errors), len(res.Warnings)

	switch {
	case res.Valid && nWarn == 0:
		fmt.Fprintf(w, "%s %s %s\n", p.MarkOK(), p.Path(res.Target),
			p.Muted("is a valid slidepack "+kind))
		return
	case res.Valid:
		fmt.Fprintf(w, "%s %s %s\n", p.MarkWarn(), p.Path(res.Target),
			p.Muted(fmt.Sprintf("is a valid slidepack %s, with %d warning%s", kind, nWarn, plural(nWarn))))
	default:
		fmt.Fprintf(w, "%s %s %s\n", p.MarkError(), p.Path(res.Target),
			p.Muted("is not a valid slidepack "+kind))
	}
	fmt.Fprintln(w)
	renderDiagnosticsWith(w, p, res, explain)

	// Summary line, so the outcome is legible after a long list.
	var parts []string
	if nErr > 0 {
		parts = append(parts, p.Error(fmt.Sprintf("%d error%s", nErr, plural(nErr))))
	}
	if nWarn > 0 {
		label := fmt.Sprintf("%d warning%s", nWarn, plural(nWarn))
		if strict {
			label += " (--strict: counted as failures)"
		}
		parts = append(parts, p.Warn(label))
	}
	fmt.Fprintf(w, "%s\n", strings.Join(parts, p.Muted(", ")))

	if !explain && (nErr > 0 || nWarn > 0) {
		fmt.Fprintf(w, "%s\n", p.Muted("Run with --explain to see what to change for each code."))
	}
}

// renderDiagnostics writes findings without the verdict line, for callers
// that have already said what happened.
func renderDiagnostics(w io.Writer, p *cli.Palette, res *diag.Result) {
	renderDiagnosticsWith(w, p, res, false)
}

// renderDiagnosticsWith groups findings by file, which is how a person reads
// them: "what is wrong with index.html", not "what MISSING_RESOURCE errors
// exist across the tree".
func renderDiagnosticsWith(w io.Writer, p *cli.Palette, res *diag.Result, explain bool) {
	section := func(label string, items []diag.Diagnostic, mark func() string, tone func(string) string) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(w, "%s\n", p.Heading(label))

		for _, group := range groupByPath(items) {
			location := group.path
			if location == "" {
				location = "(package)"
			}
			fmt.Fprintf(w, "  %s\n", p.Path(location))
			for _, d := range group.items {
				line := ""
				if d.Line > 0 {
					line = p.Muted(fmt.Sprintf(":%d", d.Line))
				}
				detail := ""
				if d.Detail != "" {
					detail = "  " + p.Muted(d.Detail)
				}
				fmt.Fprintf(w, "    %s %s%s%s\n", mark(), tone(string(d.Code)), line, detail)
				for _, l := range wrapAt(d.Message, cli.Width()-8) {
					fmt.Fprintf(w, "        %s\n", l)
				}
				if explain {
					if info, ok := diag.Describe(d.Code); ok && info.Remedy != "" {
						for i, l := range wrapAt(info.Remedy, cli.Width()-16) {
							prefix := "        " + p.Muted("fix: ")
							if i > 0 {
								prefix = "             "
							}
							fmt.Fprintf(w, "%s%s\n", prefix, p.Muted(l))
						}
					}
				}
			}
		}
		fmt.Fprintln(w)
	}

	section("ERRORS", res.Errors, p.MarkError, p.Error)
	section("WARNINGS", res.Warnings, p.MarkWarn, p.Warn)
}

type pathGroup struct {
	path  string
	items []diag.Diagnostic
}

// groupByPath collects diagnostics per file, preserving the sorted order the
// validator already established.
func groupByPath(items []diag.Diagnostic) []pathGroup {
	var groups []pathGroup
	index := map[string]int{}
	for _, d := range items {
		i, ok := index[d.Path]
		if !ok {
			index[d.Path] = len(groups)
			groups = append(groups, pathGroup{path: d.Path})
			i = len(groups) - 1
		}
		groups[i].items = append(groups[i].items, d)
	}
	sort.SliceStable(groups, func(a, b int) bool { return groups[a].path < groups[b].path })
	return groups
}

// wrapAt breaks a message into lines no longer than width, on word boundaries.
func wrapAt(s string, width int) []string {
	if width < 24 {
		width = 24
	}
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
