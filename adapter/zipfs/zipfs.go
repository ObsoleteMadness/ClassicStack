//go:build zipfs || all

// Package zipfs implements a core/fs FileSystem backend whose entire directory tree
// lives inside a single .zip archive (ShareSpec.Path names the file). It registers
// itself into the core/fs factory registry under the "zipfs" fs_type and is gated
// behind the `zipfs` build tag, so a build without the tag never links archive/zip
// or its compress/flate dependency.
//
// It lives in adapter/ (not core/) because archive/zip transitively pulls
// compress/flate → encoding/binary → reflect, which the core ring forbids
// (§1 / archtest); the design places the heavy/tag-gated FS backends at this layer
// (.refactor/00-DESIGN.md). zipfs is the smallest possible real, mutating backend —
// it exercises the whole §9 storage seam (fork engine, name engine, metastore,
// codec, DOS-attr store assembled by BuildShare) with NO host directory and NO
// sqlite, so it is the canonical check that the VFS structure works standalone.
//
// Memory model (the whole point of this backend's shape): the archive is NEVER read
// fully into RAM — a 2 GiB volume must not cost 2 GiB of memory — AND the backend holds
// NO long-lived OS handle on the archive between calls. (The core/fs FSCloser seam DOES
// give a Stop-time Close — implemented below to flush pending writes — but zipfs is
// deliberately correct WITHOUT relying on it: a pinned descriptor would otherwise lock
// the file on Windows and keep one open across a slow client.) zipfs keeps only a
// lightweight metadata overlay (member size + modtime, from the central directory);
// member bytes live on disk and are opened on demand. The ZIP format constrains how far
// we can take this:
//
//   - Reads STREAM. Each read handle opens its OWN short-lived zip.Reader (parsing only
//     the central directory, not member bodies) and owns it for the handle's lifetime.
//     A member's bytes are inflated on demand behind a forward inflate cursor; a
//     backward ReadAt reopens the member and re-inflates to the offset. Peak RAM ≈ one
//     member buffer; one transient file descriptor per open read handle.
//   - Writes can NOT be random-access inside a zip: a deflate stream cannot be patched
//     in place, and replacing member K rewrites every member after it. So a file
//     opened for write is STAGED in a host temp file (random-access WriteAt there).
//     On flush the archive is rewritten by STREAMING member-by-member from the old
//     zip into a new one (Writer.Copy copies unchanged members raw — no re-deflate),
//     substituting the dirty/new members and dropping tombstoned ones, then atomic
//     rename. Peak RAM ≈ one buffer; transient disk ≈ archive size during the repack.
//
// AppleDouble sidecars only: the resource fork / Finder-info / comment for a file is
// stored as a "._name" entry written THROUGH this FileSystem (the appledouble fork
// engine BuildShare assembles over us — itself just more zip members). zipfs forces
// ForkBackend="appledouble" and Metastore="mem" at registration so a zipfs share
// never depends on a host-native fork store or sqlite — that is the point of the
// backend (the CLAUDE.md note: "it must always use sidecars … run without sqlite").
package zipfs

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	iofs "io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	corefs "github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
)

// FSType is the fs_type token the zipfs backend registers under.
const FSType = "zipfs"

func init() {
	// Register the backend into the core/fs factory registry (the §9 storage seam).
	// PathKey is required: zipfs needs the .zip archive location. The factory pins
	// the fork backend to appledouble and the metastore to mem so a zipfs share is
	// self-contained (sidecars in the archive, no sqlite) regardless of the
	// share-level defaults — see the package doc and CLAUDE.md.
	//
	// The Validator declares zipfs's own constraint (no longer hardcoded in core): a
	// read-only zip cannot host native/xattr/ads forks (nothing can be written), so
	// resource forks must come from AppleDouble sidecars baked into the archive.
	corefs.RegisterFSWithValidator(FSType,
		func(spec corefs.ShareSpec, b bus.Bus, _ metastore.Store) (corefs.FileSystem, error) {
			return newZipFS(spec, b)
		},
		validateZipFSSpec,
		corefs.Param{Key: corefs.PathKey, Required: true, Doc: "path to the .zip archive served as the share root"},
	)
}

// validateZipFSSpec rejects a read-only zipfs share that does not use the appledouble
// fork backend. A read-only archive cannot be written, so forks must be pre-baked
// AppleDouble sidecars; a native/xattr/ads backend would have nowhere to store them.
func validateZipFSSpec(c corefs.SpecConstraints) error {
	if c.Spec.ReadOnly && c.ForkBackend != "appledouble" {
		return errors.New("fs: read-only zipfs requires appledouble fork backend")
	}
	return nil
}

// ErrReadOnly is returned for a mutating op on a read-only zipfs share.
var ErrReadOnly = errors.New("zipfs: archive is read-only")

// Compile-time assertions: zipFS is a FileSystem and opts into the optional FSCloser
// teardown seam (its Close flushes pending writes; the file services call it at Stop).
var (
	_ corefs.FileSystem = (*zipFS)(nil)
	_ corefs.FSCloser   = (*zipFS)(nil)
)

// zipFS serves a .zip archive without reading its member bodies into RAM AND without
// holding a long-lived OS handle on the archive between calls (a persistent handle would
// lock the file on Windows and stay open across a slow client; the FSCloser Close below
// only flushes pending writes at Stop). The in-memory state is only the lightweight
// overlay: the parsed central
// directory as member METADATA (size + modtime, not data, not *zip.File), a set of
// staged temp files for written/created members, dir bookkeeping, and tombstones for
// removed/renamed-away members. Member DATA always lives on disk — in the original
// .zip (re-opened per read handle, streamed) or in a per-member host temp file (dirty
// members). A read handle owns its own short-lived archive reader for its lifetime.
type zipFS struct {
	path     string
	readOnly bool
	bus      bus.Bus

	mu sync.RWMutex
	// meta maps a store path to its original member metadata (no handle, no data), for
	// Stat/ReadDir sizing and to know which clean members a read handle can stream.
	meta map[string]memberMeta
	// staged maps a store path to the host temp file holding its dirty bytes (a member
	// created or opened for write). The temp file is the authoritative content until
	// the next flush folds it into the archive.
	staged map[string]*stagedFile
	// dirs is the set of known directories (store paths); "" (root) is always present.
	dirs map[string]struct{}
	// tomb marks store paths removed/renamed-away since load, so a flush drops the
	// original member even though it is still in meta.
	tomb map[string]struct{}
	// dirty is set when a flush is needed (any staged file, tombstone, or new dir).
	dirty bool
}

// memberMeta is the lightweight central-directory record kept per clean member: just
// enough to Stat/size it and to re-open it for streaming (by name, in a fresh reader).
type memberMeta struct {
	size    int64
	modTime time.Time
}

// stagedFile is a dirty member's content staged in a host temp file.
type stagedFile struct {
	tmp     string // host temp-file path
	modTime time.Time
	refs    int // open write handles; the temp file is kept until flush regardless
}

func newZipFS(spec corefs.ShareSpec, b bus.Bus) (*zipFS, error) {
	if strings.TrimSpace(spec.Path) == "" {
		return nil, errors.New("zipfs: requires a path to a .zip archive")
	}
	abs, err := filepath.Abs(spec.Path)
	if err != nil {
		return nil, err
	}
	z := &zipFS{
		path:     abs,
		readOnly: spec.ReadOnly,
		bus:      b,
		meta:     make(map[string]memberMeta),
		staged:   make(map[string]*stagedFile),
		dirs:     map[string]struct{}{"": {}},
		tomb:     make(map[string]struct{}),
	}
	if err := z.scanArchive(); err != nil {
		return nil, err
	}
	return z, nil
}

// openReader opens the backing .zip and returns its reader plus the *os.File the
// caller must Close when done. Parsing a zip reader reads only the central directory
// (member headers), not member bodies. The caller owns the returned handle's lifetime
// — zipfs holds NO long-lived archive handle between calls.
func (z *zipFS) openReader() (*zip.Reader, *os.File, error) {
	f, err := os.Open(z.path)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close() // best-effort cleanup; returning the original error
		return nil, nil, err
	}
	r, err := zip.NewReader(f, info.Size())
	if err != nil {
		_ = f.Close() // best-effort cleanup; returning the original error
		return nil, nil, err
	}
	return r, f, nil
}

// scanArchive (re)builds the lightweight metadata overlay (meta + dirs) from the
// archive's central directory, then closes the handle. A missing file is allowed for a
// writable share (the archive is materialised on first flush); a read-only share
// requires it to exist.
func (z *zipFS) scanArchive() error {
	r, f, err := z.openReader()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !z.readOnly {
			return nil // fresh archive
		}
		return err
	}
	defer f.Close()
	for _, ze := range r.File {
		name := normalize(ze.Name)
		if name == "" {
			continue
		}
		if ze.FileInfo().IsDir() {
			z.addDirs(name)
			continue
		}
		z.meta[name] = memberMeta{size: int64(ze.UncompressedSize64), modTime: ze.Modified}
		z.addDirs(parentDir(name))
	}
	return nil
}

// findMember locates a clean member by store path in an already-open reader, or nil.
func findMember(r *zip.Reader, name string) *zip.File {
	for _, ze := range r.File {
		if normalize(ze.Name) == name && !ze.FileInfo().IsDir() {
			return ze
		}
	}
	return nil
}

// fileExists reports whether a store path resolves to a live file (staged or clean,
// and not tombstoned). Caller holds at least z.mu.RLock.
func (z *zipFS) fileExists(name string) bool {
	if _, ok := z.staged[name]; ok {
		return true
	}
	if _, tombed := z.tomb[name]; tombed {
		return false
	}
	_, ok := z.meta[name]
	return ok
}

// addDirs records dir and every ancestor as a known directory.
func (z *zipFS) addDirs(dir string) {
	for dir != "" {
		z.dirs[dir] = struct{}{}
		dir = parentDir(dir)
	}
	z.dirs[""] = struct{}{}
}

// normalize cleans a path to the store convention: '/'-separated, no leading or
// trailing slash, "." → "". Map lookups are stable afterwards.
func normalize(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = path.Clean("/" + p)
	return strings.Trim(p, "/")
}

func parentDir(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return ""
}

func baseName(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// fileSize returns a live member's uncompressed size (staged temp file or original
// member metadata). Caller holds z.mu.
func (z *zipFS) fileSize(name string) (int64, time.Time, bool) {
	if s, ok := z.staged[name]; ok {
		if fi, err := os.Stat(s.tmp); err == nil {
			return fi.Size(), s.modTime, true
		}
		return 0, s.modTime, true
	}
	if _, tombed := z.tomb[name]; tombed {
		return 0, time.Time{}, false
	}
	if m, ok := z.meta[name]; ok {
		return m.size, m.modTime, true
	}
	return 0, time.Time{}, false
}

// stageNew creates an empty temp file for a new/truncated member and records it.
// Caller holds z.mu and has checked !readOnly.
func (z *zipFS) stageNew(name string) (*stagedFile, error) {
	tf, err := os.CreateTemp(filepath.Dir(z.path), ".zipfs-stage-*")
	if err != nil {
		return nil, err
	}
	tmp := tf.Name()
	_ = tf.Close() // handle is unused after Name(); bytes land via later WriteAt
	s := &stagedFile{tmp: tmp, modTime: time.Now()}
	z.staged[name] = s
	delete(z.tomb, name)
	z.addDirs(parentDir(name))
	z.dirty = true
	return s, nil
}

// stageExisting copies a clean member's inflated bytes into a fresh temp file so the
// member can be opened for random-access write. It opens its own short-lived reader
// (zipfs keeps no archive handle). Caller holds z.mu, !readOnly.
func (z *zipFS) stageExisting(name string) (*stagedFile, error) {
	m, ok := z.meta[name]
	if !ok {
		return z.stageNew(name)
	}
	r, af, err := z.openReader()
	if err != nil {
		return nil, err
	}
	defer af.Close() // #nosec G104 -- deferred best-effort close of a read-only archive handle
	ze := findMember(r, name)
	if ze == nil {
		return z.stageNew(name)
	}
	rc, err := ze.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close() // #nosec G104 -- deferred best-effort close of a read-only member reader
	tf, err := os.CreateTemp(filepath.Dir(z.path), ".zipfs-stage-*")
	if err != nil {
		return nil, err
	}
	tmp := tf.Name()
	// Bound the inflate against a decompression bomb: a member may not expand
	// past the uncompressed size it declares in the central directory. We copy
	// through a limit of declared+1 and treat any overflow as a corrupt/hostile
	// archive rather than letting it fill the disk.
	limit := int64(ze.UncompressedSize64)
	n, err := io.Copy(tf, io.LimitReader(rc, limit+1)) // #nosec G110 -- bounded by LimitReader
	if err != nil {
		_ = tf.Close() // best-effort cleanup; returning the copy error
		_ = os.Remove(tmp)
		return nil, err
	}
	if n > limit {
		_ = tf.Close() // best-effort cleanup; returning the bomb error
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("zipfs: member %q exceeds its declared uncompressed size (%d bytes)", name, limit)
	}
	_ = tf.Close() // staged bytes are re-opened via WriteAt; close error is not actionable here
	s := &stagedFile{tmp: tmp, modTime: m.modTime}
	z.staged[name] = s
	z.dirty = true
	return s, nil
}

// publish emits an FS-mutation event onto the §10d bus, if one is wired. The Origin is
// left blank for the service-supplied OriginBus wrapper to stamp, mirroring local_fs.
func (z *zipFS) publish(op corefs.Op, p, old string) {
	if z.bus == nil {
		return
	}
	z.bus.Publish(corefs.Event{Op: op, HostPath: p, OldPath: old, Time: time.Now()})
}

// flushLocked rewrites the backing .zip by streaming members from the old archive
// into a new one — unchanged members copied raw (no re-deflate), dirty/new members
// deflated from their temp files, tombstoned members dropped — then atomic rename.
// Peak memory is one I/O buffer; transient disk is ≈ archive size. Caller holds z.mu.
// A no-op for a read-only share or when nothing is dirty.
func (z *zipFS) flushLocked() error {
	if z.readOnly || !z.dirty {
		return nil
	}

	// Open a fresh reader on the current archive for the raw-copy pass (nil if the
	// archive does not exist yet — a brand-new writable volume).
	var srcReader *zip.Reader
	var srcFile *os.File
	if r, af, err := z.openReader(); err == nil {
		srcReader, srcFile = r, af
		defer srcFile.Close()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	tmpArchive := z.path + ".tmp"
	// tmpArchive derives from z.path, the operator-configured archive location
	// (share spec), not attacker-controlled input.
	out, err := os.Create(tmpArchive) // #nosec G304 -- operator-configured archive path
	if err != nil {
		return err
	}
	w := zip.NewWriter(out)
	cleanup := func(e error) error {
		_ = w.Close()
		_ = out.Close()
		_ = os.Remove(tmpArchive)
		return e
	}

	// 1. Copy every unchanged original member raw (still compressed — no inflate).
	if srcReader != nil {
		for _, ze := range srcReader.File {
			name := normalize(ze.Name)
			if name == "" || ze.FileInfo().IsDir() {
				continue
			}
			if _, staged := z.staged[name]; staged {
				continue // superseded by a staged version, written below
			}
			if _, tombed := z.tomb[name]; tombed {
				continue // removed/renamed away
			}
			if err := w.Copy(ze); err != nil {
				return cleanup(err)
			}
		}
	}

	// 2. Write the staged (new/modified) members, deflating once from their temp file.
	names := make([]string, 0, len(z.staged))
	for n := range z.staged {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if err := z.writeStagedMember(w, n); err != nil {
			return cleanup(err)
		}
	}

	// 3. Emit explicit directory markers so an empty directory survives a round-trip.
	dirs := make([]string, 0, len(z.dirs))
	for d := range z.dirs {
		if d != "" {
			dirs = append(dirs, d)
		}
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		if _, err := w.Create(d + "/"); err != nil {
			return cleanup(err)
		}
	}

	if err := w.Close(); err != nil {
		_ = out.Close() // best-effort cleanup; returning the writer error
		_ = os.Remove(tmpArchive)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmpArchive) // best-effort cleanup; returning the close error
		return err
	}

	// Release the fresh source reader before replacing the file (Windows can't rename
	// over an open handle), then atomically swap in the new archive.
	if srcFile != nil {
		_ = srcFile.Close() // best-effort; we only need the handle released before rename
		srcFile = nil       // defeat the deferred Close above (already closed)
		srcReader = nil
	}
	if err := os.Rename(tmpArchive, z.path); err != nil {
		_ = os.Remove(tmpArchive) // best-effort cleanup; returning the rename error
		return err
	}

	// Fold staged files into the archive: drop the temp files and clear the overlay,
	// then re-scan the new central directory so subsequent reads stream from it.
	for _, s := range z.staged {
		_ = os.Remove(s.tmp) // best-effort temp cleanup after a successful flush
	}
	z.staged = make(map[string]*stagedFile)
	z.tomb = make(map[string]struct{})
	z.meta = make(map[string]memberMeta)
	z.dirty = false
	return z.scanArchive()
}

// writeStagedMember deflates one staged temp file into the archive writer.
func (z *zipFS) writeStagedMember(w *zip.Writer, name string) error {
	s := z.staged[name]
	hdr := &zip.FileHeader{Name: name, Method: zip.Deflate}
	if !s.modTime.IsZero() {
		hdr.Modified = s.modTime
	}
	fw, err := w.CreateHeader(hdr)
	if err != nil {
		return err
	}
	in, err := os.Open(s.tmp)
	if err != nil {
		return err
	}
	defer in.Close()
	_, err = io.Copy(fw, in)
	return err
}

// ── FileSystem ─────────────────────────────────────────────────────────────────

func (z *zipFS) ReadDir(p string) ([]iofs.DirEntry, error) {
	dir := normalize(p)
	z.mu.RLock()
	defer z.mu.RUnlock()
	if _, ok := z.dirs[dir]; !ok {
		return nil, iofs.ErrNotExist
	}
	prefix := dir
	if prefix != "" {
		prefix += "/"
	}
	seen := map[string]struct{}{}
	out := make([]iofs.DirEntry, 0)
	for d := range z.dirs {
		if d == dir || !strings.HasPrefix(d, prefix) {
			continue
		}
		child := strings.TrimPrefix(d, prefix)
		if i := strings.IndexByte(child, '/'); i >= 0 {
			child = child[:i]
		}
		if _, ok := seen[child]; ok {
			continue
		}
		seen[child] = struct{}{}
		out = append(out, zipDirEntry{name: child, dir: true})
	}
	// Live files = (original ∖ tombstoned) ∪ staged.
	emit := func(name string) {
		if !strings.HasPrefix(name, prefix) {
			return
		}
		child := strings.TrimPrefix(name, prefix)
		if strings.IndexByte(child, '/') >= 0 {
			return // nested deeper; surfaced as a directory above
		}
		if _, ok := seen[child]; ok {
			return
		}
		seen[child] = struct{}{}
		size, mt, _ := z.fileSize(name)
		out = append(out, zipDirEntry{name: child, size: size, modTime: mt})
	}
	for name := range z.meta {
		if _, tombed := z.tomb[name]; tombed {
			continue
		}
		if _, staged := z.staged[name]; staged {
			continue // emitted from staged set below to avoid a dup
		}
		emit(name)
	}
	for name := range z.staged {
		emit(name)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, nil
}

func (z *zipFS) Stat(p string) (iofs.FileInfo, error) {
	name := normalize(p)
	z.mu.RLock()
	defer z.mu.RUnlock()
	if _, ok := z.dirs[name]; ok {
		return zipFileInfo{name: baseName(name), dir: true}, nil
	}
	if size, mt, ok := z.fileSize(name); ok {
		return zipFileInfo{name: baseName(name), size: size, modTime: mt}, nil
	}
	return nil, iofs.ErrNotExist
}

// DiskUsage reports a synthetic capacity for the virtual volume: the uncompressed
// bytes currently stored as "used" against a nominal 2 GiB total — there is no block
// device to query. Reported uncapped (like every backend); the per-protocol caps at
// the AFP/SMB/NCP consumers saturate it.
func (z *zipFS) DiskUsage(_ string) (total, free uint64, err error) {
	z.mu.RLock()
	defer z.mu.RUnlock()
	var used uint64
	for name, m := range z.meta {
		if _, tombed := z.tomb[name]; tombed {
			continue
		}
		if _, staged := z.staged[name]; staged {
			continue
		}
		used += uint64(m.size)
	}
	for name := range z.staged {
		if sz, _, ok := z.fileSize(name); ok {
			used += uint64(sz)
		}
	}
	const nominalTotal uint64 = 2 << 30 // 2 GiB synthetic capacity
	total = nominalTotal
	if used >= total {
		return total, 0, nil
	}
	return total, total - used, nil
}

func (z *zipFS) CreateDir(p string) error {
	name := normalize(p)
	if name == "" {
		return iofs.ErrInvalid
	}
	z.mu.Lock()
	defer z.mu.Unlock()
	if z.readOnly {
		return ErrReadOnly
	}
	if z.fileExists(name) {
		return iofs.ErrExist
	}
	if _, ok := z.dirs[name]; ok {
		return iofs.ErrExist
	}
	z.addDirs(name)
	z.dirty = true
	if err := z.flushLocked(); err != nil {
		return err
	}
	z.publish(corefs.OpCreate, name, "")
	return nil
}

func (z *zipFS) CreateFile(p string) (corefs.File, error) {
	name := normalize(p)
	if name == "" {
		return nil, iofs.ErrInvalid
	}
	z.mu.Lock()
	defer z.mu.Unlock()
	if z.readOnly {
		return nil, ErrReadOnly
	}
	s, err := z.stageNew(name)
	if err != nil {
		return nil, err
	}
	wf, err := z.openWriteHandle(name, s, os.O_RDWR)
	if err != nil {
		return nil, err
	}
	z.publish(corefs.OpCreate, name, "")
	return wf, nil
}

func (z *zipFS) OpenFile(p string, flag int) (corefs.File, error) {
	name := normalize(p)
	wantsWrite := flag&(os.O_WRONLY|os.O_RDWR|os.O_APPEND|os.O_TRUNC|os.O_CREATE) != 0
	z.mu.Lock()
	defer z.mu.Unlock()

	if !z.fileExists(name) {
		if flag&os.O_CREATE == 0 {
			return nil, iofs.ErrNotExist
		}
		if z.readOnly {
			return nil, ErrReadOnly
		}
		s, err := z.stageNew(name)
		if err != nil {
			return nil, err
		}
		return z.openWriteHandle(name, s, flag)
	}

	if !wantsWrite {
		// Pure read: stream from the staged temp file if dirty, else from the archive.
		return z.openReadHandle(name)
	}

	if z.readOnly {
		return nil, ErrReadOnly
	}
	// Write open of an existing member: stage it (copy-out) if not already staged.
	s, ok := z.staged[name]
	if !ok {
		var err error
		if s, err = z.stageExisting(name); err != nil {
			return nil, err
		}
	}
	if flag&os.O_TRUNC != 0 {
		if err := os.Truncate(s.tmp, 0); err != nil {
			return nil, err
		}
		s.modTime = time.Now()
	}
	return z.openWriteHandle(name, s, flag)
}

func (z *zipFS) Remove(p string) error {
	name := normalize(p)
	z.mu.Lock()
	defer z.mu.Unlock()
	if z.readOnly {
		return ErrReadOnly
	}
	_, isDir := z.dirs[name]
	if !z.fileExists(name) && !isDir {
		return iofs.ErrNotExist
	}
	z.tombstoneLocked(name)
	delete(z.dirs, name)
	z.dirty = true
	if err := z.flushLocked(); err != nil {
		return err
	}
	z.publish(corefs.OpDelete, name, "")
	return nil
}

func (z *zipFS) Rename(oldp, newp string) error {
	o, n := normalize(oldp), normalize(newp)
	z.mu.Lock()
	defer z.mu.Unlock()
	if z.readOnly {
		return ErrReadOnly
	}
	if z.fileExists(o) {
		if err := z.renameFileLocked(o, n); err != nil {
			return err
		}
		z.dirty = true
		if err := z.flushLocked(); err != nil {
			return err
		}
		z.publish(corefs.OpRename, n, o)
		return nil
	}
	if _, ok := z.dirs[o]; ok {
		if err := z.renameSubtreeLocked(o, n); err != nil {
			return err
		}
		z.dirty = true
		if err := z.flushLocked(); err != nil {
			return err
		}
		z.publish(corefs.OpRename, n, o)
		return nil
	}
	return iofs.ErrNotExist
}

// tombstoneLocked drops a live member: remove a staged temp file if present, and
// tombstone the original so the next flush omits it. Caller holds z.mu.
func (z *zipFS) tombstoneLocked(name string) {
	if s, ok := z.staged[name]; ok {
		_ = os.Remove(s.tmp) // best-effort temp cleanup; tombstone below is authoritative
		delete(z.staged, name)
	}
	if _, ok := z.meta[name]; ok {
		z.tomb[name] = struct{}{}
	}
}

// renameFileLocked re-keys a single live file from o to n by staging o's bytes under
// n and tombstoning o. Caller holds z.mu, !readOnly.
func (z *zipFS) renameFileLocked(o, n string) error {
	if s, ok := z.staged[o]; ok {
		// Move the temp file's ownership to the new key.
		z.staged[n] = s
		delete(z.staged, o)
		delete(z.tomb, n)
	} else {
		if _, err := z.stageExisting(o); err != nil {
			return err
		}
		s := z.staged[o]
		z.staged[n] = s
		delete(z.staged, o)
		delete(z.tomb, n)
	}
	if _, ok := z.meta[o]; ok {
		z.tomb[o] = struct{}{}
	}
	z.addDirs(parentDir(n))
	return nil
}

// renameSubtreeLocked re-keys directory o (and every live descendant) to n. Caller
// holds z.mu, !readOnly.
func (z *zipFS) renameSubtreeLocked(o, n string) error {
	oldPrefix := o + "/"
	// Collect descendant files first (mutating the maps while ranging is unsafe).
	var files []string
	for name := range z.staged {
		if strings.HasPrefix(name, oldPrefix) {
			files = append(files, name)
		}
	}
	for name := range z.meta {
		if _, tombed := z.tomb[name]; tombed {
			continue
		}
		if strings.HasPrefix(name, oldPrefix) {
			if _, staged := z.staged[name]; !staged {
				files = append(files, name)
			}
		}
	}
	for _, name := range files {
		dst := n + "/" + strings.TrimPrefix(name, oldPrefix)
		if err := z.renameFileLocked(name, dst); err != nil {
			return err
		}
	}
	// Re-key directory markers.
	var subdirs []string
	for d := range z.dirs {
		if d == o || strings.HasPrefix(d, oldPrefix) {
			subdirs = append(subdirs, d)
		}
	}
	for _, d := range subdirs {
		delete(z.dirs, d)
		if d == o {
			z.addDirs(n)
		} else {
			z.addDirs(n + "/" + strings.TrimPrefix(d, oldPrefix))
		}
	}
	return nil
}

// ShortName/MediumName are passthroughs: the assembled shareFS overrides them with the
// configured NameEngine (BuildShare), so the backend's own derivation is unused —
// mirror local_fs/memfs and return the path unchanged.
func (z *zipFS) ShortName(p string) (string, error)  { return p, nil }
func (z *zipFS) MediumName(p string) (string, error) { return p, nil }

func (z *zipFS) Capabilities() corefs.Capabilities {
	return corefs.Capabilities{ChildCount: true, CatSearch: true, ReadOnly: z.readOnly}
}

// CatSearch satisfies the optional CatSearcher capability with the shared predicate
// tree-walk over this backend's own ReadDir — zipfs is a plain hierarchical store.
func (z *zipFS) CatSearch(crit corefs.CatSearchCriteria, cursor corefs.CatSearchCursor) ([]corefs.CatSearchResult, corefs.CatSearchCursor, error) {
	return corefs.WalkCatSearch(z, crit, cursor)
}

// Close (the optional fs.FSCloser teardown the file services call at service Stop)
// flushes any pending mutation and discards staged temp files. zipfs holds no
// long-lived archive handle (each read handle owns its own short-lived reader), so
// there is nothing else to release. It is idempotent and the FS stays correct even if
// Close is never called — the flush already happens on every write handle's Close.
func (z *zipFS) Close() error {
	z.mu.Lock()
	defer z.mu.Unlock()
	err := z.flushLocked()
	for _, s := range z.staged {
		_ = os.Remove(s.tmp) // best-effort temp cleanup at Close
	}
	z.staged = make(map[string]*stagedFile)
	return err
}

// ── Handles ───────────────────────────────────────────────────────────────────

// openWriteHandle returns a write handle over a staged temp file. Caller holds z.mu.
func (z *zipFS) openWriteHandle(name string, s *stagedFile, flag int) (corefs.File, error) {
	fl := os.O_RDWR
	// 0600: this is a private host-side staging temp file, not user-visible
	// content — the member's mode inside the archive is set at flush time.
	tf, err := os.OpenFile(s.tmp, fl, 0o600)
	if err != nil {
		return nil, err
	}
	s.refs++
	return &zipWriteFile{fs: z, name: name, staged: s, tmp: tf}, nil
}

// openReadHandle returns a streaming read handle. A staged (dirty) member is read from
// its temp file (random-access); a clean member streams from a short-lived archive
// reader the handle OWNS for its lifetime (closed on handle Close) — zipfs keeps no
// archive handle of its own. Caller holds z.mu.
func (z *zipFS) openReadHandle(name string) (corefs.File, error) {
	if s, ok := z.staged[name]; ok {
		tf, err := os.Open(s.tmp)
		if err != nil {
			return nil, err
		}
		return &zipStagedReadFile{name: name, tmp: tf}, nil
	}
	m, ok := z.meta[name]
	if !ok {
		return nil, iofs.ErrNotExist
	}
	r, af, err := z.openReader()
	if err != nil {
		return nil, err
	}
	ze := findMember(r, name)
	if ze == nil {
		_ = af.Close() // best-effort cleanup; returning not-exist
		return nil, iofs.ErrNotExist
	}
	return &zipReadFile{archive: af, ze: ze, name: name, size: m.size, modTime: m.modTime}, nil
}

// zipWriteFile is a write handle backed by a host temp file (random-access). On
// Sync/Close it folds the staged member into the archive via the FS flush.
type zipWriteFile struct {
	fs     *zipFS
	name   string
	staged *stagedFile
	tmp    *os.File
	dirty  bool
}

func (f *zipWriteFile) ReadAt(p []byte, off int64) (int, error) { return f.tmp.ReadAt(p, off) }
func (f *zipWriteFile) WriteAt(p []byte, off int64) (int, error) {
	n, err := f.tmp.WriteAt(p, off)
	if n > 0 {
		f.dirty = true
		f.staged.modTime = time.Now()
		f.fs.mu.Lock()
		f.fs.dirty = true
		f.fs.mu.Unlock()
	}
	return n, err
}
func (f *zipWriteFile) Truncate(size int64) error {
	f.dirty = true
	f.staged.modTime = time.Now()
	f.fs.mu.Lock()
	f.fs.dirty = true
	f.fs.mu.Unlock()
	return f.tmp.Truncate(size)
}
func (f *zipWriteFile) Stat() (iofs.FileInfo, error) {
	fi, err := f.tmp.Stat()
	if err != nil {
		return nil, err
	}
	return zipFileInfo{name: baseName(f.name), size: fi.Size(), modTime: f.staged.modTime}, nil
}

// Sync flushes the temp file's data to disk and folds the archive so a crash after
// Sync keeps the write. The whole-archive repack is the cost of durability on a zip.
func (f *zipWriteFile) Sync() error {
	if err := f.tmp.Sync(); err != nil {
		return err
	}
	if !f.dirty {
		return nil
	}
	f.fs.mu.Lock()
	defer f.fs.mu.Unlock()
	return f.fs.flushLocked()
}

// Close closes the temp-file handle and folds the staged member into the archive when
// this handle wrote, emitting OpModify (matching local_fs write-then-close).
func (f *zipWriteFile) Close() error {
	cerr := f.tmp.Close()
	f.fs.mu.Lock()
	f.staged.refs--
	dirty := f.dirty
	var ferr error
	if dirty {
		ferr = f.fs.flushLocked()
	}
	f.fs.mu.Unlock()
	if dirty && ferr == nil {
		f.fs.publish(corefs.OpModify, f.name, "")
	}
	if ferr != nil {
		return ferr
	}
	return cerr
}

// zipStagedReadFile reads a dirty (staged) member from its host temp file.
type zipStagedReadFile struct {
	name string
	tmp  *os.File
}

func (f *zipStagedReadFile) ReadAt(p []byte, off int64) (int, error) { return f.tmp.ReadAt(p, off) }
func (f *zipStagedReadFile) WriteAt([]byte, int64) (int, error)      { return 0, iofs.ErrPermission }
func (f *zipStagedReadFile) Truncate(int64) error                    { return iofs.ErrPermission }
func (f *zipStagedReadFile) Sync() error                             { return nil }
func (f *zipStagedReadFile) Close() error                            { return f.tmp.Close() }
func (f *zipStagedReadFile) Stat() (iofs.FileInfo, error) {
	fi, err := f.tmp.Stat()
	if err != nil {
		return nil, err
	}
	return zipFileInfo{name: baseName(f.name), size: fi.Size()}, nil
}

// zipReadFile streams a clean member from the archive, inflating on demand. It keeps a
// forward inflate cursor so sequential ReadAt is O(n); a backward ReadAt reopens the
// member and re-inflates to the new offset. A STORED member would be served directly,
// but archive/zip's Reader inflates transparently either way — the cursor still gives
// sequential reads no full-member buffering. Not safe for concurrent ReadAt on one
// handle (the seam opens one handle per client fork; that matches local_fs's *os.File).
type zipReadFile struct {
	archive *os.File // the short-lived archive handle backing ze (this handle owns it)
	ze      *zip.File
	name    string
	size    int64
	modTime time.Time

	rc  io.ReadCloser // current inflate stream, positioned at `pos`
	pos int64         // uncompressed offset the stream is positioned at
}

func (f *zipReadFile) reopen() error {
	if f.rc != nil {
		_ = f.rc.Close() // best-effort; discarding the old member reader before reopening
		f.rc = nil
	}
	rc, err := f.ze.Open()
	if err != nil {
		return err
	}
	f.rc = rc
	f.pos = 0
	return nil
}

// discard advances the inflate cursor to off, re-opening if it must seek backward.
func (f *zipReadFile) seekTo(off int64) error {
	if f.rc == nil || off < f.pos {
		if err := f.reopen(); err != nil {
			return err
		}
	}
	for f.pos < off {
		skip := off - f.pos
		// Bound the discard buffer; CopyN over a capped reader keeps memory flat.
		n, err := io.CopyN(io.Discard, f.rc, skip)
		f.pos += n
		if err != nil {
			return err
		}
	}
	return nil
}

func (f *zipReadFile) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, iofs.ErrInvalid
	}
	if off >= f.size {
		return 0, io.EOF
	}
	if err := f.seekTo(off); err != nil {
		return 0, err
	}
	// io.ReadFull fills p (or stops at EOF); advance the cursor by what we read.
	n, err := io.ReadFull(f.rc, p)
	f.pos += int64(n)
	if err == io.ErrUnexpectedEOF || (err == nil && off+int64(n) >= f.size) {
		return n, io.EOF
	}
	if err == io.EOF {
		return n, io.EOF
	}
	return n, err
}

func (f *zipReadFile) WriteAt([]byte, int64) (int, error) { return 0, iofs.ErrPermission }
func (f *zipReadFile) Truncate(int64) error               { return iofs.ErrPermission }
func (f *zipReadFile) Sync() error                        { return nil }
func (f *zipReadFile) Close() error {
	var err error
	if f.rc != nil {
		err = f.rc.Close()
		f.rc = nil
	}
	if f.archive != nil {
		if cerr := f.archive.Close(); err == nil {
			err = cerr
		}
		f.archive = nil
	}
	return err
}
func (f *zipReadFile) Stat() (iofs.FileInfo, error) {
	return zipFileInfo{name: baseName(f.name), size: f.size, modTime: f.modTime}, nil
}

// ── FileInfo / DirEntry ──────────────────────────────────────────────────────────

type zipFileInfo struct {
	name    string
	size    int64
	dir     bool
	modTime time.Time
}

func (i zipFileInfo) Name() string { return i.name }
func (i zipFileInfo) Size() int64  { return i.size }
func (i zipFileInfo) Mode() iofs.FileMode {
	if i.dir {
		return iofs.ModeDir | 0o755
	}
	return 0o644
}
func (i zipFileInfo) ModTime() time.Time { return i.modTime }
func (i zipFileInfo) IsDir() bool        { return i.dir }
func (i zipFileInfo) Sys() any           { return nil }

type zipDirEntry struct {
	name    string
	dir     bool
	size    int64
	modTime time.Time
}

func (d zipDirEntry) Name() string { return d.name }
func (d zipDirEntry) IsDir() bool  { return d.dir }
func (d zipDirEntry) Type() iofs.FileMode {
	if d.dir {
		return iofs.ModeDir
	}
	return 0
}
func (d zipDirEntry) Info() (iofs.FileInfo, error) {
	return zipFileInfo{name: d.name, size: d.size, dir: d.dir, modTime: d.modTime}, nil
}
