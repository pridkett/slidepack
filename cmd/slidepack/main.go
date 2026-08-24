// Command slidepack packs a presentation source directory into a single
// self-contained HTML file, and unpacks it again.
//
// The directory is the source of truth; the HTML file is a compiled
// distribution artifact. See README.md for the mental model and
// docs/format-v1.md for the file format.
package main

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
)

// Exit codes. These are part of the CLI contract.
const (
	exitOK      = 0 // success, or a valid target
	exitError   = 1 // operational failure: I/O, corruption, refusal
	exitUsage   = 2 // the command line itself was wrong
	exitInvalid = 3 // the target was read successfully but is not valid
)

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
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			return info.Main.Version
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

func generatorString() string {
	return "slidepack/" + resolveVersion()
}

type command struct {
	name    string
	summary string
	run     func(args []string) int
}

func commands() []command {
	return []command{
		{"pack", "Build a single self-contained HTML file from a source directory", runPack},
		{"unpack", "Recover the source directory from a packed HTML file", runUnpack},
		{"validate", "Check a source directory or a packed file against the format v1 contract", runValidate},
		{"inspect", "Report on a packed file's contents without expanding it", runInspect},
		{"version", "Print the slidepack version", runVersion},
	}
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage(os.Stdout)
		return exitUsage
	}
	switch args[0] {
	case "-h", "--help", "help":
		if len(args) > 1 {
			for _, c := range commands() {
				if c.name == args[1] {
					return c.run([]string{"--help"})
				}
			}
			fmt.Fprintf(os.Stderr, "slidepack: unknown command %q\n", args[1])
			return exitUsage
		}
		usage(os.Stdout)
		return exitOK
	case "-v", "--version":
		return runVersion(nil)
	}
	if strings.HasPrefix(args[0], "-") {
		fmt.Fprintf(os.Stderr, "slidepack: unknown option %q\n\nRun 'slidepack --help' for usage.\n", args[0])
		return exitUsage
	}
	for _, c := range commands() {
		if c.name == args[0] {
			return c.run(args[1:])
		}
	}
	fmt.Fprintf(os.Stderr, "slidepack: unknown command %q\n\nRun 'slidepack --help' for usage.\n", args[0])
	return exitUsage
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `slidepack %s - reversible self-contained HTML presentations

A presentation is an ordinary directory of HTML, CSS, JavaScript, images and
fonts. slidepack compiles that directory into one .html file that opens from
the filesystem with no server, no extension and no network, and expands it
again on demand.

  The directory is source. The HTML file is a build artifact.

USAGE
  slidepack <command> [options]

COMMANDS
`, resolveVersion())
	for _, c := range commands() {
		fmt.Fprintf(w, "  %-9s %s\n", c.name, c.summary)
	}
	fmt.Fprintf(w, `
EXAMPLES
  slidepack pack ./quarterly-review -o quarterly-review.html
  slidepack validate ./quarterly-review
  slidepack inspect quarterly-review.html
  slidepack unpack quarterly-review.html -o ./restored

Run 'slidepack <command> --help' for the options of a single command.
Options accept one or two leading dashes, and may appear before or after the
positional arguments.

EXIT CODES
  %d  success
  %d  operational failure (I/O, corruption, refused overwrite)
  %d  usage error
  %d  the target was readable but is not valid

SECURITY
  A packed presentation is HTML and JavaScript. Opening one executes the
  JavaScript it contains with the privileges a browser grants any local
  document. Only open presentations from sources you trust.
`, exitOK, exitError, exitUsage, exitInvalid)
}

func runVersion(_ []string) int {
	fmt.Printf("slidepack %s\n", resolveVersion())
	fmt.Printf("format version 1\n")
	return exitOK
}

// errorf prints a human-facing error to stderr. Machine-readable output always
// goes to stdout, so --json consumers never see these lines.
func errorf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "slidepack: "+format+"\n", args...)
}

// fail reports err and returns the appropriate exit code.
func fail(err error) int {
	if err == nil {
		return exitOK
	}
	errorf("%v", err)
	return exitError
}
