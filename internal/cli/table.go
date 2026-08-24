package cli

import (
	"fmt"
	"io"
	"strings"
	"unicode"
)

// DisplayWidth returns how many terminal columns a string occupies.
//
// Two things make len() the wrong answer. Escape sequences have length but no
// width, so a styled cell padded by byte count comes out short. And a
// multi-byte rune such as the en dash in "Revenue Chart – Europe.webp" is
// three bytes but one column, so a path column padded by byte count comes out
// long. Both bugs look identical from the outside: a table that does not line
// up. This counts columns.
func DisplayWidth(s string) int {
	width := 0
	forEachVisibleRune(s, func(r rune) {
		switch {
		case r == '\t':
			width += 8
		case unicode.IsControl(r):
			// Contributes nothing.
		case isWideRune(r):
			width += 2
		case isCombining(r):
			// Attaches to the previous cell.
		default:
			width++
		}
	})
	return width
}

// StripStyles removes ANSI escape sequences, leaving the visible text.
func StripStyles(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	forEachVisibleRune(s, func(r rune) { b.WriteRune(r) })
	return b.String()
}

// forEachVisibleRune calls fn for every rune of s that is not part of an ANSI
// escape sequence.
//
// Three states are needed, not two. "[" is both the CSI introducer and a
// character inside the final-byte range, so a two-state scanner ends every
// sequence at its own introducer and then counts the parameters as visible
// text. Getting this wrong produces a table that is subtly misaligned only
// when colour is on, which is a miserable thing to debug -- so it lives in one
// place and every caller shares it.
func forEachVisibleRune(s string, fn func(rune)) {
	const (
		stText = iota
		stEscape
		stCSI
	)
	state := stText
	for _, r := range s {
		switch state {
		case stEscape:
			if r == '[' {
				state = stCSI
			} else {
				// A two-character escape such as ESC c.
				state = stText
			}
			continue
		case stCSI:
			// Parameters are 0x30-0x3F and intermediates 0x20-0x2F; the
			// sequence ends at a final byte in 0x40-0x7E.
			if r >= 0x40 && r <= 0x7e {
				state = stText
			}
			continue
		}
		if r == 0x1b {
			state = stEscape
			continue
		}
		fn(r)
	}
}

// isWideRune reports whether a rune occupies two terminal columns. The ranges
// cover CJK, Hangul, and the emoji blocks a presentation title might use;
// exhaustive East Asian Width data is not worth a dependency here.
func isWideRune(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115F, // Hangul Jamo
		r >= 0x2E80 && r <= 0x303E, // CJK radicals, Kangxi
		r >= 0x3041 && r <= 0x33FF, // Hiragana .. CJK compatibility
		r >= 0x3400 && r <= 0x4DBF, // CJK extension A
		r >= 0x4E00 && r <= 0x9FFF, // CJK unified ideographs
		r >= 0xA000 && r <= 0xA4CF, // Yi
		r >= 0xAC00 && r <= 0xD7A3, // Hangul syllables
		r >= 0xF900 && r <= 0xFAFF, // CJK compatibility ideographs
		r >= 0xFE30 && r <= 0xFE6F, // CJK compatibility forms
		r >= 0xFF00 && r <= 0xFF60, // Fullwidth forms
		r >= 0xFFE0 && r <= 0xFFE6,
		r >= 0x1F300 && r <= 0x1F64F, // Emoji
		r >= 0x1F900 && r <= 0x1F9FF,
		r >= 0x20000 && r <= 0x3FFFD: // CJK extensions B+
		return true
	}
	return false
}

func isCombining(r rune) bool {
	return unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r)
}

// Pad appends spaces so that s occupies width columns.
func Pad(s string, width int) string {
	if n := width - DisplayWidth(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// PadLeft prepends spaces so that s occupies width columns.
func PadLeft(s string, width int) string {
	if n := width - DisplayWidth(s); n > 0 {
		return strings.Repeat(" ", n) + s
	}
	return s
}

// Cell is one table cell: the text, and how to style it.
type Cell struct {
	// Text is the unstyled content. Column widths are measured from this.
	Text string
	// Style renders Text for display. Nil means no styling.
	Style func(string) string
	// Right aligns the cell to the right of its column.
	Right bool
}

// Plain returns an unstyled cell.
func Plain(text string) Cell { return Cell{Text: text} }

// Styled returns a cell rendered with a style function.
func Styled(text string, style func(string) string) Cell {
	return Cell{Text: text, Style: style}
}

// RightCell returns a right-aligned styled cell.
func RightCell(text string, style func(string) string) Cell {
	return Cell{Text: text, Style: style, Right: true}
}

func (c Cell) render() string {
	if c.Style == nil {
		return c.Text
	}
	return c.Style(c.Text)
}

// Table lays out aligned columns.
//
// Widths are measured from the unstyled text and styling is applied
// afterwards, which is what text/tabwriter cannot do: it measures the bytes it
// is given, so a styled cell throws every column to its right out of line.
type Table struct {
	// Indent prefixes every row.
	Indent string
	// Gap is the number of spaces between columns.
	Gap int
	// Header is optional.
	Header []Cell
	// Rows are the body.
	Rows [][]Cell
}

// AddRow appends a row.
func (t *Table) AddRow(cells ...Cell) { t.Rows = append(t.Rows, cells) }

// Render writes the table.
func (t *Table) Render(w io.Writer) {
	gap := t.Gap
	if gap <= 0 {
		gap = 2
	}
	columns := 0
	for _, r := range t.Rows {
		if len(r) > columns {
			columns = len(r)
		}
	}
	if len(t.Header) > columns {
		columns = len(t.Header)
	}
	if columns == 0 {
		return
	}

	widths := make([]int, columns)
	measure := func(row []Cell) {
		for i, c := range row {
			if n := DisplayWidth(c.Text); n > widths[i] {
				widths[i] = n
			}
		}
	}
	if t.Header != nil {
		measure(t.Header)
	}
	for _, r := range t.Rows {
		measure(r)
	}

	writeRow := func(row []Cell) {
		var b strings.Builder
		b.WriteString(t.Indent)
		for i, c := range row {
			rendered := c.render()
			// The final column is never padded: trailing whitespace serves no
			// one and shows up in copied output.
			if i == len(row)-1 {
				b.WriteString(rendered)
				break
			}
			pad := max(widths[i]-DisplayWidth(c.Text), 0)
			if c.Right {
				b.WriteString(strings.Repeat(" ", pad))
				b.WriteString(rendered)
			} else {
				b.WriteString(rendered)
				b.WriteString(strings.Repeat(" ", pad))
			}
			b.WriteString(strings.Repeat(" ", gap))
		}
		fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
	}

	if t.Header != nil {
		writeRow(t.Header)
	}
	for _, r := range t.Rows {
		writeRow(r)
	}
}

// FieldList renders aligned "label  value" pairs.
type FieldList struct {
	Indent string
	// LabelStyle is applied to every label.
	LabelStyle func(string) string
	rows       [][3]string
	styles     []func(string) string
}

// Add appends a field. note is optional trailing detail.
func (f *FieldList) Add(label, value, note string, style func(string) string) {
	f.rows = append(f.rows, [3]string{label, value, note})
	f.styles = append(f.styles, style)
}

// Blank inserts a separating blank line.
func (f *FieldList) Blank() {
	f.rows = append(f.rows, [3]string{"", "", ""})
	f.styles = append(f.styles, nil)
}

// Render writes the field list, aligning values in one column.
func (f *FieldList) Render(w io.Writer, p *Palette) {
	width := 0
	for _, r := range f.rows {
		if n := DisplayWidth(r[0]); n > width {
			width = n
		}
	}
	for i, r := range f.rows {
		if r[0] == "" && r[1] == "" {
			fmt.Fprintln(w)
			continue
		}
		label := r[0]
		if f.LabelStyle != nil {
			label = f.LabelStyle(label)
		}
		label = Pad(label, width)
		value := r[1]
		if f.styles[i] != nil {
			value = f.styles[i](r[1])
		}
		line := f.Indent + label + "  " + value
		if r[2] != "" {
			line += "  " + p.Muted(r[2])
		}
		fmt.Fprintln(w, line)
	}
}
