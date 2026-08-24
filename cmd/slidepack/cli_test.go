package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// capture runs the CLI with args, returning its exit code, stdout and stderr.
//
// The command functions are called directly rather than through a subprocess,
// so a test failure points at a line of Go rather than at an opaque exit code.
// Colour is off because the captured pipes are not terminals, which is exactly
// what the assertions below want: plain, greppable text.
func capture(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origOut, origErr := stdout, stderr
	stdout, stderr = outW, errW
	t.Cleanup(func() { stdout, stderr = origOut, origErr })

	done := make(chan [2]string, 1)
	go func() {
		o, _ := io.ReadAll(outR)
		e, _ := io.ReadAll(errR)
		done <- [2]string{string(o), string(e)}
	}()

	code := run(args)

	outW.Close()
	errW.Close()
	stdout, stderr = origOut, origErr
	res := <-done
	return code, res[0], res[1]
}

func fixturePath(name string) string {
	return filepath.Join("..", "..", "testdata", name)
}

/* ------------------------------------------------------------------ */
/* Specification integrity                                             */
/* ------------------------------------------------------------------ */

func TestSpecificationIsInternallyConsistent(t *testing.T) {
	// The specification drives parsing, help and the JSON interface alike, so
	// an inconsistency here is a bug in all three at once.
	if problems := buildApp().Validate(); len(problems) > 0 {
		for _, p := range problems {
			t.Errorf("specification: %s", p)
		}
	}
}

func TestEveryCommandDocumentsItself(t *testing.T) {
	app := buildApp()
	for _, c := range app.Commands {
		if c.Description == "" {
			t.Errorf("command %q has no description; every command must be self-describing", c.Name)
		}
		if len(c.Examples) == 0 {
			t.Errorf("command %q has no examples", c.Name)
		}
		for _, o := range c.Options {
			if o.Summary == "" {
				t.Errorf("option --%s of %q has no summary", o.Name, c.Name)
			}
		}
	}
}

func TestEveryCommandAcceptsHelp(t *testing.T) {
	for _, c := range buildApp().Commands {
		t.Run(c.Name, func(t *testing.T) {
			for _, form := range [][]string{{c.Name, "--help"}, {c.Name, "-h"}, {"help", c.Name}} {
				code, out, errOut := capture(t, form...)
				if code != exitOK {
					t.Errorf("%v exited %d: %s", form, code, errOut)
				}
				if !strings.Contains(out, "USAGE") {
					t.Errorf("%v printed no usage section:\n%s", form, out)
				}
				if !strings.Contains(out, c.Summary) {
					t.Errorf("%v does not mention the command's summary", form)
				}
			}
		})
	}
}

/* ------------------------------------------------------------------ */
/* Help and version                                                    */
/* ------------------------------------------------------------------ */

func TestTopLevelHelp(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}} {
		code, out, _ := capture(t, args...)
		if code != exitOK {
			t.Errorf("%v exited %d", args, code)
		}
		for _, want := range []string{"USAGE", "COMMANDS", "EXIT CODES", "LEARN MORE", "pack", "unpack", "validate", "inspect"} {
			if !strings.Contains(out, want) {
				t.Errorf("%v help is missing %q", args, want)
			}
		}
	}
}

func TestBareInvocationPrintsHelp(t *testing.T) {
	code, out, _ := capture(t)
	if code != exitUsage {
		t.Errorf("exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(out, "COMMANDS") {
		t.Error("a bare invocation should print usage")
	}
}

func TestHelpAllCoversEveryCommand(t *testing.T) {
	code, out, _ := capture(t, "help", "--all")
	if code != exitOK {
		t.Fatalf("exit = %d", code)
	}
	for _, c := range buildApp().Commands {
		if !strings.Contains(out, "slidepack "+c.Name) {
			t.Errorf("--all output omits %q", c.Name)
		}
	}
}

func TestVersion(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--version"}, {"-v"}} {
		code, out, _ := capture(t, args...)
		if code != exitOK {
			t.Errorf("%v exited %d", args, code)
		}
		if !strings.Contains(out, "slidepack") {
			t.Errorf("%v printed %q", args, out)
		}
	}

	code, out, _ := capture(t, "version", "--json")
	if code != exitOK {
		t.Fatalf("version --json exited %d", code)
	}
	var v struct {
		Name          string `json:"name"`
		Version       string `json:"version"`
		FormatVersion int    `json:"formatVersion"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("version --json is not valid JSON: %v\n%s", err, out)
	}
	if v.Name != "slidepack" || v.FormatVersion != 1 || v.Version == "" {
		t.Errorf("unexpected version document: %+v", v)
	}
}

/* ------------------------------------------------------------------ */
/* The machine-readable interface                                      */
/* ------------------------------------------------------------------ */

type interfaceDoc struct {
	Schema        string `json:"$schema"`
	Name          string `json:"name"`
	FormatVersion int    `json:"formatVersion"`
	Commands      []struct {
		Name    string `json:"name"`
		Summary string `json:"summary"`
		Usage   []string
		Options []struct {
			Name    string `json:"name"`
			Type    string `json:"type"`
			Summary string `json:"summary"`
		} `json:"options"`
		Examples []struct {
			Command string `json:"command"`
			Summary string `json:"summary"`
		} `json:"examples"`
	} `json:"commands"`
	ExitCodes []struct {
		Code    int    `json:"code"`
		Name    string `json:"name"`
		Summary string `json:"summary"`
	} `json:"exitCodes"`
	Diagnostics []struct {
		Code     string `json:"code"`
		Severity string `json:"severity"`
		Summary  string `json:"summary"`
		Remedy   string `json:"remedy"`
	} `json:"diagnostics"`
	Conventions struct {
		JSONOutputStream string `json:"jsonOutputStream"`
	} `json:"conventions"`
}

func TestHelpJSONDescribesTheWholeInterface(t *testing.T) {
	code, out, _ := capture(t, "help", "--json")
	if code != exitOK {
		t.Fatalf("exit = %d", code)
	}
	var doc interfaceDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("help --json is not valid JSON: %v", err)
	}

	if doc.Schema == "" || doc.Name != "slidepack" || doc.FormatVersion != 1 {
		t.Errorf("unexpected header: %+v", doc)
	}

	names := map[string]bool{}
	for _, c := range doc.Commands {
		names[c.Name] = true
		if c.Summary == "" || len(c.Usage) == 0 {
			t.Errorf("command %q is under-described in JSON", c.Name)
		}
		for _, o := range c.Options {
			if o.Type != "string" && o.Type != "boolean" {
				t.Errorf("option --%s of %q has type %q", o.Name, c.Name, o.Type)
			}
			if o.Summary == "" {
				t.Errorf("option --%s of %q has no summary", o.Name, c.Name)
			}
		}
		for _, e := range c.Examples {
			if !strings.HasPrefix(e.Command, "slidepack ") {
				t.Errorf("example %q of %q is not a slidepack command", e.Command, c.Name)
			}
		}
	}
	for _, want := range []string{"pack", "unpack", "validate", "inspect", "version", "help"} {
		if !names[want] {
			t.Errorf("help --json does not describe %q", want)
		}
	}

	if len(doc.ExitCodes) != 4 {
		t.Errorf("expected 4 exit codes, got %d", len(doc.ExitCodes))
	}
	if len(doc.Diagnostics) < 20 {
		t.Errorf("only %d diagnostics documented", len(doc.Diagnostics))
	}
	for _, d := range doc.Diagnostics {
		if d.Severity != "error" && d.Severity != "warning" {
			t.Errorf("diagnostic %s has severity %q", d.Code, d.Severity)
		}
		if d.Summary == "" || d.Remedy == "" {
			t.Errorf("diagnostic %s is under-described", d.Code)
		}
	}
	if doc.Conventions.JSONOutputStream != "stdout" {
		t.Errorf("conventions.jsonOutputStream = %q", doc.Conventions.JSONOutputStream)
	}
}

func TestHelpJSONForOneCommand(t *testing.T) {
	code, out, _ := capture(t, "help", "--json", "pack")
	if code != exitOK {
		t.Fatalf("exit = %d", code)
	}
	var c struct {
		Name    string `json:"name"`
		Options []struct {
			Name     string `json:"name"`
			Required bool   `json:"required"`
		} `json:"options"`
	}
	if err := json.Unmarshal([]byte(out), &c); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if c.Name != "pack" {
		t.Fatalf("described %q, want pack", c.Name)
	}
	var foundRequiredOutput bool
	for _, o := range c.Options {
		if o.Name == "output" && o.Required {
			foundRequiredOutput = true
		}
	}
	if !foundRequiredOutput {
		t.Error("pack's JSON does not mark --output as required")
	}
}

func TestHelpForAnUnknownCommandSuggests(t *testing.T) {
	code, _, errOut := capture(t, "help", "frobnicate")
	if code != exitUsage {
		t.Errorf("exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(errOut, "unknown command") {
		t.Errorf("stderr = %q", errOut)
	}
}

/* ------------------------------------------------------------------ */
/* Argument handling                                                   */
/* ------------------------------------------------------------------ */

func TestUnknownCommandSuggestsTheClosestMatch(t *testing.T) {
	code, _, errOut := capture(t, "packk", fixturePath("basic"))
	if code != exitUsage {
		t.Errorf("exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(errOut, "Did you mean") || !strings.Contains(errOut, "pack") {
		t.Errorf("expected a suggestion, got:\n%s", errOut)
	}
}

func TestUnknownOptionSuggestsTheClosestMatch(t *testing.T) {
	code, _, errOut := capture(t, "pack", fixturePath("basic"), "--outpu", "x.html")
	if code != exitUsage {
		t.Errorf("exit = %d", code)
	}
	if !strings.Contains(errOut, "did you mean --output") {
		t.Errorf("expected a suggestion, got:\n%s", errOut)
	}
}

func TestMissingRequiredOptionIsExplained(t *testing.T) {
	code, _, errOut := capture(t, "pack", fixturePath("basic"))
	if code != exitUsage {
		t.Errorf("exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(errOut, "--output is required") {
		t.Errorf("stderr = %q", errOut)
	}
	if !strings.Contains(errOut, "usage:") {
		t.Error("a usage error should show the usage line")
	}
}

func TestMissingArgumentIsExplained(t *testing.T) {
	code, _, errOut := capture(t, "inspect")
	if code != exitUsage {
		t.Errorf("exit = %d", code)
	}
	if !strings.Contains(errOut, "<file.html>") {
		t.Errorf("stderr should name the missing argument: %q", errOut)
	}
}

func TestTooManyArgumentsIsExplained(t *testing.T) {
	code, _, errOut := capture(t, "validate", fixturePath("basic"), fixturePath("nested"))
	if code != exitUsage {
		t.Errorf("exit = %d", code)
	}
	if !strings.Contains(errOut, "takes 1 argument") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestInvalidChoiceIsExplained(t *testing.T) {
	code, _, errOut := capture(t, "validate", fixturePath("basic"), "--color", "mauve")
	if code != exitUsage {
		t.Errorf("exit = %d", code)
	}
	if !strings.Contains(errOut, "auto, always, never") {
		t.Errorf("stderr should list the accepted values: %q", errOut)
	}
}

func TestOptionsMayAppearBeforeOrAfterArguments(t *testing.T) {
	dir := t.TempDir()
	for i, args := range [][]string{
		{"pack", fixturePath("basic"), "-o", filepath.Join(dir, "a.html")},
		{"pack", "-o", filepath.Join(dir, "b.html"), fixturePath("basic")},
		{"pack", fixturePath("basic"), "--output=" + filepath.Join(dir, "c.html")},
		{"pack", "--output", filepath.Join(dir, "d.html"), "--quiet", fixturePath("basic")},
	} {
		code, _, errOut := capture(t, args...)
		if code != exitOK {
			t.Errorf("form %d (%v) exited %d: %s", i, args, code, errOut)
		}
	}
	for _, name := range []string{"a.html", "b.html", "c.html", "d.html"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s was not written: %v", name, err)
		}
	}
}

func TestDoubleDashEndsOptionParsing(t *testing.T) {
	// A directory whose name begins with a dash must still be packable.
	base := t.TempDir()
	odd := filepath.Join(base, "-weird")
	mustWrite(t, filepath.Join(odd, "index.html"), "<!doctype html><title>x</title><p>x</p>")
	out := filepath.Join(t.TempDir(), "deck.html")

	code, _, errOut := capture(t, "pack", "-o", out, "--", odd)
	if code != exitOK {
		t.Fatalf("exit = %d: %s", code, errOut)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("output not written: %v", err)
	}
}

/* ------------------------------------------------------------------ */
/* validate                                                            */
/* ------------------------------------------------------------------ */

func TestValidateDirectory(t *testing.T) {
	code, out, _ := capture(t, "validate", fixturePath("basic"))
	if code != exitOK {
		t.Errorf("exit = %d, want 0. stdout: %s", code, out)
	}
	if !strings.Contains(out, "valid slidepack source") {
		t.Errorf("stdout = %q", out)
	}
}

func TestValidateInvalidDirectoryExitsThree(t *testing.T) {
	code, out, _ := capture(t, "validate", fixturePath("invalid/missing-resource"))
	if code != exitInvalid {
		t.Errorf("exit = %d, want %d", code, exitInvalid)
	}
	if !strings.Contains(out, "MISSING_RESOURCE") {
		t.Errorf("stdout should name the diagnostic code:\n%s", out)
	}
	if !strings.Contains(out, "index.html") {
		t.Error("stdout should name the file the finding is in")
	}
	if !strings.Contains(out, "--explain") {
		t.Error("stdout should point at --explain")
	}
}

func TestValidateExplainPrintsRemedies(t *testing.T) {
	code, out, _ := capture(t, "validate", fixturePath("invalid/remote-resource"), "--explain")
	if code != exitInvalid {
		t.Errorf("exit = %d", code)
	}
	if !strings.Contains(out, "fix:") {
		t.Errorf("--explain should print a remedy:\n%s", out)
	}
	if !strings.Contains(out, "Download the resource into the source tree") {
		t.Errorf("the remedy text is missing:\n%s", out)
	}
}

type jsonResult struct {
	Valid  bool   `json:"valid"`
	Target string `json:"target"`
	Kind   string `json:"kind"`
	Errors []struct {
		Code    string `json:"code"`
		Path    string `json:"path"`
		Line    int    `json:"line"`
		Message string `json:"message"`
	} `json:"errors"`
	Warnings []struct {
		Code string `json:"code"`
	} `json:"warnings"`
}

func TestValidateJSONIsParseableAndUnpolluted(t *testing.T) {
	code, out, _ := capture(t, "validate", "--json", fixturePath("invalid/remote-resource"))
	if code != exitInvalid {
		t.Errorf("exit = %d, want %d", code, exitInvalid)
	}
	var res jsonResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, out)
	}
	if res.Valid {
		t.Error("valid should be false")
	}
	found := false
	for _, e := range res.Errors {
		if e.Code == "REMOTE_RESOURCE" {
			found = true
			if e.Path == "" || e.Message == "" {
				t.Errorf("diagnostic is missing fields: %+v", e)
			}
		}
	}
	if !found {
		t.Errorf("expected a REMOTE_RESOURCE diagnostic, got %+v", res.Errors)
	}
}

func TestValidateJSONEmitsArraysNotNulls(t *testing.T) {
	_, out, _ := capture(t, "validate", "--json", fixturePath("basic"))
	if !strings.Contains(out, `"errors": []`) || !strings.Contains(out, `"warnings": []`) {
		t.Errorf("a valid target should still emit empty arrays:\n%s", out)
	}
}

func TestValidateStrictTreatsWarningsAsFailures(t *testing.T) {
	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "index.html"), `<a href="appendix.html">A</a>`)
	mustWrite(t, filepath.Join(src, "appendix.html"), `<p>A</p>`)

	if code, _, _ := capture(t, "validate", src); code != exitOK {
		t.Error("a warning alone should not fail validation")
	}
	if code, _, _ := capture(t, "validate", src, "--strict"); code != exitInvalid {
		t.Error("--strict should turn warnings into failures")
	}
}

/* ------------------------------------------------------------------ */
/* Packed-file commands                                                */
/* ------------------------------------------------------------------ */

func packFixture(t *testing.T, name string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "deck.html")
	code, _, errOut := capture(t, "pack", fixturePath(name), "-o", out)
	if code != exitOK {
		t.Fatalf("pack exited %d: %s", code, errOut)
	}
	return out
}

func TestValidatePackedFile(t *testing.T) {
	out := packFixture(t, "basic")
	code, stdout, _ := capture(t, "validate", out)
	if code != exitOK {
		t.Errorf("exit = %d: %s", code, stdout)
	}
	if !strings.Contains(stdout, "valid slidepack package") {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestValidateDetectsACorruptPayload(t *testing.T) {
	out := packFixture(t, "basic")
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	at := strings.Index(string(body), `id="slidepack-payload"`)
	if at < 0 {
		t.Fatal("no payload element")
	}
	start := strings.Index(string(body[at:]), ">") + at + 40
	for i := start; i < start+32; i++ {
		if body[i] == 'A' {
			body[i] = 'B'
		} else {
			body[i] = 'A'
		}
	}
	corrupt := filepath.Join(t.TempDir(), "corrupt.html")
	mustWriteBytes(t, corrupt, body)

	code, stdout, _ := capture(t, "validate", corrupt)
	if code == exitOK {
		t.Fatalf("validate accepted a corrupted payload:\n%s", stdout)
	}
	if !strings.Contains(stdout, "PAYLOAD_HASH_MISMATCH") && !strings.Contains(stdout, "CORRUPT_") {
		t.Errorf("expected an integrity diagnostic:\n%s", stdout)
	}
}

func TestInspectHumanAndJSON(t *testing.T) {
	out := packFixture(t, "basic")

	code, stdout, _ := capture(t, "inspect", out)
	if code != exitOK {
		t.Fatalf("exit = %d", code)
	}
	for _, want := range []string{"slidepack v1", "entrypoint", "index.html", "payload sha256", "css/style.css", "entrypoint"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("inspect output is missing %q:\n%s", want, stdout)
		}
	}

	code, stdout, _ = capture(t, "inspect", "--json", out)
	if code != exitOK {
		t.Fatalf("exit = %d", code)
	}
	var report struct {
		Format     string `json:"format"`
		Version    int    `json:"version"`
		Entrypoint string `json:"entrypoint"`
		FileCount  int    `json:"fileCount"`
		Files      []struct {
			Path string `json:"path"`
			MIME string `json:"mime"`
			Mode string `json:"mode"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("inspect --json is not valid JSON: %v\n%s", err, stdout)
	}
	if report.Format != "slidepack" || report.Version != 1 || report.Entrypoint != "index.html" {
		t.Errorf("unexpected report: %+v", report)
	}
	if report.FileCount != len(report.Files) || report.FileCount == 0 {
		t.Errorf("fileCount = %d, files = %d", report.FileCount, len(report.Files))
	}
}

func TestInspectFilesListsPathsOnly(t *testing.T) {
	out := packFixture(t, "basic")
	code, stdout, _ := capture(t, "inspect", "--files", out)
	if code != exitOK {
		t.Fatalf("exit = %d", code)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 paths, got %d:\n%s", len(lines), stdout)
	}
	for _, l := range lines {
		if strings.ContainsAny(l, " \t") {
			t.Errorf("--files should print bare paths, got %q", l)
		}
	}
}

func TestInspectDigestsShowsFullHashes(t *testing.T) {
	out := packFixture(t, "basic")
	code, stdout, _ := capture(t, "inspect", "--digests", out)
	if code != exitOK {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stdout, "SHA-256") {
		t.Error("--digests should add a SHA-256 column")
	}
}

func TestInspectRejectsANonSlidepackFile(t *testing.T) {
	plain := filepath.Join(t.TempDir(), "plain.html")
	mustWrite(t, plain, "<!doctype html><p>hello</p>")
	code, _, errOut := capture(t, "inspect", plain)
	if code != exitInvalid {
		t.Errorf("exit = %d, want %d", code, exitInvalid)
	}
	if !strings.Contains(errOut, "NOT_SLIDEPACK") {
		t.Errorf("stderr = %q", errOut)
	}
	if !strings.Contains(errOut, "remedy:") {
		t.Errorf("a package error should offer a remedy:\n%s", errOut)
	}
}

func TestUnpackRefusesANonEmptyDestination(t *testing.T) {
	out := packFixture(t, "basic")
	dest := t.TempDir()
	mustWrite(t, filepath.Join(dest, "keep.txt"), "keep me")

	code, _, errOut := capture(t, "unpack", out, "-o", dest)
	if code == exitOK {
		t.Fatal("unpack wrote into a non-empty destination without --force")
	}
	if !strings.Contains(errOut, "--force") {
		t.Errorf("the error should mention --force: %q", errOut)
	}
	if _, err := os.Stat(filepath.Join(dest, "index.html")); !os.IsNotExist(err) {
		t.Error("unpack wrote files despite refusing")
	}

	if code, _, errOut := capture(t, "unpack", out, "-o", dest, "--force"); code != exitOK {
		t.Fatalf("--force should permit it: %s", errOut)
	}
	if _, err := os.Stat(filepath.Join(dest, "index.html")); err != nil {
		t.Errorf("--force did not extract: %v", err)
	}
}

func TestUnpackRoundTripThroughTheCLI(t *testing.T) {
	out := packFixture(t, "nested")
	dest := filepath.Join(t.TempDir(), "restored")
	code, stdout, errOut := capture(t, "unpack", out, "-o", dest)
	if code != exitOK {
		t.Fatalf("exit = %d: %s", code, errOut)
	}
	if !strings.Contains(stdout, "index.html") {
		t.Errorf("stdout should report the entrypoint: %q", stdout)
	}
	got, err := os.ReadFile(filepath.Join(dest, "assets", "styles", "main", "deck.css"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join(fixturePath("nested"), "assets", "styles", "main", "deck.css"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Error("restored file differs from the source")
	}
}

func TestPackRefusesToOverwriteWithoutForce(t *testing.T) {
	out := filepath.Join(t.TempDir(), "deck.html")
	if code, _, _ := capture(t, "pack", fixturePath("basic"), "-o", out); code != exitOK {
		t.Fatal("first pack failed")
	}
	if code, _, errOut := capture(t, "pack", fixturePath("basic"), "-o", out); code != exitError {
		t.Errorf("exit = %d, want %d (stderr: %s)", code, exitError, errOut)
	}
	if code, _, errOut := capture(t, "pack", fixturePath("basic"), "-o", out, "--force"); code != exitOK {
		t.Errorf("--force should permit the overwrite: %s", errOut)
	}
}

func TestPackReportsValidationFailuresWithExitCodeThree(t *testing.T) {
	out := filepath.Join(t.TempDir(), "deck.html")
	code, _, errOut := capture(t, "pack", fixturePath("invalid/base-element"), "-o", out)
	if code != exitInvalid {
		t.Errorf("exit = %d, want %d", code, exitInvalid)
	}
	if !strings.Contains(errOut, "BASE_ELEMENT") {
		t.Errorf("stderr should carry the diagnostic:\n%s", errOut)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Error("a failed pack wrote an output file")
	}
}

func TestQuietSuppressesSuccessOutput(t *testing.T) {
	out := filepath.Join(t.TempDir(), "deck.html")
	code, stdout, _ := capture(t, "pack", fixturePath("basic"), "-o", out, "--quiet")
	if code != exitOK {
		t.Fatalf("exit = %d", code)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("--quiet should print nothing, got %q", stdout)
	}
}

/* ------------------------------------------------------------------ */
/* Colour                                                              */
/* ------------------------------------------------------------------ */

func TestOutputIsPlainWhenNotATerminal(t *testing.T) {
	// The capture pipes are not terminals, so nothing should be styled.
	_, out, _ := capture(t, "validate", fixturePath("basic"))
	if strings.Contains(out, "\x1b[") {
		t.Errorf("output contains escape sequences when not writing to a terminal:\n%q", out)
	}
}

func TestColorNeverIsRespected(t *testing.T) {
	t.Setenv("CLICOLOR_FORCE", "1")
	_, out, _ := capture(t, "validate", fixturePath("basic"), "--color", "never")
	if strings.Contains(out, "\x1b[") {
		t.Errorf("--color never still emitted escapes:\n%q", out)
	}
	_, out, _ = capture(t, "validate", fixturePath("basic"), "--no-color")
	if strings.Contains(out, "\x1b[") {
		t.Errorf("--no-color still emitted escapes:\n%q", out)
	}
}

func TestNoColorEnvironmentWins(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("CLICOLOR_FORCE", "1")
	_, out, _ := capture(t, "validate", fixturePath("basic"), "--color", "always")
	if strings.Contains(out, "\x1b[") {
		t.Errorf("NO_COLOR must win over --color always:\n%q", out)
	}
}

func TestColorAlwaysWorksWhenPiped(t *testing.T) {
	_, out, _ := capture(t, "validate", fixturePath("basic"), "--color", "always")
	if !strings.Contains(out, "\x1b[") {
		t.Errorf("--color always should style even a pipe:\n%q", out)
	}
}

func TestJSONOutputIsNeverColored(t *testing.T) {
	_, out, _ := capture(t, "validate", "--json", fixturePath("basic"), "--color", "always")
	if strings.Contains(out, "\x1b[") {
		t.Errorf("JSON output must never be styled:\n%q", out)
	}
	if err := json.Unmarshal([]byte(out), &jsonResult{}); err != nil {
		t.Fatalf("coloured run produced invalid JSON: %v", err)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	mustWriteBytes(t, path, []byte(body))
}

func mustWriteBytes(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
}
