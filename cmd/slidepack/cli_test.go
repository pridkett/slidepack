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
	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	done := make(chan [2]string, 1)
	go func() {
		o, _ := io.ReadAll(outR)
		e, _ := io.ReadAll(errR)
		done <- [2]string{string(o), string(e)}
	}()

	code := run(args)

	outW.Close()
	errW.Close()
	os.Stdout, os.Stderr = origOut, origErr
	res := <-done
	return code, res[0], res[1]
}

func fixturePath(name string) string {
	return filepath.Join("..", "..", "testdata", name)
}

/* ------------------------------------------------------------------ */
/* Help and version (AC-043)                                           */
/* ------------------------------------------------------------------ */

func TestHelpIsAvailableEverywhere(t *testing.T) {
	cases := []struct {
		args     []string
		wantText string
	}{
		{[]string{"--help"}, "slidepack <command>"},
		{[]string{"-h"}, "COMMANDS"},
		{[]string{"help"}, "EXIT CODES"},
		{[]string{"pack", "--help"}, "slidepack pack"},
		{[]string{"unpack", "--help"}, "slidepack unpack"},
		{[]string{"validate", "--help"}, "slidepack validate"},
		{[]string{"inspect", "--help"}, "slidepack inspect"},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			_, out, errOut := capture(t, c.args...)
			combined := out + errOut
			if !strings.Contains(combined, c.wantText) {
				t.Errorf("help for %v does not mention %q:\n%s", c.args, c.wantText, combined)
			}
			if len(strings.TrimSpace(combined)) < 100 {
				t.Errorf("help for %v is suspiciously short:\n%s", c.args, combined)
			}
		})
	}
}

func TestVersion(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--version"}, {"-v"}} {
		code, out, _ := capture(t, args...)
		if code != exitOK {
			t.Errorf("%v exited %d", args, code)
		}
		if !strings.Contains(out, "slidepack ") || !strings.Contains(out, "format version 1") {
			t.Errorf("%v printed %q", args, out)
		}
	}
}

func TestUnknownCommandIsAUsageError(t *testing.T) {
	code, _, errOut := capture(t, "frobnicate")
	if code != exitUsage {
		t.Errorf("exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(errOut, "unknown command") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestNoArgumentsPrintsUsage(t *testing.T) {
	code, out, _ := capture(t)
	if code != exitUsage {
		t.Errorf("exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(out, "COMMANDS") {
		t.Error("bare invocation should print usage")
	}
}

/* ------------------------------------------------------------------ */
/* Option placement                                                    */
/* ------------------------------------------------------------------ */

func TestOptionsMayFollowPositionalArguments(t *testing.T) {
	// The documented form puts -o after the directory, which Go's flag package
	// would otherwise refuse to parse.
	out := filepath.Join(t.TempDir(), "deck.html")
	code, _, errOut := capture(t, "pack", fixturePath("basic"), "-o", out)
	if code != exitOK {
		t.Fatalf("exit = %d, stderr = %s", code, errOut)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("output not written: %v", err)
	}
}

func TestPackRequiresAnOutputPath(t *testing.T) {
	code, _, errOut := capture(t, "pack", fixturePath("basic"))
	if code != exitUsage {
		t.Errorf("exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(errOut, "output path") {
		t.Errorf("stderr = %q", errOut)
	}
}

/* ------------------------------------------------------------------ */
/* validate                                                           */
/* ------------------------------------------------------------------ */

func TestValidateDirectory(t *testing.T) {
	// AC-030: validating a directory needs no packed file.
	code, out, _ := capture(t, "validate", fixturePath("basic"))
	if code != exitOK {
		t.Errorf("exit = %d, want 0. stdout: %s", code, out)
	}
	if !strings.Contains(out, "valid slidepack source") {
		t.Errorf("stdout = %q", out)
	}
}

func TestValidateInvalidDirectoryExitsNonZero(t *testing.T) {
	code, out, _ := capture(t, "validate", fixturePath("invalid/missing-resource"))
	if code != exitInvalid {
		t.Errorf("exit = %d, want %d", code, exitInvalid)
	}
	if !strings.Contains(out, "MISSING_RESOURCE") {
		t.Errorf("stdout should name the diagnostic code:\n%s", out)
	}
}

// jsonResult mirrors the documented --json shape.
type jsonResult struct {
	Valid  bool `json:"valid"`
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
	// AC-031. stdout must contain the JSON document and nothing else, even
	// when the target is invalid and there is plenty to say about it.
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
	// AC-019.
	out := packFixture(t, "basic")
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	marker := []byte(`id="slidepack-payload"`)
	at := strings.Index(string(body), string(marker))
	if at < 0 {
		t.Fatal("no payload element")
	}
	// Flip characters well inside the base64 blob.
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

	code, stdout, _ := capture(t, "inspect", out) // AC-032
	if code != exitOK {
		t.Fatalf("exit = %d", code)
	}
	for _, want := range []string{"slidepack v1", "Entrypoint", "index.html", "Payload SHA-256", "css/style.css"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("inspect output is missing %q:\n%s", want, stdout)
		}
	}

	code, stdout, _ = capture(t, "inspect", "--json", out) // AC-033
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
}

func TestUnpackRefusesANonEmptyDestination(t *testing.T) {
	// AC-035.
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
	if !strings.Contains(stdout, "Entrypoint: index.html") {
		t.Errorf("stdout = %q", stdout)
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
