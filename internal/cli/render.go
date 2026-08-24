package cli

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// Layout constants. The width is a compromise: 80 columns is the safe floor,
// but help that wraps hard at 80 in a 200-column terminal looks cramped, so
// the renderer uses the terminal width when it can discover one.
const (
	minWidth     = 60
	maxWidth     = 92
	defaultWidth = 80
	indent       = "  "
)

// Width returns the column budget for rendered help.
func Width() int {
	if v := os.Getenv("COLUMNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return clampWidth(n)
		}
	}
	return defaultWidth
}

func clampWidth(n int) int {
	if n < minWidth {
		return minWidth
	}
	if n > maxWidth {
		return maxWidth
	}
	return n
}

/* --- low-level writing ------------------------------------------------- */

type writer struct {
	w     io.Writer
	p     *Palette
	width int
}

func (x *writer) line(s string)             { fmt.Fprintln(x.w, s) }
func (x *writer) blank()                    { fmt.Fprintln(x.w) }
func (x *writer) printf(f string, a ...any) { fmt.Fprintf(x.w, f, a...) }
func (x *writer) heading(title string)      { x.line(x.p.Heading(strings.ToUpper(title))) }

// para writes a paragraph, wrapped and indented.
func (x *writer) para(prefix, text string) {
	for _, line := range wrapText(text, x.width-len(prefix)) {
		x.line(prefix + line)
	}
}

// paragraphs writes blank-line-separated paragraphs, preserving indented
// blocks (lines beginning with two or more spaces) verbatim so that examples
// and diagrams inside a description survive.
func (x *writer) paragraphs(prefix, text string) {
	blocks := strings.Split(strings.TrimSpace(text), "\n\n")
	for i, block := range blocks {
		if i > 0 {
			x.blank()
		}
		if isPreformatted(block) {
			// An indented block may be a shell command or an indented list.
			// Colouring prose as code makes it look executable, so only lines
			// that begin with the program name get the code treatment.
			for _, line := range strings.Split(block, "\n") {
				line = strings.TrimRight(line, " ")
				if looksLikeCommand(line) {
					x.line(prefix + x.p.Code(line))
					continue
				}
				x.line(prefix + x.p.Muted(line))
			}
			continue
		}
		x.para(prefix, strings.Join(strings.Fields(block), " "))
	}
}

// isPreformatted reports whether every line of a block is indented, which is
// how a description marks a block that must not be reflowed.
func isPreformatted(block string) bool {
	for _, line := range strings.Split(block, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(line, "  ") {
			return false
		}
	}
	return true
}

// wrapText breaks text into lines of at most width columns.
func wrapText(text string, width int) []string {
	if width < 20 {
		width = 20
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	cur := words[0]
	for _, w := range words[1:] {
		if DisplayWidth(cur)+1+DisplayWidth(w) > width {
			lines = append(lines, cur)
			cur = w
			continue
		}
		cur += " " + w
	}
	return append(lines, cur)
}

// column writes a two-column row, wrapping the right column under itself.
//
// The left column is padded to `gutter` *visible* characters. Padding is
// computed from the unstyled text because escape sequences have width zero
// but length greater than zero, and using len() on styled text is the classic
// way to produce a misaligned table.
func (x *writer) column(prefix, left, styledLeft string, gutter int, right string) {
	pad := gutter - DisplayWidth(left)
	if pad < 0 {
		// The label is too wide for the gutter: give it its own line rather
		// than pushing the whole column out of alignment.
		x.line(prefix + styledLeft)
		for _, l := range wrapText(right, x.width-len(prefix)-gutter-2) {
			x.line(prefix + strings.Repeat(" ", gutter+2) + l)
		}
		return
	}
	body := wrapText(right, x.width-len(prefix)-gutter-2)
	x.line(prefix + styledLeft + strings.Repeat(" ", pad) + "  " + body[0])
	for _, l := range body[1:] {
		x.line(prefix + strings.Repeat(" ", gutter+2) + l)
	}
}

/* --- app help ---------------------------------------------------------- */

// RenderAppHelp writes the top-level help.
func RenderAppHelp(w io.Writer, p *Palette, app *App) {
	x := &writer{w: w, p: p, width: Width()}

	x.printf("%s %s  %s\n", p.Title(app.Name), p.Muted(app.Version), p.Muted("— "+app.Summary))
	x.blank()

	if app.Tagline != "" {
		x.line(indent + p.Bold(app.Tagline))
		x.blank()
	}
	if app.Description != "" {
		x.paragraphs(indent, app.Description)
		x.blank()
	}

	x.heading("Usage")
	for _, u := range app.Usage {
		x.line(indent + p.Command(app.Name) + " " + u)
	}
	x.blank()

	x.heading("Commands")
	gutter := 0
	for _, c := range app.VisibleCommands() {
		if len(c.Name) > gutter {
			gutter = len(c.Name)
		}
	}
	for _, c := range app.VisibleCommands() {
		x.column(indent, c.Name, p.Command(c.Name), gutter, c.Summary)
	}
	x.blank()

	if len(app.GlobalOptions) > 0 {
		x.heading("Global options")
		renderOptions(x, app.GlobalOptions)
		x.blank()
	}

	if len(app.Examples) > 0 {
		x.heading("Examples")
		renderExamples(x, app.Examples)
		x.blank()
	}

	if len(app.ExitCodes) > 0 {
		x.heading("Exit codes")
		for _, e := range app.ExitCodes {
			label := strconv.Itoa(e.Code)
			x.column(indent, label, p.Value(label), 3, e.Summary)
		}
		x.blank()
	}

	x.heading("Learn more")
	learn := [][2]string{
		{app.Name + " help <command>", "full help for one command, with examples"},
		{app.Name + " help --json", "the complete interface as machine-readable JSON"},
	}
	for _, r := range app.Resources {
		learn = append(learn, [2]string{r.Name, r.Summary})
	}
	gutter = 0
	for _, l := range learn {
		if len(l[0]) > gutter {
			gutter = len(l[0])
		}
	}
	for i, l := range learn {
		styled := p.Code(l[0])
		if i >= 2 {
			styled = p.Path(l[0])
		}
		x.column(indent, l[0], styled, gutter, l[1])
	}
}

/* --- command help ------------------------------------------------------ */

// RenderCommandHelp writes the full help for one command.
func RenderCommandHelp(w io.Writer, p *Palette, app *App, c *Command) {
	x := &writer{w: w, p: p, width: Width()}

	x.printf("%s  %s\n", p.Title(app.Name+" "+c.Name), p.Muted("— "+c.Summary))
	x.blank()

	x.heading("Usage")
	for _, u := range c.Usage {
		x.line(indent + p.Command(app.Name+" "+c.Name) + " " + colorizeUsage(p, u))
	}
	x.blank()

	if len(c.Aliases) > 0 {
		x.heading("Aliases")
		x.line(indent + p.Command(strings.Join(c.Aliases, ", ")))
		x.blank()
	}

	if c.Description != "" {
		x.heading("Description")
		x.paragraphs(indent, c.Description)
		x.blank()
	}

	if len(c.Arguments) > 0 {
		x.heading("Arguments")
		gutter := 0
		for _, a := range c.Arguments {
			if n := len(a.Placeholder()); n > gutter {
				gutter = n
			}
		}
		for _, a := range c.Arguments {
			ph := a.Placeholder()
			x.column(indent, ph, p.Arg(ph), gutter, a.Summary)
		}
		x.blank()
	}

	local, global := splitOptions(c.Options)
	if len(local) > 0 {
		x.heading("Options")
		renderOptions(x, local)
		x.blank()
	}
	if len(global) > 0 {
		x.heading("Global options")
		renderOptions(x, global)
		x.blank()
	}

	if len(c.Notes) > 0 {
		for _, n := range c.Notes {
			x.heading(n.Title)
			x.paragraphs(indent, n.Body)
			x.blank()
		}
	}

	if len(c.Examples) > 0 {
		x.heading("Examples")
		renderExamples(x, c.Examples)
		x.blank()
	}

	codes := c.ExitCodes
	if len(codes) == 0 {
		codes = app.ExitCodes
	}
	if len(codes) > 0 {
		x.heading("Exit codes")
		for _, e := range codes {
			label := strconv.Itoa(e.Code)
			x.column(indent, label, p.Value(label), 3, e.Summary)
		}
		x.blank()
	}

	if len(c.SeeAlso) > 0 {
		x.heading("See also")
		gutter := 0
		for _, name := range c.SeeAlso {
			if n := len(app.Name + " " + name); n > gutter {
				gutter = n
			}
		}
		for _, name := range c.SeeAlso {
			related, ok := app.Lookup(name)
			if !ok {
				continue
			}
			label := app.Name + " " + name
			x.column(indent, label, p.Command(label), gutter, related.Summary)
		}
	}
}

func splitOptions(opts []Option) (local, global []Option) {
	for _, o := range opts {
		if o.Global {
			global = append(global, o)
		} else {
			local = append(local, o)
		}
	}
	return
}

func renderOptions(x *writer, opts []Option) {
	gutter := 0
	for _, o := range opts {
		if n := len(o.Signature()); n > gutter {
			gutter = n
		}
	}
	if gutter > 28 {
		gutter = 28
	}
	for _, o := range opts {
		sig := o.Signature()
		x.column(indent, sig, styleSignature(x.p, o), gutter, o.Summary+optionSuffix(x.p, o))
		if o.Details != "" {
			x.para(indent+strings.Repeat(" ", gutter+2), x.p.Muted(o.Details))
		}
	}
}

// styleSignature colours the flag names and the value placeholder separately.
func styleSignature(p *Palette, o Option) string {
	flags := o.Flags()
	if o.Type != TypeString {
		return p.Flag(flags)
	}
	ph := o.Placeholder
	if ph == "" {
		ph = "value"
	}
	return p.Flag(flags) + " " + p.Arg("<"+ph+">")
}

// optionSuffix appends the annotations that belong on the summary line.
func optionSuffix(p *Palette, o Option) string {
	// Each annotation is styled on its own and then concatenated. Wrapping a
	// coloured run inside a dim run would not work: the inner reset ends the
	// dim as well, leaving the rest of the line unstyled.
	var parts []string
	if o.Required {
		parts = append(parts, p.Yellow("required"))
	}
	if len(o.Choices) > 0 {
		parts = append(parts, p.Muted("one of "+strings.Join(o.Choices, ", ")))
	}
	if o.Default != "" && o.Default != "false" {
		parts = append(parts, p.Muted("default: "+o.Default))
	}
	if len(parts) == 0 {
		return ""
	}
	return "  " + p.Muted("(") + strings.Join(parts, p.Muted("; ")) + p.Muted(")")
}

func renderExamples(x *writer, examples []Example) {
	for i, ex := range examples {
		if i > 0 {
			x.blank()
		}
		x.line(indent + x.p.Muted(ex.Summary))
		x.line(indent + x.p.Prompt() + " " + x.p.Code(ex.Command))
	}
}

// looksLikeCommand reports whether an indented line is a shell invocation
// rather than prose.
func looksLikeCommand(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "slidepack ") || strings.HasPrefix(t, "$ ")
}

// colorizeUsage highlights <placeholders> and [optional] groups in a usage
// line, so the shape of the command is legible at a glance.
func colorizeUsage(p *Palette, usage string) string {
	if !p.Enabled() {
		return usage
	}
	var b strings.Builder
	i := 0
	for i < len(usage) {
		switch usage[i] {
		case '<':
			if end := strings.IndexByte(usage[i:], '>'); end >= 0 {
				b.WriteString(p.Arg(usage[i : i+end+1]))
				i += end + 1
				continue
			}
		case '-':
			end := i
			for end < len(usage) && usage[end] != ' ' && usage[end] != ']' && usage[end] != '<' {
				end++
			}
			b.WriteString(p.Flag(usage[i:end]))
			i = end
			continue
		case '[', ']', '|':
			b.WriteString(p.Muted(string(usage[i])))
			i++
			continue
		}
		b.WriteByte(usage[i])
		i++
	}
	return b.String()
}

/* --- usage errors ------------------------------------------------------ */

// RenderUsageError writes a usage failure and points at the right help.
func RenderUsageError(w io.Writer, p *Palette, app *App, err *UsageError) {
	x := &writer{w: w, p: p, width: Width()}
	x.printf("%s %s\n", p.Error(app.Name+":"), err.Message)
	if err.Hint != "" {
		x.blank()
		x.para(indent, p.Muted(err.Hint))
	}
	x.blank()
	if err.Command != nil {
		x.line(indent + p.Muted("usage: ") + p.Command(app.Name+" "+err.Command.Name) + " " + colorizeUsage(p, firstUsage(err.Command)))
		x.blank()
		x.line(indent + p.Muted("Run ") + p.Code(app.Name+" help "+err.Command.Name) + p.Muted(" for the full description."))
		return
	}
	x.line(indent + p.Muted("Run ") + p.Code(app.Name+" help") + p.Muted(" to see the available commands."))
}

func firstUsage(c *Command) string {
	if len(c.Usage) == 0 {
		return ""
	}
	return c.Usage[0]
}

// RenderUnknownCommand writes the "no such command" message with a suggestion.
func RenderUnknownCommand(w io.Writer, p *Palette, app *App, name string) {
	x := &writer{w: w, p: p, width: Width()}
	x.printf("%s unknown command %s\n", p.Error(app.Name+":"), p.Value(name))
	x.blank()
	if suggestion := app.SuggestCommand(name); suggestion != "" {
		x.line(indent + p.Muted("Did you mean ") + p.Command(app.Name+" "+suggestion) + p.Muted("?"))
		x.blank()
	}
	x.line(indent + p.Muted("Available commands:"))
	for _, c := range app.VisibleCommands() {
		x.line(indent + indent + p.Command(c.Name))
	}
	x.blank()
	x.line(indent + p.Muted("Run ") + p.Code(app.Name+" help") + p.Muted(" for details."))
}
