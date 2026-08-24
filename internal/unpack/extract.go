package unpack

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pridkett/slidepack/internal/diag"
	"github.com/pridkett/slidepack/internal/pathutil"
)

// ExtractOptions configures extraction.
type ExtractOptions struct {
	// Force permits writing into a destination that already has files in it.
	Force bool
	// DirMode is the permission applied to directories slidepack creates.
	DirMode os.FileMode
}

// ExtractResult summarises a completed extraction.
type ExtractResult struct {
	Destination string
	FileCount   int
	TotalBytes  int64
	Staged      bool
}

// Extract writes the decoded package to dest.
//
// When dest does not exist the files are built in a sibling staging directory
// and renamed into place, so an interrupted or failing extraction leaves no
// half-populated directory behind. When dest already exists (and --force was
// given) files are written in place, because renaming onto an existing
// directory is not an atomic operation on any supported platform.
func Extract(pkg *Package, dest string, opts ExtractOptions) (*ExtractResult, error) {
	if opts.DirMode == 0 {
		opts.DirMode = 0o755
	}
	if err := pkg.CheckPaths(); err != nil {
		return nil, err
	}

	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return nil, err
	}

	info, statErr := os.Lstat(destAbs)
	switch {
	case statErr != nil && !os.IsNotExist(statErr):
		return nil, statErr

	case statErr == nil && info.Mode()&os.ModeSymlink != 0:
		return nil, fmt.Errorf("destination %s is a symbolic link; refusing to extract through it", dest)

	case statErr == nil && !info.IsDir():
		return nil, fmt.Errorf("destination %s exists and is not a directory", dest)

	case statErr == nil:
		empty, err := isEmptyDir(destAbs)
		if err != nil {
			return nil, err
		}
		if !empty && !opts.Force {
			return nil, fmt.Errorf("destination %s is not empty; pass --force to write into it anyway", dest)
		}
		n, total, err := writeInto(pkg, destAbs, opts)
		if err != nil {
			return nil, err
		}
		return &ExtractResult{Destination: destAbs, FileCount: n, TotalBytes: total}, nil
	}

	// Destination does not exist: stage then rename.
	parent := filepath.Dir(destAbs)
	if err := os.MkdirAll(parent, opts.DirMode); err != nil {
		return nil, fmt.Errorf("creating %s: %w", parent, err)
	}
	staging, err := os.MkdirTemp(parent, "."+filepath.Base(destAbs)+".unpack-*")
	if err != nil {
		return nil, fmt.Errorf("creating staging directory: %w", err)
	}
	n, total, err := writeInto(pkg, staging, opts)
	if err != nil {
		os.RemoveAll(staging)
		return nil, err
	}
	if err := os.Chmod(staging, opts.DirMode); err != nil {
		os.RemoveAll(staging)
		return nil, err
	}
	if err := os.Rename(staging, destAbs); err != nil {
		os.RemoveAll(staging)
		return nil, fmt.Errorf("moving extracted files into place: %w", err)
	}
	return &ExtractResult{Destination: destAbs, FileCount: n, TotalBytes: total, Staged: true}, nil
}

func writeInto(pkg *Package, root string, opts ExtractOptions) (int, int64, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return 0, 0, err
	}
	// Resolve the root through any symlinks once, up front. From here on every
	// target is built with SafeJoin against this resolved root, so a symlinked
	// destination cannot be used to redirect individual files.
	if resolved, err := filepath.EvalSymlinks(rootAbs); err == nil {
		rootAbs = resolved
	}

	files := make([]File, len(pkg.Files))
	copy(files, pkg.Files)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	var total int64
	for _, f := range files {
		target, err := pathutil.SafeJoin(rootAbs, f.Path)
		if err != nil {
			return 0, 0, &Error{Code: diag.InvalidPath, Path: f.Path, Err: err}
		}
		if err := mkdirParents(rootAbs, f.Path, opts.DirMode); err != nil {
			return 0, 0, err
		}
		if err := writeFile(target, f.Data, modeOrDefault(f.Mode)); err != nil {
			return 0, 0, fmt.Errorf("writing %s: %w", f.Path, err)
		}
		total += int64(len(f.Data))
	}
	return len(files), total, nil
}

// modeOrDefault guards against an archive that records mode 0, which would
// produce a file nobody can read.
func modeOrDefault(m os.FileMode) os.FileMode {
	p := m.Perm()
	if p == 0 {
		return 0o644
	}
	return p
}

// mkdirParents creates the directories leading to a package path, one
// component at a time.
//
// os.MkdirAll would happily traverse an existing symlink; creating each
// component explicitly and rejecting any that is a symlink means a hostile
// archive cannot use a directory entry it planted earlier to escape the root.
func mkdirParents(root, pkgPath string, mode os.FileMode) error {
	dir := ""
	if i := strings.LastIndex(pkgPath, "/"); i >= 0 {
		dir = pkgPath[:i]
	}
	if dir == "" {
		return nil
	}
	cur := root
	for _, seg := range strings.Split(dir, "/") {
		cur = filepath.Join(cur, seg)
		info, err := os.Lstat(cur)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(cur, mode); err != nil && !errors.Is(err, os.ErrExist) {
				return fmt.Errorf("creating directory %s: %w", cur, err)
			}
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to extract through the symbolic link %s", cur)
		}
		if !info.IsDir() {
			return fmt.Errorf("cannot create directory %s: a file of that name already exists", cur)
		}
	}
	return nil
}

func writeFile(target string, data []byte, mode os.FileMode) error {
	if info, err := os.Lstat(target); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to write through the symbolic link %s", target)
		}
		if info.IsDir() {
			return fmt.Errorf("%s already exists as a directory", target)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// The open above is subject to umask; chmod sets the recorded bits exactly
	// so that mode round-trips.
	return os.Chmod(target, mode)
}

func isEmptyDir(dir string) (bool, error) {
	f, err := os.Open(dir)
	if err != nil {
		return false, err
	}
	defer f.Close()
	names, err := f.Readdirnames(1)
	if errors.Is(err, io.EOF) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return len(names) == 0, nil
}
