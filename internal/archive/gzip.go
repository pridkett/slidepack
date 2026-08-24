package archive

import (
	"compress/gzip"
	"fmt"
	"io"
)

// GzipLevel is pinned so that output depends only on input bytes. Changing it
// changes every packed file, so it is part of the format's compatibility
// surface even though decompressors do not care.
const GzipLevel = gzip.BestCompression

// NewDeterministicGzipWriter returns a gzip writer whose 10-byte header
// contains no machine- or time-dependent data.
//
// The gzip header can carry a modification time, an original file name and a
// comment. All three are left empty, so ModTime serialises as 0. Go's writer
// already defaults the OS byte to 255 ("unknown"), which is what we want:
// writing the real OS byte would make Linux and macOS output differ.
func NewDeterministicGzipWriter(w io.Writer) (*gzip.Writer, error) {
	zw, err := gzip.NewWriterLevel(w, GzipLevel)
	if err != nil {
		return nil, fmt.Errorf("configuring gzip writer: %w", err)
	}
	zw.Header = gzip.Header{OS: 255}
	return zw, nil
}
