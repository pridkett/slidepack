// Command slidepack packs a presentation source directory into a single
// self-contained HTML file, and unpacks it again.
//
// The directory is the source of truth; the HTML file is a compiled
// distribution artifact. See README.md for the mental model and
// docs/format-v1.md for the file format.
package main

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"runtime/debug"
	"strings"

	"github.com/pwagstro/slidepack/internal/cli"
	"github.com/pwagstro/slidepack/internal/manifest"
)

// Exit codes. These are part of the CLI contract and are published by
// `slidepack help --json`.
const (
	exitOK      = 0 // success, or a valid target
	exitError   = 1 // operational failure: I/O, corruption, refusal
	exitUsage   = 2 // the command line itself was wrong
	exitInvalid = 3 // the target was readable but is not valid
)

func exitCodes() []cli.ExitCode {
	return []cli.ExitCode{
		{Code: exitOK, Name: "ok", Summary: "Success, or the target is valid"},
		{Code: exitError, Name: "error", Summary: "Operational failure: I/O, corruption, or a refused overwrite"},
		{Code: exitUsage, Name: "usage", Summary: "The command line was wrong"},
		{Code: exitInvalid, Name: "invalid", Summary: "The target was readable but is not valid"},
	}
}

// pseudoVersion matches the synthetic versions the module system invents for
// untagged commits: v0.0.0-20260824174750-fa0d4fb820c6.
var pseudoVersion = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+-[0-9]{14}-[0-9a-f]{12}`)

// version is overridden at build time with
//
//	go build -ldflags "-X main.version=1.2.3" ./cmd/slidepack
var version = ""

// resolveVersion falls back to the module's VCS stamp, then to "dev", so a
// `go install`ed binary still reports something meaningful.
func resolveVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		// A real tagged version is worth reporting; a pseudo-version is not.
		// "v0.0.0-20260824174750-fa0d4fb820c6" tells a user nothing they can
		// act on, so fall through to the shorter VCS stamp below.
		if v := info.Main.Version; v != "" && v != "(devel)" && !pseudoVersion.MatchString(v) {
			return v
		}
		var rev, modified string
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value
			case "vcs.modified":
				modified = s.Value
			}
		}
		if rev != "" {
			short := rev
			if len(short) > 12 {
				short = short[:12]
			}
			if modified == "true" {
				short += "-dirty"
			}
			return "dev+" + short
		}
	}
	return "dev"
}

func generatorString() string { return "slidepack/" + resolveVersion() }

/* ------------------------------------------------------------------ */
/* Global options                                                      */
/* ------------------------------------------------------------------ */

// globalOptions are accepted by every command. They are appended to each
// command's own options so that the parser, the help renderer and the JSON
// interface all see one list.
func globalOptions() []cli.Option {
	return []cli.Option{
		{
			Name: "color", Type: cli.TypeString, Placeholder: "when",
			Default: "auto", Choices: []string{"auto", "always", "never"}, Global: true,
			Summary: "When to colorize output",
			Details: "auto colours only when writing to a terminal. NO_COLOR in the environment always wins.",
		},
		{
			Name: "no-color", Type: cli.TypeBoolean, Global: true,
			Summary: "Disable colour; the same as --color never",
		},
		{
			Name: "help", Short: "h", Type: cli.TypeBoolean, Global: true,
			Summary: "Show help for this command and exit",
		},
	}
}

// withGlobals returns a command with the global options appended.
func withGlobals(c *cli.Command) *cli.Command {
	c.Options = append(c.Options, globalOptions()...)
	return c
}

/* ------------------------------------------------------------------ */
/* Application                                                         */
/* ------------------------------------------------------------------ */

func buildApp() *cli.App {
	app := &cli.App{
		Name:          "slidepack",
		Version:       resolveVersion(),
		FormatVersion: manifest.Version,
		Summary:       "reversible self-contained HTML presentations",
		Tagline:       "Edit a directory. Pack it when you want one file to distribute.",
		Description: `A presentation is an ordinary directory of HTML, CSS, JavaScript, images and fonts. slidepack compiles that directory into a single .html file that opens straight from the filesystem — no server, no browser extension, no companion folder, no network — and expands it back into the original directory whenever you want.

The directory is source. The HTML file is a build artifact. Keep the directory under version control and treat the .html as generated output.`,
		Usage: []string{
			"<command> [options]",
		},
		GlobalOptions: globalOptions(),
		ExitCodes:     exitCodes(),
		Examples: []cli.Example{
			{Summary: "Build one distributable file from a directory",
				Command: "slidepack pack ./quarterly-review -o quarterly-review.html"},
			{Summary: "Check a presentation before packing it",
				Command: "slidepack validate ./quarterly-review"},
			{Summary: "See what is inside a packed file, without expanding it",
				Command: "slidepack inspect quarterly-review.html"},
			{Summary: "Get the source directory back",
				Command: "slidepack unpack quarterly-review.html -o ./restored"},
		},
		Resources: []cli.Resource{
			{Name: "docs/source-format.md", Summary: "what a presentation directory may contain"},
			{Name: "docs/format-v1.md", Summary: "the packed file format, in full"},
		},
	}

	app.Commands = []*cli.Command{
		packCommand(),
		unpackCommand(),
		validateCommand(),
		inspectCommand(),
		versionCommand(),
		helpCommand(),
	}
	return app
}

/* ------------------------------------------------------------------ */
/* Entry point                                                         */
/* ------------------------------------------------------------------ */

func main() {
	os.Exit(run(os.Args[1:]))
}

// stdout and stderr are indirected so tests can capture them.
var (
	stdout = os.Stdout
	stderr = os.Stderr
)

func run(args []string) int {
	app := buildApp()

	// Colour is resolved before dispatch so that even a usage error, printed
	// before any command runs, is styled the way the user asked for.
	mode := preScanColor(args)
	env := &cli.Env{
		Out:       stdout,
		Err:       stderr,
		Style:     cli.NewPalette(stdout, mode),
		ErrStyle:  cli.NewPalette(stderr, mode),
		App:       app,
		ColorMode: mode,
	}

	if len(args) == 0 {
		cli.RenderAppHelp(env.Out, env.Style, app)
		return exitUsage
	}

	head, rest := args[0], args[1:]

	// Root-level switches that are not commands.
	switch head {
	case "-h", "--help":
		return dispatch(env, mustLookup(app, "help"), rest)
	case "-v", "-V", "--version":
		return dispatch(env, mustLookup(app, "version"), rest)
	}

	if strings.HasPrefix(head, "-") {
		cli.RenderUsageError(env.Err, env.ErrStyle, app, &cli.UsageError{
			Message: fmt.Sprintf("unknown option %s", head),
			Hint:    "options come after a command, as in: slidepack pack ./deck -o deck.html",
		})
		return exitUsage
	}

	cmd, ok := app.Lookup(head)
	if !ok {
		cli.RenderUnknownCommand(env.Err, env.ErrStyle, app, head)
		return exitUsage
	}
	return dispatch(env, cmd, rest)
}

// dispatch parses a command's arguments and runs it.
func dispatch(env *cli.Env, cmd *cli.Command, args []string) int {
	values, err := cli.Parse(cmd, args)
	if err != nil {
		return reportUsage(env, err)
	}

	// --help on any command short-circuits to that command's help, which is
	// what makes every command self-describing without a special case in each.
	if values.Bool("help") {
		cli.RenderCommandHelp(env.Out, env.Style, env.App, cmd)
		return exitOK
	}

	if err := cli.CheckRequired(cmd, values); err != nil {
		return reportUsage(env, err)
	}
	if err := cli.CheckArgs(cmd, values); err != nil {
		return reportUsage(env, err)
	}
	return cmd.Run(env, values)
}

func reportUsage(env *cli.Env, err error) int {
	var ue *cli.UsageError
	if errors.As(err, &ue) {
		cli.RenderUsageError(env.Err, env.ErrStyle, env.App, ue)
		return exitUsage
	}
	fmt.Fprintf(env.Err, "%s %v\n", env.ErrStyle.Error("slidepack:"), err)
	return exitUsage
}

func mustLookup(app *cli.App, name string) *cli.Command {
	c, ok := app.Lookup(name)
	if !ok {
		panic("slidepack: missing built-in command " + name)
	}
	return c
}

// preScanColor finds the colour setting before the real parse, so that a
// parse *failure* is still rendered with the user's chosen colour policy.
//
// It is deliberately forgiving: an unrecognised value is ignored here and
// reported properly by the real parser a moment later.
func preScanColor(args []string) cli.ColorMode {
	for i, a := range args {
		switch {
		case a == "--no-color", a == "-no-color":
			return cli.ColorNever
		case a == "--color", a == "-color":
			if i+1 < len(args) {
				if m, ok := cli.ParseColorMode(args[i+1]); ok {
					return m
				}
			}
		case strings.HasPrefix(a, "--color="), strings.HasPrefix(a, "-color="):
			if m, ok := cli.ParseColorMode(a[strings.IndexByte(a, '=')+1:]); ok {
				return m
			}
		}
	}
	return cli.ColorAuto
}

// applyColor re-resolves the palettes once a command's own options are known.
// The pre-scan handles the common spellings; this covers the rest.
func applyColor(env *cli.Env, v *cli.Values) {
	mode := env.ColorMode
	if v.Bool("no-color") {
		mode = cli.ColorNever
	} else if m, ok := cli.ParseColorMode(v.String("color")); ok && v.String("color") != "" {
		mode = m
	}
	env.ColorMode = mode
	env.Style = cli.NewPalette(env.Out, mode)
	env.ErrStyle = cli.NewPalette(env.Err, mode)
}

/* ------------------------------------------------------------------ */
/* Shared helpers                                                      */
/* ------------------------------------------------------------------ */

// errorf prints a human-facing error to stderr. Machine-readable output always
// goes to stdout, so a --json consumer never sees these lines.
func errorf(env *cli.Env, format string, args ...any) {
	fmt.Fprintf(env.Err, "%s %s\n", env.ErrStyle.Error("slidepack:"), fmt.Sprintf(format, args...))
}

// fail reports err and returns the operational exit code.
func fail(env *cli.Env, err error) int {
	if err == nil {
		return exitOK
	}
	errorf(env, "%v", err)
	return exitError
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func versionCommand() *cli.Command {
	return withGlobals(&cli.Command{
		Name:    "version",
		Summary: "Print the slidepack version and the format version it writes",
		Description: `Prints the version of this executable and the packed format version it reads and writes.

The format version matters more than the tool version: a file packed by any build that writes format 1 can be read by any build that reads format 1.`,
		Usage: []string{"", "--json"},
		Options: []cli.Option{
			{Name: "json", Type: cli.TypeBoolean, Summary: "Emit machine-readable JSON on stdout"},
		},
		Examples: []cli.Example{
			{Summary: "Print the version", Command: "slidepack version"},
			{Summary: "Read the version from a script", Command: "slidepack version --json"},
		},
		SeeAlso: []string{"help"},
		Run: func(env *cli.Env, v *cli.Values) int {
			applyColor(env, v)
			if v.Bool("json") {
				if err := cli.WriteJSON(env.Out, map[string]any{
					"name":          "slidepack",
					"version":       resolveVersion(),
					"formatVersion": manifest.Version,
				}); err != nil {
					return fail(env, err)
				}
				return exitOK
			}
			p := env.Style
			fmt.Fprintf(env.Out, "%s %s\n", p.Title("slidepack"), p.Value(resolveVersion()))
			fmt.Fprintf(env.Out, "%s %s\n", p.Muted("packed format version"), p.Value(fmt.Sprint(manifest.Version)))
			return exitOK
		},
	})
}
