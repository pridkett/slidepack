package source

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pridkett/slidepack/internal/pathutil"
)

// DefaultEntrypoint is the entry document assumed when --entry is not given.
const DefaultEntrypoint = "index.html"

// excludedNames are never packed. Keeping this list short and fixed matters
// for reproducibility: .DS_Store and friends appear and disappear depending on
// which machine last looked at the directory, so packing them would make
// output depend on the host rather than on the source.
var excludedNames = map[string]bool{
	".DS_Store":   true,
	"Thumbs.db":   true,
	"desktop.ini": true,
}

// excludedDirs are skipped along with everything beneath them.
var excludedDirs = map[string]bool{
	".git": true,
	".hg":  true,
	".svn": true,
}

// Entry is one regular file in a source tree.
type Entry struct {
	// Path is the canonical package path.
	Path string
	// Mode holds the permission bits (mode & 0777).
	Mode os.FileMode
	// Size is the length in bytes.
	Size int64
	// FSPath is the host filesystem path, empty for in-memory trees.
	FSPath string
}

// Tree is a read-only source tree, either on disk or reconstructed in memory
// from a packed archive. Both are validated by exactly the same code, which is
// what lets `validate presentation.html` check the same rules as
// `validate ./source`.
type Tree interface {
	// Entries returns every regular file, sorted by path.
	Entries() []Entry
	// Read returns the contents of a package path.
	Read(path string) ([]byte, error)
	// Has reports whether a package path exists.
	Has(path string) bool
}

// ErrSpecialFile reports a filesystem object slidepack refuses to archive.
type ErrSpecialFile struct {
	Path string
	Kind string
}

func (e *ErrSpecialFile) Error() string {
	return fmt.Sprintf("%s: %s is not a regular file or directory; slidepack v1 archives only regular files", e.Path, e.Kind)
}

// DiskTree is a source tree rooted at a host directory.
type DiskTree struct {
	Root    string
	entries []Entry
	index   map[string]Entry
}

// LoadDiskTree walks root and records every regular file.
//
// The walk uses lstat semantics so that a symlink is seen as a symlink rather
// than as whatever it points at. Symlinks are rejected outright: following one
// could pull in data from outside the source tree, and recording one would
// make the archive's meaning depend on the extraction environment.
func LoadDiskTree(root string) (*DiskTree, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("cannot read source directory %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", root)
	}

	t := &DiskTree{Root: abs, index: map[string]Entry{}}
	err = filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == abs {
			return nil
		}
		rel, rerr := filepath.Rel(abs, p)
		if rerr != nil {
			return rerr
		}
		pkg := pathutil.FromFS(rel)
		name := d.Name()

		if d.IsDir() {
			if excludedDirs[name] {
				return filepath.SkipDir
			}
			return nil
		}
		if excludedNames[name] {
			return nil
		}
		typ := d.Type()
		switch {
		case typ&os.ModeSymlink != 0:
			return &ErrSpecialFile{Path: pkg, Kind: "a symbolic link"}
		case typ&os.ModeDevice != 0:
			return &ErrSpecialFile{Path: pkg, Kind: "a device node"}
		case typ&os.ModeNamedPipe != 0:
			return &ErrSpecialFile{Path: pkg, Kind: "a FIFO"}
		case typ&os.ModeSocket != 0:
			return &ErrSpecialFile{Path: pkg, Kind: "a socket"}
		case typ&os.ModeIrregular != 0:
			return &ErrSpecialFile{Path: pkg, Kind: "an irregular file"}
		case !typ.IsRegular():
			return &ErrSpecialFile{Path: pkg, Kind: "a special filesystem object"}
		}
		fi, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		// A hard link is indistinguishable from a regular file by mode alone.
		// It is left alone deliberately: the archive stores contents, so a
		// hard-linked file round-trips as two independent regular files, which
		// is a faithful representation of the source bytes.
		//
		// The mode is canonicalised here, at the one place a host filesystem
		// is read, so the manifest and the archive cannot disagree about it.
		e := Entry{
			Path:   pkg,
			Mode:   CanonicalMode(fi.Mode(), hostPreservesModes),
			Size:   fi.Size(),
			FSPath: p,
		}
		t.entries = append(t.entries, e)
		t.index[pkg] = e
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(t.entries, func(i, j int) bool { return t.entries[i].Path < t.entries[j].Path })
	return t, nil
}

// Entries implements Tree.
func (t *DiskTree) Entries() []Entry { return t.entries }

// Read implements Tree.
func (t *DiskTree) Read(p string) ([]byte, error) {
	e, ok := t.index[p]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return os.ReadFile(e.FSPath)
}

// Has implements Tree.
func (t *DiskTree) Has(p string) bool {
	_, ok := t.index[p]
	return ok
}

// MemTree is a source tree held entirely in memory.
type MemTree struct {
	entries []Entry
	data    map[string][]byte
}

// NewMemTree builds an in-memory tree.
func NewMemTree() *MemTree {
	return &MemTree{data: map[string][]byte{}}
}

// Add records a file. Later adds of the same path replace earlier ones.
func (t *MemTree) Add(p string, mode os.FileMode, data []byte) {
	if _, exists := t.data[p]; !exists {
		t.entries = append(t.entries, Entry{Path: p, Mode: mode.Perm(), Size: int64(len(data))})
	}
	t.data[p] = data
}

// Sort orders entries canonically. Call once after the last Add.
func (t *MemTree) Sort() {
	sort.Slice(t.entries, func(i, j int) bool { return t.entries[i].Path < t.entries[j].Path })
}

// Entries implements Tree.
func (t *MemTree) Entries() []Entry { return t.entries }

// Read implements Tree.
func (t *MemTree) Read(p string) ([]byte, error) {
	b, ok := t.data[p]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return b, nil
}

// Has implements Tree.
func (t *MemTree) Has(p string) bool {
	_, ok := t.data[p]
	return ok
}

// DescribeExclusions renders the exclusion list for help text and docs.
func DescribeExclusions() string {
	var names []string
	for n := range excludedNames {
		names = append(names, n)
	}
	for n := range excludedDirs {
		names = append(names, n+"/")
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
