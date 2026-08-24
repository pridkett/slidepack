package archive

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"strings"
	"testing"
)

// entryFrom builds an Entry backed by an in-memory buffer.
func entryFrom(path string, mode os.FileMode, data string) Entry {
	return Entry{
		Path: path,
		Mode: mode,
		Size: int64(len(data)),
		Open: func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(data)), nil
		},
	}
}

func writeAll(t *testing.T, entries []Entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := WriteTar(&buf, entries); err != nil {
		t.Fatalf("WriteTar: %v", err)
	}
	return buf.Bytes()
}

func readAll(t *testing.T, data []byte) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := ReadTar(bytes.NewReader(data), func(h Header, body io.Reader) error {
		b, err := io.ReadAll(body)
		if err != nil {
			return err
		}
		out[h.Path] = string(b)
		return nil
	})
	if err != nil {
		t.Fatalf("ReadTar: %v", err)
	}
	return out
}

func TestRoundTrip(t *testing.T) {
	entries := []Entry{
		entryFrom("assets/Revenue Chart – Europe.webp", 0o644, "webp-bytes"),
		entryFrom("empty", 0o644, ""),
		entryFrom("index.html", 0o644, "<!doctype html>"),
		entryFrom("scripts/run", 0o755, "#!/bin/sh\n"),
		entryFrom("大きい/ファイル.txt", 0o600, strings.Repeat("x", 1024)),
	}
	data := writeAll(t, entries)

	if len(data)%blockSize != 0 {
		t.Errorf("archive length %d is not a multiple of %d", len(data), blockSize)
	}
	got := readAll(t, data)
	if len(got) != len(entries) {
		t.Fatalf("read %d entries, wrote %d", len(got), len(entries))
	}
	for _, e := range entries {
		rc, _ := e.Open()
		want, _ := io.ReadAll(rc)
		if got[e.Path] != string(want) {
			t.Errorf("%q: content mismatch", e.Path)
		}
	}
}

func TestModesRoundTrip(t *testing.T) {
	entries := []Entry{
		entryFrom("a", 0o644, "a"),
		entryFrom("b", 0o755, "b"),
		entryFrom("c", 0o600, "c"),
	}
	data := writeAll(t, entries)
	modes := map[string]os.FileMode{}
	err := ReadTar(bytes.NewReader(data), func(h Header, body io.Reader) error {
		modes[h.Path] = h.Mode
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if modes[e.Path] != e.Mode {
			t.Errorf("%q mode = %o, want %o", e.Path, modes[e.Path], e.Mode)
		}
	}
}

func TestNonASCIIStaysUSTAR(t *testing.T) {
	// This is the reason archive/tar is not used for writing: Go would emit a
	// PAX record here, which the browser's minimal reader cannot parse.
	data := writeAll(t, []Entry{entryFrom("ünïcode/файл.png", 0o644, "x")})
	if string(data[257:262]) != "ustar" {
		t.Fatalf("first record is not USTAR: %q", data[257:263])
	}
	if data[156] != typeRegular {
		t.Errorf("type flag = %q, want '0'", data[156])
	}
	got := readAll(t, data)
	if _, ok := got["ünïcode/файл.png"]; !ok {
		t.Errorf("unicode path not recovered, got %v", keys(got))
	}
}

func TestHeaderCarriesNoHostMetadata(t *testing.T) {
	data := writeAll(t, []Entry{entryFrom("a.txt", 0o644, "hello")})
	hdr := data[:blockSize]

	checks := []struct {
		name       string
		start, end int
	}{
		{"uid", 108, 116},
		{"gid", 116, 124},
		{"mtime", 136, 148},
	}
	for _, c := range checks {
		v, err := parseOctal(hdr[c.start:c.end])
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if v != 0 {
			t.Errorf("%s = %d, want 0 so that output cannot depend on the host", c.name, v)
		}
	}
	for _, c := range []struct {
		name       string
		start, end int
	}{{"uname", 265, 297}, {"gname", 297, 329}} {
		if s := trimNUL(hdr[c.start:c.end]); s != "" {
			t.Errorf("%s = %q, want empty", c.name, s)
		}
	}
}

func TestDeterministicOutput(t *testing.T) {
	build := func() []byte {
		return writeAll(t, []Entry{
			entryFrom("a.txt", 0o644, "alpha"),
			entryFrom("b/c.txt", 0o600, "beta"),
		})
	}
	if !bytes.Equal(build(), build()) {
		t.Fatal("two identical WriteTar calls produced different bytes")
	}
}

func TestDeterministicGzip(t *testing.T) {
	compress := func(payload string) []byte {
		var buf bytes.Buffer
		zw, err := NewDeterministicGzipWriter(&buf)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(zw, payload); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	a, b := compress("hello world"), compress("hello world")
	if !bytes.Equal(a, b) {
		t.Fatal("gzip output is not reproducible")
	}
	// The 4-byte MTIME field at offset 4 must be zero, and the OS byte at
	// offset 9 must be 255 ("unknown"), or output would vary by machine.
	if a[4] != 0 || a[5] != 0 || a[6] != 0 || a[7] != 0 {
		t.Errorf("gzip header records a modification time: % x", a[4:8])
	}
	if a[9] != 255 {
		t.Errorf("gzip OS byte = %d, want 255", a[9])
	}
	zr, err := gzip.NewReader(bytes.NewReader(a))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(zr)
	if string(got) != "hello world" {
		t.Errorf("round trip = %q", got)
	}
}

func TestWriteRejectsUnsafePaths(t *testing.T) {
	for _, p := range []string{"../escape", "/absolute", "a/../b", `back\slash`} {
		err := WriteTar(io.Discard, []Entry{entryFrom(p, 0o644, "x")})
		if err == nil {
			t.Errorf("WriteTar accepted unsafe path %q", p)
		}
	}
}

func TestWriteRejectsUSTAROverflow(t *testing.T) {
	long := "dir/" + strings.Repeat("n", 150) + ".png"
	err := WriteTar(io.Discard, []Entry{entryFrom(long, 0o644, "x")})
	if err == nil {
		t.Fatal("WriteTar accepted a path that does not fit a USTAR header")
	}
	if !strings.Contains(err.Error(), "USTAR") {
		t.Errorf("error should mention USTAR, got: %v", err)
	}
}

func TestWriteRejectsSizeMismatch(t *testing.T) {
	e := entryFrom("a.txt", 0o644, "four")
	e.Size = 99
	if err := WriteTar(io.Discard, []Entry{e}); err == nil {
		t.Fatal("WriteTar accepted an entry whose content did not match its declared size")
	}
}

func TestReadRejectsCorruptChecksum(t *testing.T) {
	data := writeAll(t, []Entry{entryFrom("a.txt", 0o644, "hello")})
	data[10] ^= 0xff // mutate the name, invalidating the checksum
	err := ReadTar(bytes.NewReader(data), func(Header, io.Reader) error { return nil })
	if err == nil {
		t.Fatal("ReadTar accepted a record with a bad checksum")
	}
	if !strings.Contains(err.Error(), "checksum") {
		t.Errorf("error should mention the checksum, got: %v", err)
	}
}

func TestReadRejectsNonUSTARRecord(t *testing.T) {
	data := writeAll(t, []Entry{entryFrom("a.txt", 0o644, "hello")})
	copy(data[257:263], "xxxxx\x00")
	err := ReadTar(bytes.NewReader(data), func(Header, io.Reader) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "USTAR") {
		t.Fatalf("want a USTAR magic error, got %v", err)
	}
}

func TestReadRejectsSymlinkRecord(t *testing.T) {
	data := writeAll(t, []Entry{entryFrom("a.txt", 0o644, "hello")})
	data[156] = '2' // symlink type flag
	fixChecksum(data[:blockSize])
	err := ReadTar(bytes.NewReader(data), func(Header, io.Reader) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "type flag") {
		t.Fatalf("want an unsupported-type error, got %v", err)
	}
}

func TestReadRejectsTraversalPath(t *testing.T) {
	// Hand-build a record whose name escapes the extraction root. WriteTar
	// refuses to produce one, so this is what a hostile file would look like.
	hdr, err := buildHeader("placeholder", 0o644, 5, typeRegular)
	if err != nil {
		t.Fatal(err)
	}
	copy(hdr[0:100], make([]byte, 100))
	copy(hdr[0:], "../../evil")
	fixChecksum(hdr)

	var buf bytes.Buffer
	buf.Write(hdr)
	buf.Write([]byte("evil\n"))
	buf.Write(make([]byte, blockSize-5))
	buf.Write(make([]byte, 2*blockSize))

	err = ReadTar(bytes.NewReader(buf.Bytes()), func(Header, io.Reader) error { return nil })
	if err == nil {
		t.Fatal("ReadTar accepted a traversal path")
	}
	if !strings.Contains(err.Error(), "unsafe archive path") {
		t.Errorf("want an unsafe-path error, got: %v", err)
	}
}

func TestReadRejectsAbsoluteAndDrivePaths(t *testing.T) {
	for _, evil := range []string{"/etc/passwd", `C:/Windows/system32/x`} {
		hdr, err := buildHeader("placeholder", 0o644, 0, typeRegular)
		if err != nil {
			t.Fatal(err)
		}
		copy(hdr[0:100], make([]byte, 100))
		copy(hdr[0:], evil)
		fixChecksum(hdr)
		var buf bytes.Buffer
		buf.Write(hdr)
		buf.Write(make([]byte, 2*blockSize))
		if err := ReadTar(bytes.NewReader(buf.Bytes()), func(Header, io.Reader) error { return nil }); err == nil {
			t.Errorf("ReadTar accepted %q", evil)
		}
	}
}

func TestReadRejectsTruncatedArchive(t *testing.T) {
	data := writeAll(t, []Entry{entryFrom("a.txt", 0o644, strings.Repeat("x", 700))})
	err := ReadTar(bytes.NewReader(data[:len(data)-1200]), func(Header, io.Reader) error { return nil })
	if err == nil {
		t.Fatal("ReadTar accepted a truncated archive")
	}
}

func TestSplitUSTARPrefersLongestPrefix(t *testing.T) {
	p := strings.Repeat("a", 60) + "/" + strings.Repeat("b", 60) + "/file.png"
	name, prefix, err := splitUSTAR(p)
	if err != nil {
		t.Fatal(err)
	}
	if prefix+"/"+name != p {
		t.Errorf("split does not reassemble: %q + / + %q != %q", prefix, name, p)
	}
	if len(prefix) > 155 || len(name) > 100 {
		t.Errorf("split exceeds USTAR fields: prefix %d, name %d", len(prefix), len(name))
	}
}

// fixChecksum recomputes a header's checksum after a test has mutated it.
func fixChecksum(hdr []byte) {
	for i := 148; i < 156; i++ {
		hdr[i] = ' '
	}
	var sum uint64
	for _, c := range hdr {
		sum += uint64(c)
	}
	copy(hdr[148:154], []byte(octal6(sum)))
	hdr[154] = 0
	hdr[155] = ' '
}

func octal6(v uint64) string {
	s := ""
	for i := 0; i < 6; i++ {
		s = string(rune('0'+(v&7))) + s
		v >>= 3
	}
	return s
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
