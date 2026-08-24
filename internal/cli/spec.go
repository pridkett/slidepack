package cli

import (
	"fmt"
	"sort"
	"strings"
)

// OptionType is the value shape of an option.
type OptionType string

const (
	// TypeString takes a value: --entry slides.html.
	TypeString OptionType = "string"
	// TypeBoolean is a switch: --force.
	TypeBoolean OptionType = "boolean"
)

// Option declares one command-line option.
type Option struct {
	// Name is the long form, without dashes: "output".
	Name string `json:"name"`
	// Short is the single-character form, without a dash: "o". Optional.
	Short string `json:"short,omitempty"`
	// Type is "string" or "boolean".
	Type OptionType `json:"type"`
	// Placeholder names the value in help text: "path", "when". String
	// options only.
	Placeholder string `json:"placeholder,omitempty"`
	// Default is the value used when the option is absent, rendered as text.
	Default string `json:"default,omitempty"`
	// Required means the command fails without it.
	Required bool `json:"required,omitempty"`
	// Choices enumerates the accepted values, when there is a fixed set.
	Choices []string `json:"choices,omitempty"`
	// Summary is one line, no trailing period.
	Summary string `json:"summary"`
	// Details is an optional paragraph shown in full command help.
	Details string `json:"details,omitempty"`
	// Global marks an option every command accepts.
	Global bool `json:"global,omitempty"`
}

// Flags renders an option's forms for help output: "-o, --output".
func (o Option) Flags() string {
	if o.Short != "" {
		return "-" + o.Short + ", --" + o.Name
	}
	return "    --" + o.Name
}

// Signature renders the option with its value placeholder.
func (o Option) Signature() string {
	s := o.Flags()
	if o.Type == TypeString {
		ph := o.Placeholder
		if ph == "" {
			ph = "value"
		}
		s += " <" + ph + ">"
	}
	return s
}

// Argument declares one positional argument.
type Argument struct {
	Name     string `json:"name"`
	Summary  string `json:"summary"`
	Required bool   `json:"required"`
	Variadic bool   `json:"variadic,omitempty"`
}

// Placeholder renders the argument as it appears in a usage line.
func (a Argument) Placeholder() string {
	s := "<" + a.Name + ">"
	if a.Variadic {
		s += "..."
	}
	if !a.Required {
		s = "[" + s + "]"
	}
	return s
}

// Example is a runnable invocation with an explanation.
type Example struct {
	// Summary says what the example achieves, in the imperative.
	Summary string `json:"summary"`
	// Command is the full command line, without a shell prompt.
	Command string `json:"command"`
}

// Note is a titled paragraph of command-specific guidance.
type Note struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// ExitCode documents one exit status.
type ExitCode struct {
	Code    int    `json:"code"`
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

// Command is the complete declaration of one subcommand.
//
// Everything the user or an agent can learn about the command comes from
// here: `--help`, `help <command>`, `help --json` and the flag parser are all
// built from this one value.
type Command struct {
	Name string `json:"name"`
	// Aliases are alternative names that resolve to this command.
	Aliases []string `json:"aliases,omitempty"`
	// Summary is one line for the command list. No trailing period.
	Summary string `json:"summary"`
	// Description is the full explanation, in paragraphs separated by blank
	// lines.
	Description string `json:"description,omitempty"`
	// Usage lines, without the program name.
	Usage     []string   `json:"usage"`
	Arguments []Argument `json:"arguments,omitempty"`
	Options   []Option   `json:"options"`
	Examples  []Example  `json:"examples,omitempty"`
	Notes     []Note     `json:"notes,omitempty"`
	// SeeAlso names related commands.
	SeeAlso []string `json:"seeAlso,omitempty"`
	// ExitCodes overrides the program-wide list when a command uses a
	// narrower set.
	ExitCodes []ExitCode `json:"exitCodes,omitempty"`
	// Hidden keeps a command out of the summary list but not out of help.
	Hidden bool `json:"hidden,omitempty"`

	// Run executes the command. Not serialised.
	Run func(env *Env, v *Values) int `json:"-"`
}

// Option returns the declared option with the given long name.
func (c *Command) Option(name string) (Option, bool) {
	for _, o := range c.Options {
		if o.Name == name {
			return o, true
		}
	}
	return Option{}, false
}

// Matches reports whether name addresses this command.
func (c *Command) Matches(name string) bool {
	if c.Name == name {
		return true
	}
	for _, a := range c.Aliases {
		if a == name {
			return true
		}
	}
	return false
}

// App is the whole program.
type App struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	// FormatVersion is the packed format version this build reads and writes.
	FormatVersion int    `json:"formatVersion"`
	Summary       string `json:"summary"`
	Description   string `json:"description,omitempty"`
	// Tagline is the one-sentence mental model, printed prominently.
	Tagline       string     `json:"tagline,omitempty"`
	Usage         []string   `json:"usage"`
	Commands      []*Command `json:"commands"`
	GlobalOptions []Option   `json:"globalOptions"`
	Examples      []Example  `json:"examples,omitempty"`
	ExitCodes     []ExitCode `json:"exitCodes"`
	// Resources are documentation pointers.
	Resources []Resource `json:"resources,omitempty"`
}

// Resource is a documentation pointer published in help.
type Resource struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

// Lookup finds a command by name or alias.
func (a *App) Lookup(name string) (*Command, bool) {
	for _, c := range a.Commands {
		if c.Matches(name) {
			return c, true
		}
	}
	return nil, false
}

// VisibleCommands returns the commands shown in the summary list.
func (a *App) VisibleCommands() []*Command {
	var out []*Command
	for _, c := range a.Commands {
		if !c.Hidden {
			out = append(out, c)
		}
	}
	return out
}

// SuggestCommand returns the closest command name to a mistyped one, if any is
// close enough to be worth offering.
//
// A wrong suggestion is worse than none, so the threshold is deliberately
// tight: at most a third of the input's length in edits, and never more
// than three.
func (a *App) SuggestCommand(name string) string {
	best, bestDist := "", 1<<30
	limit := len(name) / 3
	if limit < 1 {
		limit = 1
	}
	if limit > 3 {
		limit = 3
	}
	for _, c := range a.Commands {
		for _, candidate := range append([]string{c.Name}, c.Aliases...) {
			d := editDistance(strings.ToLower(name), candidate)
			// A prefix of a command name is almost certainly that command.
			if strings.HasPrefix(candidate, strings.ToLower(name)) && len(name) >= 2 {
				d = 0
			}
			if d < bestDist {
				best, bestDist = c.Name, d
			}
		}
	}
	if bestDist <= limit {
		return best
	}
	return ""
}

// editDistance is the Levenshtein distance between two strings.
func editDistance(a, b string) int {
	if a == b {
		return 0
	}
	ar, br := []rune(a), []rune(b)
	prev := make([]int, len(br)+1)
	cur := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		cur[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			cur[j] = min3(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(br)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

// Validate checks the specification's internal consistency.
//
// This runs in a test rather than at startup: these are authoring mistakes,
// and catching them in CI is more useful than catching them in a user's
// terminal.
func (a *App) Validate() []string {
	var problems []string
	seen := map[string]string{}

	for _, c := range a.Commands {
		if c.Summary == "" {
			problems = append(problems, fmt.Sprintf("command %q has no summary", c.Name))
		}
		if strings.HasSuffix(c.Summary, ".") {
			problems = append(problems, fmt.Sprintf("command %q summary ends with a period", c.Name))
		}
		if len(c.Usage) == 0 {
			problems = append(problems, fmt.Sprintf("command %q has no usage line", c.Name))
		}
		if c.Run == nil {
			problems = append(problems, fmt.Sprintf("command %q has no Run function", c.Name))
		}
		for _, name := range append([]string{c.Name}, c.Aliases...) {
			if prev, dup := seen[name]; dup {
				problems = append(problems, fmt.Sprintf("name %q is claimed by both %q and %q", name, prev, c.Name))
			}
			seen[name] = c.Name
		}

		shorts := map[string]bool{}
		longs := map[string]bool{}
		for _, o := range c.Options {
			if o.Name == "" {
				problems = append(problems, fmt.Sprintf("command %q has an option with no name", c.Name))
			}
			if o.Summary == "" {
				problems = append(problems, fmt.Sprintf("option --%s of %q has no summary", o.Name, c.Name))
			}
			if o.Type != TypeString && o.Type != TypeBoolean {
				problems = append(problems, fmt.Sprintf("option --%s of %q has type %q", o.Name, c.Name, o.Type))
			}
			if o.Type == TypeString && o.Placeholder == "" {
				problems = append(problems, fmt.Sprintf("string option --%s of %q has no placeholder", o.Name, c.Name))
			}
			if o.Type == TypeBoolean && o.Default != "" && o.Default != "false" {
				problems = append(problems, fmt.Sprintf("boolean option --%s of %q defaults to %q; only false is supported", o.Name, c.Name, o.Default))
			}
			if longs[o.Name] {
				problems = append(problems, fmt.Sprintf("option --%s is declared twice on %q", o.Name, c.Name))
			}
			longs[o.Name] = true
			if o.Short != "" {
				if len(o.Short) != 1 {
					problems = append(problems, fmt.Sprintf("short form %q of --%s is not a single character", o.Short, o.Name))
				}
				if shorts[o.Short] {
					problems = append(problems, fmt.Sprintf("short form -%s is declared twice on %q", o.Short, c.Name))
				}
				shorts[o.Short] = true
			}
		}

		for _, ex := range c.Examples {
			if !strings.HasPrefix(ex.Command, a.Name+" ") {
				problems = append(problems, fmt.Sprintf("example %q of %q does not start with %q", ex.Command, c.Name, a.Name))
			}
			if ex.Summary == "" {
				problems = append(problems, fmt.Sprintf("example %q of %q has no summary", ex.Command, c.Name))
			}
		}
		for _, ref := range c.SeeAlso {
			if _, ok := a.Lookup(ref); !ok {
				problems = append(problems, fmt.Sprintf("%q lists unknown command %q under SeeAlso", c.Name, ref))
			}
		}
	}

	if len(a.ExitCodes) == 0 {
		problems = append(problems, "the app declares no exit codes")
	}
	sort.Strings(problems)
	return problems
}
