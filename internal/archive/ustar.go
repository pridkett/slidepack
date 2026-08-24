// Package archive implements the deterministic USTAR + gzip payload used by
// slidepack format v1.
//
// The standard library's archive/tar is deliberately not used for writing.
// Go refuses to emit a USTAR record whose name is not pure ASCII and silently
// upgrades it to PAX (see verifyString in archive/tar/common.go). slidepack
// requires Unicode file names AND requires that every record be plain USTAR,
// because the browser runtime ships a ~100 line TAR reader that understands
// nothing else. Writing the 512-byte headers directly is smaller than working
// around the negotiation, and it makes every byte of the output explicit,
// which is what determinism demands.
package archive

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/pwagstro/slidepack/internal/pathutil"
)

const (
	blockSize = 512

	// Fixed, host-independent header values. Nothing about the machine that
	// ran the pack may leak into the archive.
	fixedUID   = 0
	fixedGID   = 0
	fixedMtime = 0

	typeRegular = '0'
	typeDir     = '5'
)

// Entry describes one regular file to place in the archive.
type Entry struct {
	// Path is the canonical package path. Must pass pathutil.Check.
	Path string
	// Mode holds the permission bits to record; only mode&0777 is used.
	Mode os.FileMode
	// Size is the exact number of bytes Open will yield.
	Size int64
	// Open returns the file contents. It is called at most once.
	Open func() (io.ReadCloser, error)
}

// Header is a decoded USTAR record.
type Header struct {
	Path     string
	Mode     os.FileMode
	Size     int64
	TypeFlag byte
}

// WriteTar streams entries into w as a USTAR archive.
//
// Entries must already be sorted by path; WriteTar does not sort, so that
// callers keep explicit control over the canonical ordering. Contents are
// copied straight from Open to w, so memory use is independent of file size.
func WriteTar(w io.Writer, entries []Entry) error {
	for _, e := range entries {
		if err := pathutil.Check(e.Path); err != nil {
			return fmt.Errorf("archive entry %q: %w", e.Path, err)
		}
		if err := pathutil.CheckUSTAR(e.Path); err != nil {
			return fmt.Errorf("archive entry %q: %w", e.Path, err)
		}
		hdr, err := buildHeader(e.Path, e.Mode, e.Size, typeRegular)
		if err != nil {
			return fmt.Errorf("archive entry %q: %w", e.Path, err)
		}
		if _, err := w.Write(hdr); err != nil {
			return err
		}
		rc, err := e.Open()
		if err != nil {
			return fmt.Errorf("archive entry %q: %w", e.Path, err)
		}
		n, err := io.Copy(w, rc)
		cerr := rc.Close()
		if err != nil {
			return fmt.Errorf("archive entry %q: %w", e.Path, err)
		}
		if cerr != nil {
			return fmt.Errorf("archive entry %q: %w", e.Path, cerr)
		}
		if n != e.Size {
			return fmt.Errorf("archive entry %q: declared %d bytes but read %d", e.Path, e.Size, n)
		}
		if pad := padding(e.Size); pad > 0 {
			if _, err := w.Write(make([]byte, pad)); err != nil {
				return err
			}
		}
	}
	// Two zero blocks terminate the archive.
	if _, err := w.Write(make([]byte, 2*blockSize)); err != nil {
		return err
	}
	return nil
}

func padding(size int64) int64 {
	if r := size % blockSize; r != 0 {
		return blockSize - r
	}
	return 0
}

// buildHeader renders one 512-byte USTAR record.
func buildHeader(p string, mode os.FileMode, size int64, typeflag byte) ([]byte, error) {
	name, prefix, err := splitUSTAR(p)
	if err != nil {
		return nil, err
	}
	if size < 0 {
		return nil, errors.New("negative size")
	}
	// 8**11-1 is the largest value an 11-digit octal field can hold: 8 GiB.
	if size > (1<<33)-1 {
		return nil, fmt.Errorf("file size %d exceeds the USTAR octal field limit", size)
	}

	b := make([]byte, blockSize)
	copy(b[0:100], name)
	putOctal(b[100:108], uint64(mode.Perm()))
	putOctal(b[108:116], fixedUID)
	putOctal(b[116:124], fixedGID)
	putOctal(b[124:136], uint64(size))
	putOctal(b[136:148], fixedMtime)
	b[156] = typeflag
	copy(b[257:263], "ustar\x00")
	copy(b[263:265], "00")
	// uname/gname/devmajor/devminor are deliberately left as NULs: recording a
	// user name would make output depend on the machine that packed it.
	copy(b[345:500], prefix)

	// Checksum is computed with the checksum field itself read as 8 spaces.
	for i := 148; i < 156; i++ {
		b[i] = ' '
	}
	var sum uint64
	for _, c := range b {
		sum += uint64(c)
	}
	// Historical layout: six octal digits, NUL, space.
	chk := fmt.Sprintf("%06o", sum)
	copy(b[148:154], chk)
	b[154] = 0
	b[155] = ' '
	return b, nil
}

func putOctal(field []byte, v uint64) {
	// n-1 octal digits, zero padded, then a NUL terminator.
	s := strconv.FormatUint(v, 8)
	width := len(field) - 1
	for i := 0; i < width-len(s); i++ {
		field[i] = '0'
	}
	copy(field[width-len(s):width], s)
	field[width] = 0
}

// splitUSTAR divides a package path into the name (<=100 bytes) and prefix
// (<=155 bytes) fields. Unlike archive/tar it does not require ASCII: UTF-8
// bytes are stored verbatim, which is what every mainstream tar does in
// practice and what the browser reader expects.
func splitUSTAR(p string) (name, prefix string, err error) {
	if len(p) <= 100 {
		return p, "", nil
	}
	if err := pathutil.CheckUSTAR(p); err != nil {
		return "", "", err
	}
	// Prefer the split that keeps the prefix as long as possible, i.e. the last
	// separator that still leaves a name of at most 100 bytes.
	best := -1
	for i, c := range []byte(p) {
		if c != '/' {
			continue
		}
		nameLen := len(p) - i - 1
		if i > 0 && i <= 155 && nameLen > 0 && nameLen <= 100 {
			best = i
		}
	}
	if best < 0 {
		return "", "", fmt.Errorf("path %q cannot be represented as a USTAR name/prefix pair", p)
	}
	return p[best+1:], p[:best], nil
}

// ReadTar streams a USTAR archive, invoking fn for each regular file.
//
// The reader is intentionally strict: anything other than a regular file or a
// directory entry is an error, because a slidepack archive is only ever
// produced by WriteTar and any deviation means corruption or tampering.
// Paths are validated before fn sees them.
func ReadTar(r io.Reader, fn func(h Header, body io.Reader) error) error {
	hdrBuf := make([]byte, blockSize)
	for {
		if _, err := io.ReadFull(r, hdrBuf); err != nil {
			if errors.Is(err, io.EOF) {
				return errors.New("truncated archive: missing end-of-archive marker")
			}
			return fmt.Errorf("reading tar header: %w", err)
		}
		if isZeroBlock(hdrBuf) {
			// One zero block signals the end; a well-formed archive has two.
			// Trailing bytes after that are ignored, as tar readers normally do.
			return nil
		}
		h, err := parseHeader(hdrBuf)
		if err != nil {
			return err
		}
		if h.TypeFlag == typeDir {
			continue
		}
		lr := io.LimitReader(r, h.Size)
		if err := fn(h, lr); err != nil {
			return err
		}
		// Drain anything fn did not consume, then the padding.
		if _, err := io.Copy(io.Discard, lr); err != nil {
			return fmt.Errorf("reading %q: %w", h.Path, err)
		}
		if pad := padding(h.Size); pad > 0 {
			if _, err := io.CopyN(io.Discard, r, pad); err != nil {
				return fmt.Errorf("truncated padding after %q: %w", h.Path, err)
			}
		}
	}
}

func isZeroBlock(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}

func parseHeader(b []byte) (Header, error) {
	var h Header
	if string(b[257:262]) != "ustar" {
		return h, errors.New("corrupt archive: record is not a USTAR header")
	}
	if err := verifyChecksum(b); err != nil {
		return h, err
	}
	name := trimNUL(b[0:100])
	prefix := trimNUL(b[345:500])
	if prefix != "" {
		h.Path = prefix + "/" + name
	} else {
		h.Path = name
	}
	h.TypeFlag = b[156]
	if h.TypeFlag == 0 {
		// Historical tars used NUL for regular files.
		h.TypeFlag = typeRegular
	}
	switch h.TypeFlag {
	case typeRegular, typeDir:
	default:
		return h, fmt.Errorf("archive entry %q has unsupported type flag %q; slidepack archives contain only regular files", h.Path, string(h.TypeFlag))
	}
	if h.TypeFlag == typeDir {
		h.Path = strings.TrimSuffix(h.Path, "/")
	}
	mode, err := parseOctal(b[100:108])
	if err != nil {
		return h, fmt.Errorf("archive entry %q: bad mode field: %w", h.Path, err)
	}
	h.Mode = os.FileMode(mode).Perm()
	size, err := parseOctal(b[124:136])
	if err != nil {
		return h, fmt.Errorf("archive entry %q: bad size field: %w", h.Path, err)
	}
	if size > 1<<40 {
		return h, fmt.Errorf("archive entry %q: implausible size %d", h.Path, size)
	}
	h.Size = int64(size)
	if h.TypeFlag == typeRegular {
		if err := pathutil.Check(h.Path); err != nil {
			return h, fmt.Errorf("unsafe archive path %q: %w", h.Path, err)
		}
	}
	return h, nil
}

func verifyChecksum(b []byte) error {
	stored, err := parseOctal(b[148:156])
	if err != nil {
		return fmt.Errorf("corrupt archive: unreadable header checksum: %w", err)
	}
	var unsigned, signed int64
	for i, c := range b {
		if i >= 148 && i < 156 {
			c = ' '
		}
		unsigned += int64(c)
		signed += int64(int8(c))
	}
	if int64(stored) != unsigned && int64(stored) != signed {
		return fmt.Errorf("corrupt archive: header checksum mismatch (stored %d, computed %d)", stored, unsigned)
	}
	return nil
}

func trimNUL(b []byte) string {
	if i := indexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}

func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

func parseOctal(field []byte) (uint64, error) {
	s := strings.Trim(string(field), " \x00")
	if s == "" {
		return 0, nil
	}
	v, err := strconv.ParseUint(s, 8, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not an octal number", s)
	}
	return v, nil
}
