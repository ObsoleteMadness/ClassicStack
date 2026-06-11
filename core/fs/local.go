package fs

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
)

// local_fs is the first real backend in the registry: a host directory tree
// rooted at ShareSpec.Path. It speaks the store-native FileSystem contract over
// '/'-joined, share-relative paths and maps them onto the OS filesystem, so the
// fork engines / name engines / filename codec assembled above it (BuildShare)
// behave identically to the memfs reference backend. memfs stays for tests; this
// is what an AFP/SMB volume backed by a real directory uses.
//
// ShareSpec.Path is required and must be an existing directory. Paths are joined
// under the root with traversal protection: any element resolving outside the
// root is rejected with fs.ErrInvalid, so a malformed wire path can never escape
// the share.
type localFS struct {
	root string
}

// ErrPathEscape is returned when a share-relative path resolves outside the
// share root after cleaning (a path-traversal attempt).
var ErrPathEscape = errors.New("fs: path escapes share root")

func newLocalFS(spec ShareSpec) (*localFS, error) {
	root := spec.Path
	if root == "" {
		return nil, errors.New("fs: local_fs requires a path")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("fs: local_fs path is not a directory")
	}
	return &localFS{root: abs}, nil
}

// host maps a '/'-joined, share-relative store path to an absolute host path
// under the root, rejecting any path that escapes the root.
func (l *localFS) host(p string) (string, error) {
	// Store paths are always '/'-separated and share-relative; strip a leading
	// '/' so filepath.Join treats it as relative to the root.
	clean := strings.TrimPrefix(p, "/")
	// Reject NUL and Windows volume/UNC roots before joining.
	if strings.ContainsRune(clean, 0) {
		return "", fs.ErrInvalid
	}
	full := filepath.Join(l.root, filepath.FromSlash(clean))
	// filepath.Join already cleans "..", so confirm the result is still within
	// the root (defence in depth against symlink-free traversal).
	rel, err := filepath.Rel(l.root, full)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrPathEscape
	}
	return full, nil
}

func (l *localFS) ReadDir(path string) ([]fs.DirEntry, error) {
	h, err := l.host(path)
	if err != nil {
		return nil, err
	}
	return os.ReadDir(h)
}

func (l *localFS) Stat(path string) (fs.FileInfo, error) {
	h, err := l.host(path)
	if err != nil {
		return nil, err
	}
	return os.Stat(h)
}

func (l *localFS) DiskUsage(path string) (total, free uint64, err error) {
	// Real per-volume usage is platform-specific (statfs/GetDiskFreeSpaceEx);
	// the OS adapter layer can refine this. Report 0/0 (unknown) for now, which
	// AFP/SMB treat as "very large" rather than failing the mount.
	_ = path
	return 0, 0, nil
}

func (l *localFS) CreateDir(path string) error {
	h, err := l.host(path)
	if err != nil {
		return err
	}
	return os.Mkdir(h, 0o755)
}

func (l *localFS) CreateFile(path string) (File, error) {
	h, err := l.host(path)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(h, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}
	return &localFile{f: f}, nil
}

func (l *localFS) OpenFile(path string, flag int) (File, error) {
	h, err := l.host(path)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(h, flag, 0o644)
	if err != nil {
		return nil, err
	}
	return &localFile{f: f}, nil
}

func (l *localFS) Remove(path string) error {
	h, err := l.host(path)
	if err != nil {
		return err
	}
	return os.Remove(h)
}

func (l *localFS) Rename(old, new string) error {
	ho, err := l.host(old)
	if err != nil {
		return err
	}
	hn, err := l.host(new)
	if err != nil {
		return err
	}
	return os.Rename(ho, hn)
}

// ShortName/MediumName are passthroughs: the assembled shareFS overrides them
// with the configured NameEngine (BuildShare), so the backend's own derivation
// is unused — mirror memfs and return the path unchanged.
func (l *localFS) ShortName(path string) (string, error)  { return path, nil }
func (l *localFS) MediumName(path string) (string, error) { return path, nil }

func (l *localFS) Capabilities() Capabilities {
	return Capabilities{ChildCount: true, CatSearch: true}
}

// CatSearch satisfies the optional CatSearcher capability with the default
// predicate tree-walk over the host directory. local_fs is a plain hierarchical
// store, so WalkCatSearch (which descends through the backend's own ReadDir, and
// thus the traversal guard) is exactly right.
func (l *localFS) CatSearch(crit CatSearchCriteria, cursor CatSearchCursor) ([]CatSearchResult, CatSearchCursor, error) {
	return WalkCatSearch(l, crit, cursor)
}

// localFile wraps *os.File, which already satisfies positional ReadAt/WriteAt.
type localFile struct {
	f *os.File
}

func (f *localFile) ReadAt(p []byte, off int64) (int, error)  { return f.f.ReadAt(p, off) }
func (f *localFile) WriteAt(p []byte, off int64) (int, error) { return f.f.WriteAt(p, off) }
func (f *localFile) Truncate(size int64) error                { return f.f.Truncate(size) }
func (f *localFile) Stat() (fs.FileInfo, error)               { return f.f.Stat() }
func (f *localFile) Sync() error                              { return f.f.Sync() }
func (f *localFile) Close() error                             { return f.f.Close() }

func init() {
	RegisterFSWithParams("local_fs", func(spec ShareSpec, b bus.Bus, store metastore.Store) (FileSystem, error) {
		_ = b
		_ = store
		return newLocalFS(spec)
	}, Param{Key: PathKey, Required: true, Doc: "host directory served as the share root"})
}
