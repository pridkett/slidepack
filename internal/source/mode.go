package source

import (
	"os"
	"runtime"
)

// Canonical permission bits. slidepack records one of these three for every
// file, so that a tree packed on a filesystem without POSIX permissions
// produces the same bytes as the same tree packed on one that has them.
const (
	ModeFile       os.FileMode = 0o644
	ModeExecutable os.FileMode = 0o755
	ModeReadOnly   os.FileMode = 0o444
)

// hostPreservesModes reports whether this platform's file modes carry POSIX
// permission bits at all.
//
// Windows does not. Go synthesises a mode there from a single filesystem
// attribute: FILE_ATTRIBUTE_READONLY becomes 0444 and its absence becomes
// 0666, and os.Chmod reads only the 0200 bit to toggle that attribute back.
// Nothing finer exists to record or restore.
var hostPreservesModes = runtime.GOOS != "windows"

// CanonicalMode maps a filesystem permission set onto the bits slidepack
// records for a source file.
//
// posixModes says whether the host filesystem can express POSIX permissions.
// When it can, the file's own bits are canonical and are recorded verbatim.
// When it cannot, the only real distinction available is read-only versus
// writable, so it is mapped onto 0444 and 0644.
//
// Without this mapping, packing on Windows would record 0666 for every file —
// which is not a permission the user chose, merely how Go spells "writable"
// there. Two consequences follow, and both are worth avoiding: the same tree
// would pack to different bytes on Windows and on Linux, defeating the
// reproducibility guarantee; and unpacking a Windows-packed presentation on
// Linux would produce world-writable files.
//
// The parameter is explicit rather than read from runtime.GOOS inside the
// function so that both branches can be tested on any platform.
func CanonicalMode(perm os.FileMode, posixModes bool) os.FileMode {
	if posixModes {
		return perm.Perm()
	}
	if perm.Perm()&0o200 != 0 {
		return ModeFile
	}
	return ModeReadOnly
}
