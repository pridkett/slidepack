package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// Env carries everything a command needs to talk to the user.
type Env struct {
	// Out is for the command's product: reports, JSON, results.
	Out io.Writer
	// Err is for diagnostics and anything a --json consumer must not see.
	Err io.Writer
	// Style colours Out.
	Style *Palette
	// ErrStyle colours Err. It is resolved separately because stdout and
	// stderr are redirected independently: `slidepack validate x > log` should
	// still colour the messages the user actually sees on the terminal.
	ErrStyle *Palette
	// App is the specification, so commands can render help for themselves.
	App *App
	// ColorMode is the resolved --color setting, for sub-renderers.
	ColorMode ColorMode
}

// Values holds parsed option values and positional arguments.
type Values struct {
	strings map[string]string
	bools   map[string]bool
	seen    map[string]bool
	args    []string
	cmd     *Command
}

// Seen reports whether an option was given explicitly, as opposed to taking
// its declared default.
func (v *Values) Seen(name string) bool { return v.seen[name] }

// String returns a string option's value, or its declared default.
func (v *Values) String(name string) string { return v.strings[name] }

// Bool returns a boolean option's value.
func (v *Values) Bool(name string) bool { return v.bools[name] }

// Args returns the positional arguments.
func (v *Values) Args() []string { return v.args }

// UsageError is a malformed command line. It is separate from an operational
// failure so the caller can exit 2 and print usage rather than exit 1.
type UsageError struct {
	Command *Command
	Message string
	// Hint is an optional second line suggesting the fix.
	Hint string
}

func (e *UsageError) Error() string { return e.Message }

func usagef(cmd *Command, format string, args ...any) *UsageError {
	return &UsageError{Command: cmd, Message: fmt.Sprintf(format, args...)}
}

// Parse interprets args against a command's declared options.
//
// The parser is written here rather than delegated to the flag package for
// three reasons: options may appear after positional arguments, which
// flag.FlagSet refuses; long options need a real "--" form rather than the
// single-dash spelling flag prints in its own help; and errors must name the
// option and suggest a correction, which flag cannot do.
//
// Accepted forms:
//
//	--name value    --name=value    --flag
//	-n value        -n=value        -f        -abc (bundled booleans)
//	--              everything after is positional
func Parse(cmd *Command, args []string) (*Values, error) {
	v := &Values{
		strings: map[string]string{},
		bools:   map[string]bool{},
		seen:    map[string]bool{},
		cmd:     cmd,
	}
	byLong := map[string]Option{}
	byShort := map[string]Option{}
	for _, o := range cmd.Options {
		byLong[o.Name] = o
		if o.Short != "" {
			byShort[o.Short] = o
		}
		if o.Type == TypeString {
			v.strings[o.Name] = o.Default
		} else {
			v.bools[o.Name] = o.Default == "true"
		}
	}
	seen := v.seen

	set := func(o Option, value string) error {
		if o.Type == TypeString {
			if len(o.Choices) > 0 && !contains(o.Choices, value) {
				return &UsageError{
					Command: cmd,
					Message: fmt.Sprintf("--%s does not accept %q", o.Name, value),
					Hint:    "accepted values: " + strings.Join(o.Choices, ", "),
				}
			}
			v.strings[o.Name] = value
		} else {
			v.bools[o.Name] = true
		}
		seen[o.Name] = true
		return nil
	}

	i := 0
	for ; i < len(args); i++ {
		arg := args[i]

		if arg == "--" {
			v.args = append(v.args, args[i+1:]...)
			break
		}

		// A lone "-" is a conventional stand-in for stdin, not an option.
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			v.args = append(v.args, arg)
			continue
		}

		var name, inline string
		var hasInline bool
		var short bool

		if strings.HasPrefix(arg, "--") {
			name = arg[2:]
		} else {
			name = arg[1:]
			short = true
		}
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			name, inline, hasInline = name[:eq], name[eq+1:], true
		}
		if name == "" {
			return nil, usagef(cmd, "%q is not a valid option", arg)
		}

		// Bundled short booleans: -abc. Only valid when every character is a
		// declared boolean short form, so "-o out" is never misread.
		if short && len(name) > 1 && !hasInline && allBooleanShorts(name, byShort) {
			for _, r := range name {
				o := byShort[string(r)]
				if err := set(o, ""); err != nil {
					return nil, err
				}
			}
			continue
		}

		var o Option
		var ok bool
		if short {
			o, ok = byShort[name]
		} else {
			// Accept the single-dash spelling of a long name too, since Go
			// programs have trained people to type "-output".
			o, ok = byLong[name]
		}
		if !ok && short {
			o, ok = byLong[name]
		}
		if !ok {
			return nil, unknownOption(cmd, arg, name, byLong)
		}

		if hasInline {
			if o.Type == TypeBoolean && !isBoolLiteral(inline) {
				return nil, usagef(cmd, "--%s is a switch and does not take a value", o.Name)
			}
			if o.Type == TypeBoolean {
				v.bools[o.Name] = inline == "true" || inline == "1" || inline == "yes"
				seen[o.Name] = true
				continue
			}
			if err := set(o, inline); err != nil {
				return nil, err
			}
			continue
		}

		if o.Type == TypeBoolean {
			if err := set(o, ""); err != nil {
				return nil, err
			}
			continue
		}

		if i+1 >= len(args) {
			return nil, &UsageError{
				Command: cmd,
				Message: fmt.Sprintf("--%s needs a value", o.Name),
				Hint:    "try: --" + o.Name + " <" + placeholderOf(o) + ">",
			}
		}
		i++
		if err := set(o, args[i]); err != nil {
			return nil, err
		}
	}

	return v, nil
}

// CheckRequired verifies that every required option was given.
//
// This is separate from Parse so that the caller can honour --help first. A
// user who types "slidepack pack --help" is asking what --output is for;
// answering "--output is required" would be exactly the wrong response.
func CheckRequired(cmd *Command, v *Values) error {
	for _, o := range cmd.Options {
		if o.Required && !v.seen[o.Name] {
			return &UsageError{
				Command: cmd,
				Message: fmt.Sprintf("--%s is required", o.Name),
				Hint:    o.Summary,
			}
		}
	}
	return nil
}

// CheckArgs validates the positional arguments against the declaration.
func CheckArgs(cmd *Command, v *Values) error {
	required, variadic := 0, false
	for _, a := range cmd.Arguments {
		if a.Required {
			required++
		}
		if a.Variadic {
			variadic = true
		}
	}
	got := len(v.args)

	if got < required {
		missing := cmd.Arguments[got]
		return &UsageError{
			Command: cmd,
			Message: fmt.Sprintf("%s needs %s", cmd.Name, missing.Placeholder()),
			Hint:    missing.Summary,
		}
	}
	if !variadic && got > len(cmd.Arguments) {
		word := "argument"
		if len(cmd.Arguments) != 1 {
			word = "arguments"
		}
		return &UsageError{
			Command: cmd,
			Message: fmt.Sprintf("%s takes %d %s, but %d were given", cmd.Name, len(cmd.Arguments), word, got),
			Hint:    "unexpected: " + strings.Join(v.args[len(cmd.Arguments):], " "),
		}
	}
	return nil
}

// unknownOption builds an error that suggests the intended option.
func unknownOption(cmd *Command, raw, name string, byLong map[string]Option) error {
	err := &UsageError{Command: cmd, Message: fmt.Sprintf("unknown option %s", raw)}

	best, bestDist := "", 1<<30
	for long := range byLong {
		if d := editDistance(strings.ToLower(name), long); d < bestDist {
			best, bestDist = long, d
		}
	}
	if bestDist <= 2 && best != "" {
		err.Hint = "did you mean --" + best + "?"
		return err
	}

	var known []string
	for long := range byLong {
		known = append(known, "--"+long)
	}
	sort.Strings(known)
	if len(known) > 0 {
		err.Hint = cmd.Name + " accepts: " + strings.Join(known, ", ")
	}
	return err
}

func allBooleanShorts(chars string, byShort map[string]Option) bool {
	for _, r := range chars {
		o, ok := byShort[string(r)]
		if !ok || o.Type != TypeBoolean {
			return false
		}
	}
	return true
}

func isBoolLiteral(s string) bool {
	switch strings.ToLower(s) {
	case "true", "false", "1", "0", "yes", "no":
		return true
	}
	return false
}

func placeholderOf(o Option) string {
	if o.Placeholder != "" {
		return o.Placeholder
	}
	return "value"
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
