package manifest

import (
	"bytes"
	"io"
)

// newTrimReader strips surrounding whitespace so that a manifest pretty-printed
// inside an HTML document parses without the decoder tripping over indentation.
func newTrimReader(data []byte) io.Reader {
	return bytes.NewReader(bytes.TrimSpace(data))
}
