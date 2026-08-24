package manifest

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func sample() *Manifest {
	return &Manifest{
		Format:     Format,
		Version:    Version,
		Generator:  "slidepack/test",
		Entrypoint: "index.html",
		Payload: Payload{
			Archive:        "tar",
			Compression:    "gzip",
			Encoding:       "base64",
			SHA256:         strings.Repeat("a", 64),
			CompressedSize: 100,
			ArchiveSize:    200,
		},
		Files: []File{
			{Path: "css/a.css", Size: 10, SHA256: strings.Repeat("b", 64), MIME: "text/css", Mode: "0644"},
			{Path: "index.html", Size: 20, SHA256: strings.Repeat("c", 64), MIME: "text/html", Mode: "0644"},
		},
	}
}

func TestMarshalIsDeterministic(t *testing.T) {
	m := sample()
	a, err := m.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("Marshal is not stable across calls")
	}
	if bytes.Contains(a, []byte("timestamp")) || bytes.Contains(a, []byte("uuid")) {
		t.Error("manifest contains a field that would break reproducible packing")
	}
}

func TestMarshalEscapesScriptTerminators(t *testing.T) {
	// A file called "</script>.png" must not be able to break out of the
	// element the manifest is embedded in.
	m := sample()
	m.Files = append(m.Files, File{
		Path: "assets/</script>.png", Size: 1, SHA256: strings.Repeat("d", 64), MIME: "image/png", Mode: "0644",
	})
	SortFiles(m.Files)
	out, err := m.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(bytes.ToLower(out), []byte("</script")) {
		t.Fatalf("marshalled manifest can terminate its <script> element:\n%s", out)
	}
	back, err := Unmarshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := back.Lookup("assets/</script>.png"); !ok {
		t.Error("the escaped path did not survive a round trip")
	}
}

func TestRoundTrip(t *testing.T) {
	data, err := sample().Marshal()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Unmarshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("round-tripped manifest failed validation: %v", err)
	}
	if got.Entrypoint != "index.html" || len(got.Files) != 2 {
		t.Errorf("unexpected manifest: %+v", got)
	}
}

func TestUnmarshalTolerantOfSurroundingWhitespace(t *testing.T) {
	data, _ := sample().Marshal()
	padded := append([]byte("\n   \n"), append(data, []byte("\n\t")...)...)
	if _, err := Unmarshal(padded); err != nil {
		t.Fatalf("Unmarshal rejected indented JSON: %v", err)
	}
}

func TestUnmarshalRejectsUnknownFields(t *testing.T) {
	if _, err := Unmarshal([]byte(`{"format":"slidepack","version":1,"surprise":true}`)); err == nil {
		t.Fatal("Unmarshal accepted an unknown field")
	}
}

func TestValidateRejectsUnsupportedVersion(t *testing.T) {
	m := sample()
	m.Version = 99
	err := m.Validate()
	var uv *ErrUnsupportedVersion
	if !errors.As(err, &uv) {
		t.Fatalf("want ErrUnsupportedVersion, got %v", err)
	}
	if uv.Got != 99 {
		t.Errorf("ErrUnsupportedVersion.Got = %d", uv.Got)
	}
	if !strings.Contains(err.Error(), "version 99") {
		t.Errorf("error should name the offending version: %v", err)
	}
}

func TestValidateRejectsStructuralProblems(t *testing.T) {
	cases := map[string]func(*Manifest){
		"wrong format":       func(m *Manifest) { m.Format = "notslidepack" },
		"wrong archive":      func(m *Manifest) { m.Payload.Archive = "zip" },
		"wrong compression":  func(m *Manifest) { m.Payload.Compression = "brotli" },
		"wrong encoding":     func(m *Manifest) { m.Payload.Encoding = "hex" },
		"short digest":       func(m *Manifest) { m.Payload.SHA256 = "abc" },
		"no entrypoint":      func(m *Manifest) { m.Entrypoint = "" },
		"entrypoint missing": func(m *Manifest) { m.Entrypoint = "absent.html" },
		"unsorted files":     func(m *Manifest) { m.Files[0], m.Files[1] = m.Files[1], m.Files[0] },
		"duplicate paths":    func(m *Manifest) { m.Files[1].Path = m.Files[0].Path },
		"bad file digest":    func(m *Manifest) { m.Files[0].SHA256 = "nope" },
	}
	for name, mutate := range cases {
		m := sample()
		mutate(m)
		if err := m.Validate(); err == nil {
			t.Errorf("Validate accepted a manifest with %s", name)
		}
	}
}

func TestLookupUsesSortedOrder(t *testing.T) {
	m := sample()
	if _, ok := m.Lookup("index.html"); !ok {
		t.Error("Lookup missed an existing path")
	}
	if _, ok := m.Lookup("absent"); ok {
		t.Error("Lookup found a path that is not there")
	}
}

func TestSortFilesIsBytewise(t *testing.T) {
	files := []File{{Path: "b"}, {Path: "A"}, {Path: "a"}, {Path: "Z"}}
	SortFiles(files)
	want := []string{"A", "Z", "a", "b"}
	for i, f := range files {
		if f.Path != want[i] {
			t.Fatalf("SortFiles produced %v, want byte-wise ascending %v", files, want)
		}
	}
}

func TestTotalSize(t *testing.T) {
	if got := sample().TotalSize(); got != 30 {
		t.Errorf("TotalSize = %d, want 30", got)
	}
}
