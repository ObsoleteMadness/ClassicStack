package fs

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	bus  bus.Bus // FS-mutation bus (§10d); publishes Create/Modify/Rename/Delete. May be nil.
}

// ErrPathEscape is returned when a share-relative path resolves outside the
// share root after cleaning (a path-traversal attempt).
var ErrPathEscape = errors.New("fs: path escapes share root")

func newLocalFS(spec ShareSpec, b bus.Bus) (*localFS, error) {
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
	return &localFS{root: abs, bus: b}, nil
}

// publish emits an FS-mutation Event for a store path onto the §10d bus, if one is
// wired. hostPath is the absolute host path the mutation touched; oldHost is set
// only for a rename. The Origin is left blank: the service-supplied OriginBus
// wrapper stamps "afp"/"smb" so reactors can filter their own events.
func (l *localFS) publish(op Op, hostPath, oldHost string) {
	if l.bus == nil {
		return
	}
	l.bus.Publish(Event{Op: op, HostPath: hostPath, OldPath: oldHost, Time: time.Now()})
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

// HostPath implements fs.HostPather: it exposes the absolute host path for a
// store path so the DOS-attribute / shortname interop backends (Windows-native,
// Samba xattr) can reach the real file. ok is false when the path escapes the
// root (the caller then declines the host-native backend).
func (l *localFS) HostPath(storePath string) (string, bool) {
	h, err := l.host(storePath)
	if err != nil {
		return "", false
	}
	return h, true
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
	if err := os.Mkdir(h, 0o755); err != nil {
		return err
	}
	l.publish(OpCreate, h, "")
	return nil
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
	l.publish(OpCreate, h, "")
	// Hand the file a publish hook so a subsequent write+close emits OpModify.
	return &localFile{f: f, fs: l, host: h}, nil
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
	// The file publishes OpModify on close only if it was actually written to
	// (dirty); a read-only open that never writes stays silent regardless of flags.
	return &localFile{f: f, fs: l, host: h}, nil
}

func (l *localFS) Remove(path string) error {
	h, err := l.host(path)
	if err != nil {
		return err
	}
	if err := os.Remove(h); err != nil {
		return err
	}
	l.publish(OpDelete, h, "")
	return nil
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
	if err := os.Rename(ho, hn); err != nil {
		return err
	}
	l.publish(OpRename, hn, ho)
	return nil
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

// localFile wraps *os.File, which already satisfies positional ReadAt/WriteAt. It
// holds a back-reference to its localFS + host path so a write-then-close publishes
// one OpModify on the §10d bus (a per-WriteAt event would flood the bus; coalescing
// to close is the right granularity for a change-notify).
type localFile struct {
	f     *os.File
	fs    *localFS
	host  string
	dirty bool // a write/truncate happened, so Close should publish OpModify
}

func (f *localFile) ReadAt(p []byte, off int64) (int, error) { return f.f.ReadAt(p, off) }
func (f *localFile) WriteAt(p []byte, off int64) (int, error) {
	n, err := f.f.WriteAt(p, off)
	if n > 0 {
		f.dirty = true
	}
	return n, err
}
func (f *localFile) Truncate(size int64) error {
	f.dirty = true
	return f.f.Truncate(size)
}
func (f *localFile) Stat() (fs.FileInfo, error) { return f.f.Stat() }
func (f *localFile) Sync() error                { return f.f.Sync() }
func (f *localFile) Close() error {
	err := f.f.Close()
	// Publish the modification after the data is flushed/closed, so a same-path
	// reactor that re-stats the file sees the post-write state. A Create already
	// emitted OpCreate; a subsequent write still emits OpModify (create+write is two
	// events, matching how a host watcher would observe it).
	if f.dirty && f.fs != nil {
		f.fs.publish(OpModify, f.host, "")
	}
	return err
}

func init() {
	RegisterFSWithParams("local_fs", func(spec ShareSpec, b bus.Bus, store metastore.Store) (FileSystem, error) {
		_ = store
		return newLocalFS(spec, b)
	}, Param{Key: PathKey, Required: true, Doc: "host directory served as the share root"})
}
