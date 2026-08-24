package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

/* --- colour ------------------------------------------------------------ */

func TestPaletteRespectsMode(t *testing.T) {
	var buf bytes.Buffer // never a terminal

	if p := NewPalette(&buf, ColorNever); p.Enabled() {
		t.Error("ColorNever should disable colour")
	}
	if p := NewPalette(&buf, ColorAlways); !p.Enabled() {
		t.Error("ColorAlways should enable colour even for a non-terminal")
	}
	if p := NewPalette(&buf, ColorAuto); p.Enabled() {
		t.Error("ColorAuto should disable colour for a non-terminal")
	}
}

func TestNoColorEnvironmentWinsOverColorAlways(t *testing.T) {
	// https://no-color.org: a user who sets this has expressed a durable
	// preference, and a flag default should not be able to override it.
	t.Setenv("NO_COLOR", "1")
	if p := NewPalette(&bytes.Buffer{}, ColorAlways); p.Enabled() {
		t.Error("NO_COLOR must win over --color always")
	}
}

func TestNoColorLosesToColorNeverOrder(t *testing.T) {
	// Both disable colour; this just confirms neither path panics.
	t.Setenv("NO_COLOR", "")
	if p := NewPalette(&bytes.Buffer{}, ColorNever); p.Enabled() {
		t.Error("ColorNever should disable colour")
	}
}

func TestClicolorForceEnablesColor(t *testing.T) {
	t.Setenv("CLICOLOR_FORCE", "1")
	os.Unsetenv("NO_COLOR")
	if p := NewPalette(&bytes.Buffer{}, ColorAuto); !p.Enabled() {
		t.Error("CLICOLOR_FORCE should enable colour")
	}
	t.Setenv("CLICOLOR_FORCE", "0")
	if p := NewPalette(&bytes.Buffer{}, ColorAuto); p.Enabled() {
		t.Error("CLICOLOR_FORCE=0 should not enable colour")
	}
}

func TestDumbTerminalGetsNoColor(t *testing.T) {
	t.Setenv("TERM", "dumb")
	os.Unsetenv("CLICOLOR_FORCE")
	os.Unsetenv("NO_COLOR")
	if p := NewPalette(&bytes.Buffer{}, ColorAuto); p.Enabled() {
		t.Error("TERM=dumb should disable colour")
	}
}

func TestDisabledPaletteIsTransparent(t *testing.T) {
	p := &Palette{}
	if got := p.Error("boom"); got != "boom" {
		t.Errorf("a disabled palette must not alter text, got %q", got)
	}
	if got := p.MarkOK(); strings.Contains(got, "\x1b") {
		t.Errorf("a disabled palette emitted an escape sequence: %q", got)
	}
}

func TestParseColorMode(t *testing.T) {
	cases := map[string]ColorMode{
		"": ColorAuto, "auto": ColorAuto, "AUTO": ColorAuto,
		"always": ColorAlways, "force": ColorAlways,
		"never": ColorNever, "off": ColorNever,
	}
	for in, want := range cases {
		got, ok := ParseColorMode(in)
		if !ok || got != want {
			t.Errorf("ParseColorMode(%q) = (%v, %v), want (%v, true)", in, got, ok, want)
		}
	}
	if _, ok := ParseColorMode("mauve"); ok {
		t.Error("ParseColorMode accepted a nonsense value")
	}
}

/* --- width and layout -------------------------------------------------- */

func TestDisplayWidthIgnoresEscapesAndCountsRunes(t *testing.T) {
	p := &Palette{on: true}
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"abc", 3},
		{p.Red("abc"), 3},
		{p.Bold(p.Red("abc")), 3},
		// An en dash is three bytes and one column: the exact case that made
		// the inspect table misalign.
		{"Revenue Chart – Europe.webp", 27},
		{"日本語", 6},
	}
	for _, c := range cases {
		if got := DisplayWidth(c.in); got != c.want {
			t.Errorf("DisplayWidth(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestPadUsesDisplayWidth(t *testing.T) {
	p := &Palette{on: true}
	got := Pad(p.Red("ab"), 5)
	if DisplayWidth(got) != 5 {
		t.Errorf("Pad produced width %d, want 5", DisplayWidth(got))
	}
	if !strings.HasSuffix(got, "   ") {
		t.Errorf("Pad(%q) = %q; three spaces should follow the styled text", "ab", got)
	}
}

func TestTableAlignsRegardlessOfStyling(t *testing.T) {
	p := &Palette{on: true}
	table := &Table{Indent: "  ", Gap: 2}
	table.Header = []Cell{Styled("MODE", p.Muted), Styled("PATH", p.Muted)}
	// The unicode path is the case that broke text/tabwriter: three bytes for
	// the en dash, one column on screen.
	table.AddRow(Styled("0644", p.Muted), Styled("assets/Revenue Chart – Europe.webp", p.Path))
	table.AddRow(Styled("0755", p.Muted), Styled("x", p.Path))

	var buf bytes.Buffer
	table.Render(&buf)
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected a header and two rows, got %d lines", len(lines))
	}

	// Every row's second column must begin in the same visible column. The
	// content of that column differs per row, so locate each row's own value.
	secondColumn := []string{"PATH", "assets/Revenue Chart – Europe.webp", "x"}
	want := -1
	for i, l := range lines {
		plain := StripStyles(l)
		if !strings.HasPrefix(plain, "  ") {
			t.Errorf("row %d is not indented: %q", i, plain)
		}
		at := strings.Index(plain, secondColumn[i])
		if at < 0 {
			t.Fatalf("row %d does not contain %q: %q", i, secondColumn[i], plain)
		}
		// Measure in columns, not bytes.
		at = DisplayWidth(plain[:at])
		if want == -1 {
			want = at
		}
		if at != want {
			t.Errorf("row %d starts its second column at %d, want %d: %q", i, at, want, plain)
		}
	}
	// Two of indent, four for the widest first-column value, two of gap.
	if want != 8 {
		t.Errorf("second column starts at %d, want 8", want)
	}
}

func TestTableTrimsTrailingWhitespace(t *testing.T) {
	table := &Table{Indent: "  "}
	table.AddRow(Plain("aaaa"), Plain("b"))
	table.AddRow(Plain("a"), Plain("bbbb"))
	var buf bytes.Buffer
	table.Render(&buf)
	for _, l := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if strings.HasSuffix(l, " ") {
			t.Errorf("row has trailing whitespace: %q", l)
		}
	}
}

func TestWrapText(t *testing.T) {
	const width = 24
	got := wrapText("one two three four five six seven eight nine ten", width)
	for _, l := range got {
		if DisplayWidth(l) > width {
			t.Errorf("line %q is %d columns, over the %d limit", l, DisplayWidth(l), width)
		}
	}
	if strings.Join(got, " ") != "one two three four five six seven eight nine ten" {
		t.Errorf("wrapping lost or reordered words: %q", got)
	}
	if len(got) < 2 {
		t.Errorf("expected the text to wrap, got %q", got)
	}
}

func TestWrapTextHasAFloor(t *testing.T) {
	// Wrapping at a very small width would put one word on each line, which is
	// less readable than overflowing slightly; the renderer clamps at 20.
	got := wrapText("alpha beta gamma delta", 4)
	if len(got) > 2 {
		t.Errorf("width clamping is not applied: %q", got)
	}
}

/* --- parsing ----------------------------------------------------------- */

func testCommand() *Command {
	return &Command{
		Name:    "demo",
		Summary: "A demo command",
		Usage:   []string{"<target> [options]"},
		Arguments: []Argument{
			{Name: "target", Summary: "The thing.", Required: true},
		},
		Options: []Option{
			{Name: "output", Short: "o", Type: TypeString, Placeholder: "path", Summary: "Where to write"},
			{Name: "entry", Type: TypeString, Placeholder: "path", Default: "index.html", Summary: "Entry document"},
			{Name: "force", Short: "f", Type: TypeBoolean, Summary: "Overwrite"},
			{Name: "quiet", Short: "q", Type: TypeBoolean, Summary: "Be quiet"},
			{Name: "mode", Type: TypeString, Placeholder: "when", Choices: []string{"a", "b"}, Summary: "A choice"},
		},
		Run: func(*Env, *Values) int { return 0 },
	}
}

func TestParseAcceptsEveryDocumentedForm(t *testing.T) {
	cases := [][]string{
		{"t", "--output", "x"},
		{"t", "--output=x"},
		{"t", "-o", "x"},
		{"t", "-o=x"},
		{"--output", "x", "t"},
		{"-o", "x", "t"},
	}
	for _, args := range cases {
		v, err := Parse(testCommand(), args)
		if err != nil {
			t.Errorf("Parse(%v): %v", args, err)
			continue
		}
		if v.String("output") != "x" {
			t.Errorf("Parse(%v) output = %q, want x", args, v.String("output"))
		}
		if got := v.Args(); len(got) != 1 || got[0] != "t" {
			t.Errorf("Parse(%v) args = %q, want [t]", args, got)
		}
	}
}

func TestParseDefaults(t *testing.T) {
	v, err := Parse(testCommand(), []string{"t"})
	if err != nil {
		t.Fatal(err)
	}
	if v.String("entry") != "index.html" {
		t.Errorf("entry = %q, want the declared default", v.String("entry"))
	}
	if v.Bool("force") {
		t.Error("force should default to false")
	}
	if v.Seen("entry") {
		t.Error("a defaulted option should not report as seen")
	}
}

func TestParseBundledBooleanShorts(t *testing.T) {
	v, err := Parse(testCommand(), []string{"t", "-fq"})
	if err != nil {
		t.Fatal(err)
	}
	if !v.Bool("force") || !v.Bool("quiet") {
		t.Error("bundled short booleans were not both set")
	}
}

func TestParseDoesNotBundleAStringShort(t *testing.T) {
	// "-of" must not be read as -o -f: -o takes a value.
	_, err := Parse(testCommand(), []string{"t", "-of"})
	if err == nil {
		t.Fatal("Parse silently accepted -of as a bundle containing a string option")
	}
}

func TestParseDoubleDash(t *testing.T) {
	v, err := Parse(testCommand(), []string{"--", "-o", "--force"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-o", "--force"}
	if got := v.Args(); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("args after -- = %q, want %q", got, want)
	}
	if v.Bool("force") {
		t.Error("--force after -- should be a positional, not an option")
	}
}

func TestParseRejectsAMissingValue(t *testing.T) {
	_, err := Parse(testCommand(), []string{"t", "--output"})
	if err == nil {
		t.Fatal("Parse accepted --output with no value")
	}
	if !strings.Contains(err.Error(), "needs a value") {
		t.Errorf("unhelpful error: %v", err)
	}
}

func TestParseRejectsAValueOnASwitch(t *testing.T) {
	if _, err := Parse(testCommand(), []string{"t", "--force=loud"}); err == nil {
		t.Fatal("Parse accepted a value on a boolean option")
	}
	// An explicit boolean literal is fine, though.
	v, err := Parse(testCommand(), []string{"t", "--force=false"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Bool("force") {
		t.Error("--force=false should be false")
	}
}

func TestParseEnforcesChoices(t *testing.T) {
	if _, err := Parse(testCommand(), []string{"t", "--mode", "c"}); err == nil {
		t.Fatal("Parse accepted a value outside the declared choices")
	}
	if _, err := Parse(testCommand(), []string{"t", "--mode", "a"}); err != nil {
		t.Fatalf("Parse rejected a declared choice: %v", err)
	}
}

func TestParseSuggestsACorrection(t *testing.T) {
	_, err := Parse(testCommand(), []string{"t", "--outpt", "x"})
	if err == nil {
		t.Fatal("Parse accepted an unknown option")
	}
	var ue *UsageError
	if !asUsage(err, &ue) {
		t.Fatalf("want a UsageError, got %T", err)
	}
	if !strings.Contains(ue.Hint, "--output") {
		t.Errorf("hint should suggest --output, got %q", ue.Hint)
	}
}

func TestParseListsOptionsWhenNoSuggestionFits(t *testing.T) {
	_, err := Parse(testCommand(), []string{"t", "--completely-unrelated"})
	var ue *UsageError
	if !asUsage(err, &ue) {
		t.Fatalf("want a UsageError, got %v", err)
	}
	if !strings.Contains(ue.Hint, "--output") || !strings.Contains(ue.Hint, "--force") {
		t.Errorf("hint should list the accepted options, got %q", ue.Hint)
	}
}

func TestCheckRequired(t *testing.T) {
	cmd := testCommand()
	cmd.Options[0].Required = true
	v, err := Parse(cmd, []string{"t"})
	if err != nil {
		t.Fatalf("Parse should not enforce required options: %v", err)
	}
	if err := CheckRequired(cmd, v); err == nil {
		t.Fatal("CheckRequired accepted a missing required option")
	}
	v, _ = Parse(cmd, []string{"t", "-o", "x"})
	if err := CheckRequired(cmd, v); err != nil {
		t.Errorf("CheckRequired rejected a satisfied option: %v", err)
	}
}

func TestCheckArgs(t *testing.T) {
	cmd := testCommand()

	v, _ := Parse(cmd, []string{})
	if err := CheckArgs(cmd, v); err == nil {
		t.Error("CheckArgs accepted a missing required argument")
	}
	v, _ = Parse(cmd, []string{"a"})
	if err := CheckArgs(cmd, v); err != nil {
		t.Errorf("CheckArgs rejected the right number of arguments: %v", err)
	}
	v, _ = Parse(cmd, []string{"a", "b"})
	if err := CheckArgs(cmd, v); err == nil {
		t.Error("CheckArgs accepted too many arguments")
	}
}

/* --- suggestions ------------------------------------------------------- */

func TestSuggestCommand(t *testing.T) {
	app := &App{Name: "demo", Commands: []*Command{
		{Name: "pack"}, {Name: "unpack"}, {Name: "validate"}, {Name: "inspect"},
	}}
	cases := map[string]string{
		"packk":    "pack",
		"paK":      "pack",
		"validat":  "validate",
		"inspecrt": "inspect",
	}
	for in, want := range cases {
		if got := app.SuggestCommand(in); got != want {
			t.Errorf("SuggestCommand(%q) = %q, want %q", in, got, want)
		}
	}
	// A wrong suggestion is worse than none.
	if got := app.SuggestCommand("xyzzyplugh"); got != "" {
		t.Errorf("SuggestCommand(nonsense) = %q, want no suggestion", got)
	}
}

/* --- rendering --------------------------------------------------------- */

func TestRenderedHelpHasNoEscapesWithoutColor(t *testing.T) {
	app := &App{
		Name: "demo", Version: "1.0", Summary: "a demo",
		Usage:     []string{"<command>"},
		Commands:  []*Command{testCommand()},
		ExitCodes: []ExitCode{{Code: 0, Name: "ok", Summary: "fine"}},
	}
	var buf bytes.Buffer
	RenderAppHelp(&buf, &Palette{}, app)
	if strings.Contains(buf.String(), "\x1b") {
		t.Error("help contains escapes with colour disabled")
	}
	if !strings.Contains(buf.String(), "demo") {
		t.Error("help does not mention the program")
	}
}

func TestRenderedHelpStaysWithinWidth(t *testing.T) {
	t.Setenv("COLUMNS", "80")
	app := &App{
		Name: "demo", Version: "1.0", Summary: "a demo",
		Description: strings.Repeat("word ", 60),
		Usage:       []string{"<command>"},
		Commands:    []*Command{testCommand()},
		ExitCodes:   []ExitCode{{Code: 0, Name: "ok", Summary: strings.Repeat("long ", 30)}},
	}
	var buf bytes.Buffer
	RenderAppHelp(&buf, &Palette{}, app)
	for _, l := range strings.Split(buf.String(), "\n") {
		if DisplayWidth(l) > 84 { // the gutter may push a wrapped cell slightly over
			t.Errorf("line is %d columns: %q", DisplayWidth(l), l)
		}
	}
}

func TestCommandHelpIncludesEverythingDeclared(t *testing.T) {
	cmd := testCommand()
	cmd.Description = "A longer description."
	cmd.Examples = []Example{{Summary: "Do the thing", Command: "demo x -o y"}}
	cmd.Notes = []Note{{Title: "Caveat", Body: "Mind the gap."}}
	app := &App{Name: "demo", Commands: []*Command{cmd},
		ExitCodes: []ExitCode{{Code: 0, Name: "ok", Summary: "fine"}}}

	var buf bytes.Buffer
	RenderCommandHelp(&buf, &Palette{}, app, cmd)
	out := buf.String()
	for _, want := range []string{
		"USAGE", "ARGUMENTS", "OPTIONS", "DESCRIPTION",
		"A longer description.", "CAVEAT", "Mind the gap.",
		"EXAMPLES", "Do the thing", "demo x -o y",
		"--output", "--entry", "index.html", "EXIT CODES",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("command help is missing %q:\n%s", want, out)
		}
	}
}

func TestOptionSignature(t *testing.T) {
	cases := map[string]Option{
		"-o, --output <path>": {Name: "output", Short: "o", Type: TypeString, Placeholder: "path"},
		"    --force":         {Name: "force", Type: TypeBoolean},
		"-q, --quiet":         {Name: "quiet", Short: "q", Type: TypeBoolean},
	}
	for want, o := range cases {
		if got := o.Signature(); got != want {
			t.Errorf("Signature() = %q, want %q", got, want)
		}
	}
}

func TestArgumentPlaceholder(t *testing.T) {
	cases := map[string]Argument{
		"<target>":    {Name: "target", Required: true},
		"[<target>]":  {Name: "target"},
		"<file>...":   {Name: "file", Required: true, Variadic: true},
		"[<file>...]": {Name: "file", Variadic: true},
	}
	for want, a := range cases {
		if got := a.Placeholder(); got != want {
			t.Errorf("Placeholder() = %q, want %q", got, want)
		}
	}
}

/* --- specification validation ------------------------------------------ */

func TestAppValidateCatchesAuthoringMistakes(t *testing.T) {
	app := &App{
		Name: "demo",
		Commands: []*Command{
			{Name: "a"}, // no summary, no usage, no Run
			{Name: "b", Summary: "Ends with a period.", Usage: []string{"x"},
				Run:     func(*Env, *Values) int { return 0 },
				Options: []Option{{Name: "x", Type: TypeString, Summary: "no placeholder"}}},
			{Name: "a", Summary: "Duplicate name", Usage: []string{"x"},
				Run: func(*Env, *Values) int { return 0 }},
		},
	}
	problems := app.Validate()
	joined := strings.Join(problems, "\n")
	for _, want := range []string{
		"has no summary", "has no usage line", "has no Run function",
		"ends with a period", "has no placeholder", "claimed by both",
		"declares no exit codes",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("Validate did not report %q:\n%s", want, joined)
		}
	}
}

/* --- helpers ----------------------------------------------------------- */

func asUsage(err error, target **UsageError) bool {
	ue, ok := err.(*UsageError)
	if ok {
		*target = ue
	}
	return ok
}

func TestStripStyles(t *testing.T) {
	p := &Palette{on: true}
	cases := map[string]string{
		p.Red("abc"):               "abc",
		p.Bold(p.Red("abc")):       "abc",
		"plain":                    "plain",
		"a" + p.Muted("b") + "c":   "abc",
		"\x1b[38;5;196mred\x1b[0m": "red",
		"\x1bcreset":               "reset",
	}
	for in, want := range cases {
		if got := StripStyles(in); got != want {
			t.Errorf("StripStyles(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStripStylesAndDisplayWidthAgree(t *testing.T) {
	// The two derive from the same scanner, so a styled string and its plain
	// form must always measure the same. This is the invariant that keeps
	// tables aligned when colour is on.
	p := &Palette{on: true}
	for _, s := range []string{
		"index.html",
		"assets/Revenue Chart – Europe.webp",
		"日本語のファイル.png",
	} {
		styled := p.Path(s)
		if DisplayWidth(styled) != DisplayWidth(StripStyles(styled)) {
			t.Errorf("styled and plain widths differ for %q", s)
		}
		if StripStyles(styled) != s {
			t.Errorf("StripStyles(%q) did not recover the original", s)
		}
	}
}
