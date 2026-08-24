package main

import (
	"fmt"

	"github.com/pwagstro/slidepack/internal/cli"
)

func helpCommand() *cli.Command {
	return withGlobals(&cli.Command{
		Name:    "help",
		Summary: "Show help for slidepack or one of its commands",
		Description: `With no argument, prints an overview: what slidepack is for, the available commands, and where to read more.

With a command name, prints that command's full description, its arguments and options, worked examples, and the exit codes it can return.

With --json, prints the complete interface as a machine-readable document: every command, every option with its type and default, every exit code, and the entire diagnostic vocabulary with a remedy for each code. This is the intended way for an agent or a script to learn how to drive slidepack — it never has to parse help text meant for people.`,
		Usage: []string{
			"[<command>]",
			"--json [<command>]",
			"--all",
		},
		Arguments: []cli.Argument{
			{Name: "command", Summary: "The command to describe. Omit for an overview.", Required: false},
		},
		Options: []cli.Option{
			{
				Name: "json", Type: cli.TypeBoolean,
				Summary: "Emit the interface description as JSON on stdout",
				Details: "Includes every command and option, the exit codes, the full diagnostic catalogue with remedies, and the program's output conventions.",
			},
			{
				Name: "all", Type: cli.TypeBoolean,
				Summary: "Print the full help for every command, one after another",
				Details: "Useful piped into a pager, or captured as a single reference document.",
			},
		},
		Examples: []cli.Example{
			{Summary: "Overview of the whole tool", Command: "slidepack help"},
			{Summary: "Everything about one command", Command: "slidepack help pack"},
			{Summary: "Learn the interface programmatically", Command: "slidepack help --json"},
			{Summary: "Learn one command's interface", Command: "slidepack help --json validate"},
			{Summary: "Read the complete manual", Command: "slidepack help --all | less -R"},
			{Summary: "List every diagnostic code and its remedy",
				Command: "slidepack help --json | jq -r '.diagnostics[] | \"\\(.code)\\t\\(.remedy)\"'"},
		},
		ExitCodes: []cli.ExitCode{
			{Code: exitOK, Name: "ok", Summary: "Help was printed"},
			{Code: exitUsage, Name: "usage", Summary: "No such command"},
		},
		Run: runHelp,
	})
}

func runHelp(env *cli.Env, v *cli.Values) int {
	applyColor(env, v)
	app := env.App
	args := v.Args()

	// A named command narrows both the human and the JSON output.
	var target *cli.Command
	if len(args) == 1 {
		c, ok := app.Lookup(args[0])
		if !ok {
			cli.RenderUnknownCommand(env.Err, env.ErrStyle, app, args[0])
			return exitUsage
		}
		target = c
	}

	if v.Bool("json") {
		if target != nil {
			if err := cli.WriteJSON(env.Out, target); err != nil {
				return fail(env, err)
			}
			return exitOK
		}
		if err := cli.WriteJSON(env.Out, cli.Describe(app)); err != nil {
			return fail(env, err)
		}
		return exitOK
	}

	if v.Bool("all") {
		cli.RenderAppHelp(env.Out, env.Style, app)
		for _, c := range app.Commands {
			fmt.Fprintln(env.Out)
			fmt.Fprintln(env.Out, env.Style.Muted(rule(env)))
			fmt.Fprintln(env.Out)
			cli.RenderCommandHelp(env.Out, env.Style, app, c)
		}
		return exitOK
	}

	if target != nil {
		cli.RenderCommandHelp(env.Out, env.Style, app, target)
		return exitOK
	}

	cli.RenderAppHelp(env.Out, env.Style, app)
	return exitOK
}

// rule draws a horizontal separator for --all.
func rule(env *cli.Env) string {
	width := cli.Width()
	s := make([]byte, width)
	for i := range s {
		s[i] = '-'
	}
	return string(s)
}
