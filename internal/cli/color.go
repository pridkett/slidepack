// Package cli provides slidepack's command-line presentation layer: colour
// handling, a declarative command specification, flag parsing driven by that
// specification, and both human and machine-readable help rendering.
//
// The specification is the single source of truth. Parsing, `--help` and
// `help --json` are all generated from it, so the three cannot drift apart —
// a flag that exists is a flag that is documented.
package cli

import (
	"io"
	"os"
	"strings"
)

// ColorMode is the resolved --color setting.
type ColorMode int

const (
	// ColorAuto colours output when the destination is a terminal.
	ColorAuto ColorMode = iota
	// ColorAlways colours output unconditionally.
	ColorAlways
	// ColorNever disables colour.
	ColorNever
)

// ParseColorMode interprets a --color value.
func ParseColorMode(s string) (ColorMode, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto", "tty", "if-tty":
		return ColorAuto, true
	case "always", "force", "yes", "true", "on":
		return ColorAlways, true
	case "never", "none", "no", "false", "off":
		return ColorNever, true
	}
	return ColorAuto, false
}

// Palette renders styled text, or plain text when colour is disabled.
//
// Every method is a no-op when colour is off, so callers never branch on it;
// the decision is made once, at construction.
type Palette struct{ on bool }

// NewPalette resolves whether to colour output for w.
//
// The rules, in order:
//
//	--color=never                  off
//	NO_COLOR set (any value)       off   (https://no-color.org)
//	--color=always                 on
//	CLICOLOR_FORCE set and not "0" on
//	TERM=dumb                      off
//	w is not a terminal            off
//	otherwise                      on
//
// NO_COLOR is checked before --color=always deliberately: a user who has set
// it globally has expressed a durable preference, and a default-on flag value
// should not override it. An explicit --color=always still wins over the
// terminal test, which is what makes piping into a pager useful.
func NewPalette(w io.Writer, mode ColorMode) *Palette {
	if mode == ColorNever {
		return &Palette{}
	}
	if _, noColor := os.LookupEnv("NO_COLOR"); noColor {
		return &Palette{}
	}
	if mode == ColorAlways {
		return &Palette{on: true}
	}
	if force := os.Getenv("CLICOLOR_FORCE"); force != "" && force != "0" {
		return &Palette{on: true}
	}
	if os.Getenv("TERM") == "dumb" {
		return &Palette{}
	}
	return &Palette{on: isTerminal(w)}
}

// isTerminal reports whether w is a character device.
//
// Checking the file mode avoids a terminal-detection dependency and behaves
// correctly on Windows, where Go's os package already maps console handles to
// ModeCharDevice.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// Enabled reports whether this palette emits escape sequences.
func (p *Palette) Enabled() bool { return p != nil && p.on }

func (p *Palette) wrap(code, s string) string {
	if !p.Enabled() || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

/* --- primitives ------------------------------------------------------- */

func (p *Palette) Bold(s string) string      { return p.wrap("1", s) }
func (p *Palette) Dim(s string) string       { return p.wrap("2", s) }
func (p *Palette) Italic(s string) string    { return p.wrap("3", s) }
func (p *Palette) Underline(s string) string { return p.wrap("4", s) }

func (p *Palette) Red(s string) string     { return p.wrap("31", s) }
func (p *Palette) Green(s string) string   { return p.wrap("32", s) }
func (p *Palette) Yellow(s string) string  { return p.wrap("33", s) }
func (p *Palette) Blue(s string) string    { return p.wrap("34", s) }
func (p *Palette) Magenta(s string) string { return p.wrap("35", s) }
func (p *Palette) Cyan(s string) string    { return p.wrap("36", s) }

func (p *Palette) BoldRed(s string) string   { return p.wrap("1;31", s) }
func (p *Palette) BoldGreen(s string) string { return p.wrap("1;32", s) }
func (p *Palette) BoldCyan(s string) string  { return p.wrap("1;36", s) }

/* --- semantic roles ---------------------------------------------------- */
//
// Call sites use these rather than the primitives, so the palette can be
// re-tuned in one place and so a reader can tell what a colour *means*.

// Heading is a section title such as "USAGE" or "OPTIONS".
func (p *Palette) Heading(s string) string { return p.wrap("1;4", s) }

// Title is the product or command name in a header line.
func (p *Palette) Title(s string) string { return p.BoldCyan(s) }

// Command names a subcommand.
func (p *Palette) Command(s string) string { return p.Cyan(s) }

// Flag names an option.
func (p *Palette) Flag(s string) string { return p.Green(s) }

// Arg names a positional argument or a value placeholder.
func (p *Palette) Arg(s string) string { return p.Yellow(s) }

// Path names a file or directory.
func (p *Palette) Path(s string) string { return p.Magenta(s) }

// Code is inline literal text: a shell command, an element, a digest.
func (p *Palette) Code(s string) string { return p.Cyan(s) }

// Muted is secondary detail that should recede.
func (p *Palette) Muted(s string) string { return p.Dim(s) }

// Error marks a failure.
func (p *Palette) Error(s string) string { return p.BoldRed(s) }

// Warn marks an advisory finding.
func (p *Palette) Warn(s string) string { return p.Yellow(s) }

// Success marks a good outcome.
func (p *Palette) Success(s string) string { return p.BoldGreen(s) }

// Value is a piece of reported data, as opposed to its label.
func (p *Palette) Value(s string) string { return p.Bold(s) }

/* --- glyphs ------------------------------------------------------------ */
//
// Unicode marks read well in a modern terminal and badly in one that cannot
// render them. Falling back to ASCII when colour is off is a good proxy: the
// environments that reject escape sequences (pipes, dumb terminals, CI logs)
// are the same ones where a bare "x" is safer than a heavy ballot cross.

// MarkOK is the success glyph.
func (p *Palette) MarkOK() string {
	if p.Enabled() {
		return p.Green("✓")
	}
	return "ok:"
}

// MarkError is the failure glyph.
func (p *Palette) MarkError() string {
	if p.Enabled() {
		return p.Red("✗")
	}
	return "error:"
}

// MarkWarn is the advisory glyph.
func (p *Palette) MarkWarn() string {
	if p.Enabled() {
		return p.Yellow("!")
	}
	return "warning:"
}

// Bullet introduces a list item.
func (p *Palette) Bullet() string {
	if p.Enabled() {
		return p.Dim("•")
	}
	return "-"
}

// Prompt introduces an example command line.
func (p *Palette) Prompt() string { return p.Dim("$") }
