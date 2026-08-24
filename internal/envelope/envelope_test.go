package envelope

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/pwagstro/slidepack/internal/manifest"
	"github.com/pwagstro/slidepack/internal/runtime"
)

func sampleManifest() *manifest.Manifest {
	return &manifest.Manifest{
		Format:     manifest.Format,
		Version:    manifest.Version,
		Entrypoint: "index.html",
		Payload: manifest.Payload{
			Archive: "tar", Compression: "gzip", Encoding: "base64",
			SHA256: strings.Repeat("a", 64), CompressedSize: 3, ArchiveSize: 9,
		},
		Files: []manifest.File{
			{Path: "index.html", Size: 9, SHA256: strings.Repeat("b", 64), MIME: "text/html", Mode: "0644"},
		},
	}
}

func build(t *testing.T, payload string, title string) []byte {
	t.Helper()
	var buf bytes.Buffer
	err := Write(&buf, WriteOptions{
		Manifest: sampleManifest(),
		Title:    title,
		Payload:  strings.NewReader(payload),
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	return buf.Bytes()
}

func TestRuntimeAssetsCannotBreakOutOfTheirElements(t *testing.T) {
	if err := runtime.Check(); err != nil {
		t.Fatal(err)
	}
}

func TestWriteProducesAParseableDocument(t *testing.T) {
	doc := build(t, "payload-bytes", "My Deck")
	s := string(doc)

	for _, want := range []string{
		"<!doctype html>", Marker,
		`id="` + ManifestID + `"`, `id="` + PayloadID + `"`,
		`id="` + RuntimeID + `"`, `id="` + StatusID + `"`,
		"<noscript>", "Loading presentation", "<title>My Deck</title>",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("output is missing %q", want)
		}
	}

	parsed, err := Parse(doc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !bytes.Contains(parsed.ManifestJSON, []byte(`"entrypoint": "index.html"`)) {
		t.Errorf("manifest not recovered: %s", parsed.ManifestJSON)
	}
	// "payload-bytes" base64-encoded.
	if string(parsed.PayloadBase64) != "cGF5bG9hZC1ieXRlcw==" {
		t.Errorf("payload = %q", parsed.PayloadBase64)
	}
	for k, want := range map[string]string{
		"data-format": "tar", "data-compression": "gzip", "data-encoding": "base64",
	} {
		if parsed.PayloadAttrs[k] != want {
			t.Errorf("payload attribute %s = %q, want %q", k, parsed.PayloadAttrs[k], want)
		}
	}
}

func TestWriteEscapesTheTitle(t *testing.T) {
	doc := string(build(t, "x", `Deck & <script>alert(1)</script>`))
	if strings.Contains(doc, "<title>Deck & <script>") {
		t.Error("the title was not HTML-escaped")
	}
	if !strings.Contains(doc, "&lt;script&gt;") {
		t.Errorf("expected an escaped title in:\n%s", doc[:600])
	}
}

func TestWriteDefaultsTheTitle(t *testing.T) {
	if !strings.Contains(string(build(t, "x", "   ")), "<title>Presentation</title>") {
		t.Error("a blank title should fall back to a generic one")
	}
}

func TestWriteIsDeterministic(t *testing.T) {
	if !bytes.Equal(build(t, "same", "T"), build(t, "same", "T")) {
		t.Fatal("two identical Write calls produced different bytes")
	}
}

func TestParseRejectsNonSlidepackDocuments(t *testing.T) {
	_, err := Parse([]byte("<!doctype html><html><body>hello</body></html>"))
	if !errors.Is(err, ErrNotSlidepack) {
		t.Fatalf("want ErrNotSlidepack, got %v", err)
	}
}

func TestParseReportsAMissingElement(t *testing.T) {
	doc := build(t, "x", "T")
	// Keep the marker but destroy the manifest element's id.
	broken := bytes.Replace(doc, []byte(`id="`+ManifestID+`"`), []byte(`id="something-else"`), 1)
	if _, err := Parse(broken); err == nil {
		t.Fatal("Parse accepted a document with no manifest element")
	}
}

func TestParseToleratesAttributeReordering(t *testing.T) {
	doc := build(t, "x", "T")
	reordered := bytes.Replace(doc,
		[]byte(`<script id="`+PayloadID+`" type="application/octet-stream"`),
		[]byte(`<script type="application/octet-stream" data-extra="1" id="`+PayloadID+`"`), 1)
	parsed, err := Parse(reordered)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if string(parsed.PayloadBase64) != "eA==" {
		t.Errorf("payload = %q", parsed.PayloadBase64)
	}
}

func TestParseRejectsAnIDOutsideAStartTag(t *testing.T) {
	doc := []byte(Marker + "\n<p>id=\"" + ManifestID + "\"</p>")
	if _, err := Parse(doc); err == nil {
		t.Fatal("Parse accepted an id that is not on a <script> element")
	}
}

func TestParseHandlesAnEmptyPayload(t *testing.T) {
	parsed, err := Parse(build(t, "", "T"))
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.PayloadBase64) != 0 {
		t.Errorf("expected an empty payload, got %q", parsed.PayloadBase64)
	}
}

func TestParseAttrs(t *testing.T) {
	got := parseAttrs([]byte(`<script id="x" type='application/json' data-n=5 flag>`))
	want := map[string]string{"id": "x", "type": "application/json", "data-n": "5"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("attr %s = %q, want %q", k, got[k], v)
		}
	}
}

func TestPayloadIsASingleBlock(t *testing.T) {
	// AC-041: one payload, not one base64 blob per asset.
	doc := string(build(t, "payload", "T"))
	if n := strings.Count(doc, `type="application/octet-stream"`); n != 1 {
		t.Errorf("found %d payload elements, want exactly 1", n)
	}
}
