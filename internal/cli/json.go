package cli

import (
	"encoding/json"
	"io"

	"github.com/pridkett/slidepack/internal/diag"
)

// InterfaceDoc is the machine-readable description of the whole program.
//
// This is published by `slidepack help --json` and exists for one reason: an
// agent driving this tool should be able to learn what it can do, how to
// invoke it, what the exit codes mean and what every diagnostic code is
// telling it — without parsing help text written for humans.
//
// Fields are additive. Removing or repurposing one is a breaking change to
// the same degree that renaming a diagnostic code is.
type InterfaceDoc struct {
	// Schema identifies the shape of this document, so a consumer can tell a
	// v1 description from a later one without guessing.
	Schema string `json:"$schema"`

	Name          string `json:"name"`
	Version       string `json:"version"`
	FormatVersion int    `json:"formatVersion"`
	Summary       string `json:"summary"`
	Tagline       string `json:"tagline,omitempty"`
	Description   string `json:"description,omitempty"`

	Usage         []string   `json:"usage"`
	Commands      []*Command `json:"commands"`
	GlobalOptions []Option   `json:"globalOptions"`
	Examples      []Example  `json:"examples,omitempty"`
	ExitCodes     []ExitCode `json:"exitCodes"`

	// Diagnostics is the complete, stable vocabulary of codes that can appear
	// in `validate --json` output, each with what it means and what to change.
	Diagnostics []diag.Info `json:"diagnostics"`

	// Conventions records program-wide behaviour an agent would otherwise
	// have to discover by experiment.
	Conventions Conventions `json:"conventions"`
}

// Conventions describes how the program behaves across all commands.
type Conventions struct {
	// JSONOutputStream names the stream that carries --json documents.
	JSONOutputStream string `json:"jsonOutputStream"`
	// DiagnosticStream names the stream that carries human-facing messages.
	DiagnosticStream string `json:"diagnosticStream"`
	// OptionForms lists the accepted spellings of an option.
	OptionForms []string `json:"optionForms"`
	// OptionsAfterArguments reports whether options may follow positionals.
	OptionsAfterArguments bool `json:"optionsAfterArguments"`
	// Notes are short statements of behaviour worth relying on.
	Notes []string `json:"notes"`
}

// SchemaID versions the interface document itself.
const SchemaID = "https://slidepack.dev/schema/cli-interface/v1"

// Describe builds the interface document for an app.
func Describe(app *App) *InterfaceDoc {
	return &InterfaceDoc{
		Schema:        SchemaID,
		Name:          app.Name,
		Version:       app.Version,
		FormatVersion: app.FormatVersion,
		Summary:       app.Summary,
		Tagline:       app.Tagline,
		Description:   app.Description,
		Usage:         app.Usage,
		Commands:      app.Commands,
		GlobalOptions: app.GlobalOptions,
		Examples:      app.Examples,
		ExitCodes:     app.ExitCodes,
		Diagnostics:   diag.Catalog(),
		Conventions: Conventions{
			JSONOutputStream:      "stdout",
			DiagnosticStream:      "stderr",
			OptionForms:           []string{"--name value", "--name=value", "-n value", "-n=value", "--switch", "-s"},
			OptionsAfterArguments: true,
			Notes: []string{
				"With --json, stdout carries only the JSON document; every human-facing message goes to stderr.",
				"Diagnostic codes are stable. Match on the `code` field, not on message text.",
				"Exit 0 means success or a valid target; exit 3 means the target was readable but is not valid.",
				"`validate` never executes presentation JavaScript, and neither does any other command.",
				"Packing is deterministic: identical source bytes, paths, modes and entrypoint produce a byte-identical file.",
				"Work on the source directory, not the packed file. Reading the base64 payload yields nothing useful.",
			},
		},
	}
}

// WriteJSON emits the interface document.
func WriteJSON(w io.Writer, doc any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	// The document contains HTML fragments in prose ("<base>", "<script ...>");
	// escaping them to < would make the output unpleasant to read and
	// harder to grep, and this never lands in an HTML document.
	enc.SetEscapeHTML(false)
	return enc.Encode(doc)
}
