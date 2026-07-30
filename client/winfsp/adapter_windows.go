//go:build windows

package winfsp

import (
	"errors"
	iofs "io/fs"
	"os"

	winfsp "github.com/winfsp/go-winfsp"
	"golang.org/x/sys/windows"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// WinFsp createOptions / cleanup flags we care about (from the WinFSP FSCTL headers;
// go-winfsp does not export named constants for these).
const (
	fileDirectoryFile = 0x00000001 // FILE_DIRECTORY_FILE
	fspCleanupDelete  = 0x01       // FspCleanupDelete
)

// writeAccessMask is the set of granted-access bits that mean the handle may write, so we
// must open the underlying fork read-write.
const writeAccessMask = windows.FILE_WRITE_DATA | windows.FILE_APPEND_DATA |
	windows.GENERIC_WRITE | windows.GENERIC_ALL

// Adapter wraps a core/fs.ForkFS and implements go-winfsp's Behaviour* delegates. One
// Adapter serves any protocol, because every client.Connect returns the same ForkFS shape.
type Adapter struct {
	fsys        fs.ForkFS
	readOnly    bool
	volLabel    string
	nativeForks bool // surface resource forks / Apple metadata as NTFS SFM streams
	handles     *handleTable
}

// newAdapter builds an Adapter over an already-connected ForkFS. The mount is read-only
// when the ForkFS itself is read-only OR the caller forced it via Options.ReadOnly.
func newAdapter(fsys fs.ForkFS, opts Options) *Adapter {
	label := opts.VolumeLabel
	if label == "" {
		label = "ClassicStack"
	}
	return &Adapter{
		fsys:        fsys,
		readOnly:    opts.ReadOnly || fsys.Capabilities().ReadOnly,
		volLabel:    label,
		nativeForks: opts.NativeForks,
		handles:     newHandleTable(),
	}
}

// flagFor maps WinFsp granted-access to an os.O_* flag for opening the data fork. A
// read-only volume never opens read-write.
func (a *Adapter) flagFor(grantedAccess uint32) int {
	if !a.readOnly && grantedAccess&writeAccessMask != 0 {
		return os.O_RDWR
	}
	return os.O_RDONLY
}

// openStore opens (or stats, for a dir) a store path and returns a handle.
func (a *Adapter) openStore(storePath string, flag int, info *winfsp.FSP_FSCTL_FILE_INFO) (uintptr, error) {
	fi, err := a.fsys.Stat(storePath)
	if err != nil {
		return 0, err
	}
	h := &openFile{path: storePath, isDir: fi.IsDir(), flag: flag}
	if !fi.IsDir() {
		f, err := a.fsys.OpenFile(storePath, flag)
		if err != nil {
			return 0, err
		}
		h.f = f
	}
	a.fillFileInfo(info, storePath, fi)
	return a.handles.add(h), nil
}

// --- BehaviourBase ---------------------------------------------------------------------

// Open opens an existing file or directory.
func (a *Adapter) Open(
	_ *winfsp.FileSystemRef, name string,
	createOptions, grantedAccess uint32,
	info *winfsp.FSP_FSCTL_FILE_INFO,
) (uintptr, error) {
	trace("Open name=%q createOptions=%#x grantedAccess=%#x", name, createOptions, grantedAccess)
	base, streamRaw := a.peelStream(name)
	storePath, err := toStorePath(base)
	if err != nil {
		trace("Open → err=%v", err)
		return 0, err
	}
	if streamRaw != "" {
		k, ok := lookupStream(streamRaw)
		if !ok {
			trace("Open stream=%q → err=%v", streamRaw, errNoSuchStream)
			return 0, errNoSuchStream
		}
		if k != streamData {
			ctx, err := a.openStream(storePath, k, a.flagFor(grantedAccess), info)
			if err != nil {
				trace("Open %q:%s → err=%v", storePath, k.streamName(), err)
			} else {
				trace("Open → ctx=%d path=%q stream=%s", ctx, storePath, k.streamName())
			}
			return ctx, err
		}
	}
	ctx, err := a.openStore(storePath, a.flagFor(grantedAccess), info)
	if err != nil {
		trace("Open → err=%v", err)
	} else {
		trace("Open → ctx=%d path=%q", ctx, storePath)
	}
	return ctx, err
}

// Close releases an open handle. Only a directory handle can own a WinFsp DirBuffer, so we
// only release one for a directory (calling DirBuffer.Delete reaches into the WinFsp DLL,
// which is present only under a real mount — a data file never allocates one).
func (a *Adapter) Close(_ *winfsp.FileSystemRef, file uintptr) {
	trace("Close ctx=%d", file)
	if h, ok := a.handles.remove(file); ok {
		if h.stream != streamData {
			// Persist a dirty record stream (resource-fork writes already went through
			// the fs.File) before dropping the handle.
			if err := a.flushStream(h); err != nil {
				trace("Close stream=%s flush err=%v", h.stream.streamName(), err)
			}
		}
		if h.f != nil {
			_ = h.f.Close()
		}
		if h.dirBufUsed {
			// Only release a buffer WinFsp actually took (DirBuffer.Delete reaches into the
			// WinFsp DLL, which is present only under a real mount).
			h.dirBuf.Delete()
		}
	}
}

// --- BehaviourGetVolumeInfo ------------------------------------------------------------

func (a *Adapter) GetVolumeInfo(_ *winfsp.FileSystemRef, info *winfsp.FSP_FSCTL_VOLUME_INFO) error {
	trace("GetVolumeInfo")
	total, free, err := a.fsys.DiskUsage("")
	if err != nil || total == 0 {
		// Fall back to a nominal size so the volume still mounts.
		total, free = 8<<40, 8<<40
	}
	info.TotalSize = total
	info.FreeSize = free
	label := []rune(a.volLabel)
	n := 0
	for _, r := range label {
		if n >= len(info.VolumeLabel) {
			break
		}
		info.VolumeLabel[n] = uint16(r)
		n++
	}
	info.VolumeLabelLength = uint16(2 * n)
	return nil
}

// --- BehaviourCreate -------------------------------------------------------------------

// NOTE: We intentionally do NOT implement BehaviourGetSecurityByName. WinFsp treats a
// NULL GetSecurityByName as "no access checks" and grants DesiredAccess (see
// FspAccessCheckEx / the WinFsp tutorial). That avoids a Stat per Windows existence
// probe — those dominate AFP traffic under csmount. Open/Create still reconcile
// reality on the wire. A stub that always succeeds breaks Create (name collision);
// always-not-found breaks Open. Omitting the op is the supported middle path.
//
// GetSecurity / SetSecurity (by open handle) remain: they return a static Everyone SD
// without touching the remote volume.

func (a *Adapter) Create(
	_ *winfsp.FileSystemRef, name string,
	createOptions, grantedAccess, _ uint32,
	_ *windows.SECURITY_DESCRIPTOR,
	_ uint64, info *winfsp.FSP_FSCTL_FILE_INFO,
) (uintptr, error) {
	trace("Create name=%q createOptions=%#x grantedAccess=%#x", name, createOptions, grantedAccess)
	if a.readOnly {
		trace("Create → err=%v", os.ErrPermission)
		return 0, os.ErrPermission
	}
	base, streamRaw := a.peelStream(name)
	storePath, err := toStorePath(base)
	if err != nil {
		trace("Create → err=%v", err)
		return 0, err
	}
	if streamRaw != "" {
		// Creating a named stream: the base file must already exist (Windows opens the
		// base before its stream). Route to the stream open — for the record streams this
		// starts an empty buffer, for the resource fork it opens the fork O_RDWR|O_CREATE.
		k, ok := lookupStream(streamRaw)
		if !ok || k == streamData {
			trace("Create stream=%q → err=%v", streamRaw, errNoSuchStream)
			return 0, errNoSuchStream
		}
		ctx, err := a.openStream(storePath, k, os.O_RDWR, info)
		if err != nil {
			trace("Create %q:%s → err=%v", storePath, k.streamName(), err)
		} else {
			trace("Create → ctx=%d path=%q stream=%s", ctx, storePath, k.streamName())
		}
		return ctx, err
	}
	if createOptions&fileDirectoryFile != 0 {
		if err := a.fsys.CreateDir(storePath); err != nil {
			trace("Create → err=%v", err)
			return 0, err
		}
		fi, err := a.fsys.Stat(storePath)
		if err != nil {
			trace("Create → err=%v", err)
			return 0, err
		}
		a.fillFileInfo(info, storePath, fi)
		ctx := a.handles.add(&openFile{path: storePath, isDir: true})
		trace("Create → ctx=%d dir", ctx)
		return ctx, nil
	}
	f, err := a.fsys.CreateFile(storePath)
	if err != nil {
		trace("Create → err=%v", err)
		return 0, err
	}
	h := &openFile{path: storePath, f: f, flag: os.O_RDWR}
	fi, err := a.fsys.Stat(storePath)
	if err != nil {
		_ = f.Close()
		trace("Create → err=%v", err)
		return 0, err
	}
	a.fillFileInfo(info, storePath, fi)
	ctx := a.handles.add(h)
	trace("Create → ctx=%d file", ctx)
	return ctx, nil
}

// --- BehaviourOverwrite ----------------------------------------------------------------

func (a *Adapter) Overwrite(
	_ *winfsp.FileSystemRef, file uintptr,
	_ uint32, _ bool, _ uint64, info *winfsp.FSP_FSCTL_FILE_INFO,
) error {
	h, ok := a.handles.get(file)
	if !ok {
		return os.ErrInvalid
	}
	if a.readOnly {
		return os.ErrPermission
	}
	// A truncating open of a stream (FILE_OVERWRITE/SUPERSEDE) empties that fork/record,
	// never the base file.
	if h.stream != streamData {
		if err := a.truncateStream(h, 0); err != nil {
			return err
		}
		return a.streamFileInfo(info, h)
	}
	if h.f == nil {
		return os.ErrInvalid
	}
	if err := h.f.Truncate(0); err != nil {
		return err
	}
	_, err := a.statFileInfo(info, h.path)
	return err
}

// --- BehaviourRead / BehaviourWrite ----------------------------------------------------

func (a *Adapter) Read(
	_ *winfsp.FileSystemRef, file uintptr, buf []byte, offset uint64,
) (int, error) {
	trace("Read ctx=%d offset=%d len=%d", file, offset, len(buf))
	h, ok := a.handles.get(file)
	if !ok {
		trace("Read → err=%v", os.ErrInvalid)
		return 0, os.ErrInvalid
	}
	if h.stream != streamData {
		n, err := a.readStream(h, buf, offset)
		trace("Read stream=%s → n=%d err=%v", h.stream.streamName(), n, err)
		return n, err
	}
	if h.f == nil {
		trace("Read → err=%v", os.ErrInvalid)
		return 0, os.ErrInvalid
	}
	n, err := h.f.ReadAt(buf, int64(offset))
	if errors.Is(err, iofs.ErrClosed) {
		trace("Read → err=%v", err)
		return n, err
	}
	if err != nil && n > 0 {
		// A short read that still returned bytes is success to WinFsp.
		trace("Read → n=%d (short)", n)
		return n, nil
	}
	if err != nil {
		trace("Read → n=%d err=%v", n, err)
	} else {
		trace("Read → n=%d", n)
	}
	return n, err
}

func (a *Adapter) Write(
	_ *winfsp.FileSystemRef, file uintptr,
	buf []byte, offset uint64,
	writeToEndOfFile, _ bool,
	info *winfsp.FSP_FSCTL_FILE_INFO,
) (int, error) {
	trace("Write ctx=%d offset=%d len=%d eof=%v", file, offset, len(buf), writeToEndOfFile)
	h, ok := a.handles.get(file)
	if !ok {
		trace("Write → err=%v", os.ErrInvalid)
		return 0, os.ErrInvalid
	}
	if h.stream != streamData {
		n, err := a.writeStream(h, buf, offset, writeToEndOfFile)
		if err == nil {
			_ = a.streamFileInfo(info, h)
		}
		trace("Write stream=%s → n=%d err=%v", h.stream.streamName(), n, err)
		return n, err
	}
	if h.f == nil {
		trace("Write → err=%v", os.ErrInvalid)
		return 0, os.ErrInvalid
	}
	if a.readOnly {
		trace("Write → err=%v", os.ErrPermission)
		return 0, os.ErrPermission
	}
	off := int64(offset)
	if writeToEndOfFile {
		if fi, err := h.f.Stat(); err == nil {
			off = fi.Size()
		}
	}
	n, err := h.f.WriteAt(buf, off)
	if err != nil {
		trace("Write → n=%d err=%v", n, err)
		return n, err
	}
	_, _ = a.statFileInfo(info, h.path)
	trace("Write → n=%d", n)
	return n, nil
}

// --- BehaviourFlush --------------------------------------------------------------------

func (a *Adapter) Flush(_ *winfsp.FileSystemRef, file uintptr, info *winfsp.FSP_FSCTL_FILE_INFO) error {
	h, ok := a.handles.get(file)
	if !ok {
		return nil // volume flush → no-op
	}
	if h.stream != streamData {
		if err := a.flushStream(h); err != nil {
			return err
		}
		return a.streamFileInfo(info, h)
	}
	if h.f == nil {
		return nil // volume flush → no-op
	}
	if err := h.f.Sync(); err != nil {
		return err
	}
	_, _ = a.statFileInfo(info, h.path)
	return nil
}

// --- BehaviourGetFileInfo --------------------------------------------------------------

func (a *Adapter) GetFileInfo(_ *winfsp.FileSystemRef, file uintptr, info *winfsp.FSP_FSCTL_FILE_INFO) error {
	trace("GetFileInfo ctx=%d", file)
	h, ok := a.handles.get(file)
	if !ok {
		trace("GetFileInfo → err=%v (no handle)", os.ErrInvalid)
		return os.ErrInvalid
	}
	if h.stream != streamData {
		err := a.streamFileInfo(info, h)
		trace("GetFileInfo path=%q stream=%s → err=%v", h.path, h.stream.streamName(), err)
		return err
	}
	_, err := a.statFileInfo(info, h.path)
	if err != nil {
		trace("GetFileInfo path=%q → err=%v", h.path, err)
	} else {
		trace("GetFileInfo path=%q → ok", h.path)
	}
	return err
}

// --- BehaviourSetBasicInfo -------------------------------------------------------------

func (a *Adapter) SetBasicInfo(
	_ *winfsp.FileSystemRef, file uintptr,
	flags winfsp.SetBasicInfoFlags, attributes uint32,
	creationTime, lastAccessTime, _, _ uint64,
	info *winfsp.FSP_FSCTL_FILE_INFO,
) error {
	h, ok := a.handles.get(file)
	if !ok {
		return os.ErrInvalid
	}
	if a.readOnly {
		return os.ErrPermission
	}
	attr, _ := a.fsys.Meta().Attrs(h.path)
	if flags&winfsp.SetBasicInfoAttributes != 0 && attributes != 0 {
		attr.Attrs = uint16(attributes) & storableAttrMask
	}
	if flags&winfsp.SetBasicInfoCreationTime != 0 && creationTime != 0 {
		attr.CreateTime = filetimeToTime(creationTime)
	}
	if flags&winfsp.SetBasicInfoLastAccessTime != 0 && lastAccessTime != 0 {
		attr.AccessTime = filetimeToTime(lastAccessTime)
	}
	if err := a.fsys.Meta().SetAttrs(h.path, attr); err != nil {
		trace("SetBasicInfo(path=%q): SetAttrs err=%v", h.path, err)
		return err
	}
	_, err := a.statFileInfo(info, h.path)
	if err != nil {
		trace("SetBasicInfo(path=%q) err=%v", h.path, err)
	}
	return err
}

// --- BehaviourSetFileSize --------------------------------------------------------------

func (a *Adapter) SetFileSize(
	_ *winfsp.FileSystemRef, file uintptr,
	newSize uint64, setAllocationSize bool,
	info *winfsp.FSP_FSCTL_FILE_INFO,
) error {
	h, ok := a.handles.get(file)
	if !ok {
		return os.ErrInvalid
	}
	if h.stream != streamData {
		if !setAllocationSize {
			if err := a.truncateStream(h, int64(newSize)); err != nil {
				return err
			}
		}
		return a.streamFileInfo(info, h)
	}
	if h.f == nil {
		return os.ErrInvalid
	}
	if a.readOnly {
		return os.ErrPermission
	}
	if !setAllocationSize {
		// A pure allocation-size hint only shrinks if below the current size; ignore it.
		if err := h.f.Truncate(int64(newSize)); err != nil {
			return err
		}
	}
	_, err := a.statFileInfo(info, h.path)
	return err
}

// --- BehaviourCanDelete ----------------------------------------------------------------

func (a *Adapter) CanDelete(_ *winfsp.FileSystemRef, file uintptr, _ string) error {
	h, ok := a.handles.get(file)
	if !ok {
		return os.ErrInvalid
	}
	if a.readOnly {
		return os.ErrPermission
	}
	if h.isDir {
		entries, err := a.fsys.ReadDir(h.path)
		if err != nil {
			return err
		}
		if len(entries) > 0 {
			return errDirNotEmpty
		}
	}
	return nil
}

// --- BehaviourCleanup ------------------------------------------------------------------

func (a *Adapter) Cleanup(_ *winfsp.FileSystemRef, file uintptr, _ string, cleanupFlags uint32) {
	if cleanupFlags&fspCleanupDelete == 0 || a.readOnly {
		return
	}
	h, ok := a.handles.get(file)
	if !ok {
		return
	}
	if h.stream != streamData {
		// Deleting a stream clears that fork/record only — never the base file. Truncating
		// the fork/record to zero and flushing removes its content; the ForkEngine drops an
		// empty resource fork / Finder info / comment.
		if err := a.truncateStream(h, 0); err == nil {
			_ = a.flushStream(h)
		}
		if h.f != nil {
			_ = h.f.Close()
			h.f = nil
		}
		return
	}
	// Close the data handle before removing so the backend can unlink cleanly.
	if h.f != nil {
		_ = h.f.Close()
		h.f = nil
	}
	_ = a.fsys.Remove(h.path)
}

// --- BehaviourRename -------------------------------------------------------------------

func (a *Adapter) Rename(_ *winfsp.FileSystemRef, file uintptr, source, target string, _ bool) error {
	if a.readOnly {
		return os.ErrPermission
	}
	src, err := toStorePath(source)
	if err != nil {
		return err
	}
	dst, err := toStorePath(target)
	if err != nil {
		return err
	}
	h, ok := a.handles.get(file)
	// WinFsp upper-cases the source name it passes here (it derives it from the normalized
	// FileName, not the case-preserved path the Open delegate saw), so `source` may not match
	// the on-disk name for a case-sensitive backend — a real AFP server then returns
	// kFPObjectNotFound and the rename appears to fail with STATUS_INTERNAL_ERROR. The open
	// handle carries the authoritative, correctly-cased source path, so prefer it.
	if ok && h.path != "" {
		src = h.path
	}
	// A classic SMB SMB_COM_RENAME (and other legacy backends) fails with a sharing/access
	// error while the source still has an open handle (observed: Win98 returns "access
	// denied"), so close the data fork BEFORE renaming. WinFsp keeps the file context live
	// across the rename and, on success, re-issues handle delegates (GetFileInfo, etc.)
	// against it, so afterwards we reopen on the target and repoint the handle — a valid,
	// new-path handle. (This mirrors go-winfsp's gofs reference of close → rename → reopen;
	// we skip its Seek offset-restore because core/fs.File is positional (ReadAt/WriteAt), so
	// there is no cursor to preserve.)
	if ok && h.f != nil {
		_ = h.f.Close()
		h.f = nil
	}
	if err := a.fsys.Rename(src, dst); err != nil {
		trace("Rename(%q -> %q): fsys.Rename err=%v", src, dst, err)
		// Rename failed; try to restore the handle on the (unchanged) source so subsequent
		// ops on it still work.
		if ok && !h.isDir {
			if f, rerr := a.fsys.OpenFile(src, h.flag); rerr == nil {
				h.f = f
			}
		}
		return err
	}
	if ok {
		h.path = dst
		if !h.isDir {
			// Reopen the data fork on the new path so WinFsp's post-rename handle use finds a
			// live file. A reopen failure is not fatal to the rename itself (the move landed);
			// leave f nil and let a later op surface it.
			if f, rerr := a.fsys.OpenFile(dst, h.flag); rerr == nil {
				h.f = f
			} else {
				trace("Rename(%q -> %q): reopen err=%v", src, dst, rerr)
			}
		}
	}
	trace("Rename(%q -> %q): ok", src, dst)
	return nil
}

// --- BehaviourGetSecurity / SetSecurity ------------------------------------------------

func (a *Adapter) GetSecurity(_ *winfsp.FileSystemRef, _ uintptr) (*windows.SECURITY_DESCRIPTOR, error) {
	return staticSD, nil
}

func (a *Adapter) SetSecurity(
	_ *winfsp.FileSystemRef, _ uintptr, _ windows.SECURITY_INFORMATION, _ *windows.SECURITY_DESCRIPTOR,
) error {
	// Legacy filesystems have no NT ACLs; accept and no-op so Explorer copies don't fail.
	return nil
}

// --- BehaviourReadDirectory ------------------------------------------------------------

func (a *Adapter) GetOrNewDirBuffer(_ *winfsp.FileSystemRef, file uintptr) (*winfsp.DirBuffer, error) {
	h, ok := a.handles.get(file)
	if !ok {
		return nil, os.ErrInvalid
	}
	h.dirBufUsed = true
	return &h.dirBuf, nil
}

func (a *Adapter) ReadDirectory(
	_ *winfsp.FileSystemRef, file uintptr, _ string,
	fill func(string, *winfsp.FSP_FSCTL_FILE_INFO) (bool, error),
) error {
	h, ok := a.handles.get(file)
	if !ok {
		trace("ReadDirectory ctx=%d → err=%v", file, os.ErrInvalid)
		return os.ErrInvalid
	}
	trace("ReadDirectory ctx=%d path=%q", file, h.path)
	entries, err := a.fsys.ReadDir(h.path)
	if err != nil {
		trace("ReadDirectory → err=%v", err)
		return err
	}
	// WinFsp expects "." and ".." for non-root directories.
	if h.path != "" {
		var self winfsp.FSP_FSCTL_FILE_INFO
		if _, err := a.statFileInfo(&self, h.path); err == nil {
			if ok, err := fill(".", &self); err != nil || !ok {
				return err
			}
			if ok, err := fill("..", &self); err != nil || !ok {
				return err
			}
		}
	}
	n := 0
	for _, de := range entries {
		var info winfsp.FSP_FSCTL_FILE_INFO
		if err := a.dirEntryInfo(&info, h.path, de); err != nil {
			continue // skip an entry we cannot stat rather than fail the whole listing
		}
		ok, err := fill(de.Name(), &info)
		if err != nil || !ok {
			trace("ReadDirectory → filled=%d stop err=%v", n, err)
			return err
		}
		n++
	}
	trace("ReadDirectory → entries=%d", n)
	return nil
}

// --- BehaviourGetDirInfoByName ---------------------------------------------------------

func (a *Adapter) GetDirInfoByName(
	_ *winfsp.FileSystemRef, parentDirFile uintptr, name string, dirInfo *winfsp.FSP_FSCTL_DIR_INFO,
) error {
	h, ok := a.handles.get(parentDirFile)
	if !ok {
		trace("GetDirInfoByName → err=%v", os.ErrInvalid)
		return os.ErrInvalid
	}
	trace("GetDirInfoByName parent=%q name=%q", h.path, name)
	child := joinStore(h.path, name)
	fi, err := a.fsys.Stat(child)
	if err != nil {
		trace("GetDirInfoByName → err=%v", err)
		return err
	}
	a.fillFileInfo(&dirInfo.FileInfo, child, fi)
	return nil
}

// mountOptions returns the go-winfsp Options for this adapter, passed to winfsp.Mount.
func (a *Adapter) mountOptions(opts Options) []winfsp.Option {
	ms := opts.FileInfoTimeoutMs
	if !opts.FileInfoTimeoutSet {
		ms = DefaultFileInfoTimeoutMs
	}
	var timeout uint32
	if ms < 0 {
		timeout = ^uint32(0) // WinFsp "infinite" + Cache Manager
	} else {
		timeout = uint32(ms)
	}
	return []winfsp.Option{
		winfsp.CaseSensitive(false),
		winfsp.FileSystemName("ClassicStack"),
		winfsp.FileInfoTimeout(timeout),
	}
}

// Compile-time assertions that the Adapter satisfies every delegate it means to.
// BehaviourGetSecurityByName is deliberately omitted — see the note above Create.
var (
	_ winfsp.BehaviourBase             = (*Adapter)(nil)
	_ winfsp.BehaviourGetVolumeInfo    = (*Adapter)(nil)
	_ winfsp.BehaviourCreate           = (*Adapter)(nil)
	_ winfsp.BehaviourOverwrite        = (*Adapter)(nil)
	_ winfsp.BehaviourRead             = (*Adapter)(nil)
	_ winfsp.BehaviourWrite            = (*Adapter)(nil)
	_ winfsp.BehaviourFlush            = (*Adapter)(nil)
	_ winfsp.BehaviourGetFileInfo      = (*Adapter)(nil)
	_ winfsp.BehaviourSetBasicInfo     = (*Adapter)(nil)
	_ winfsp.BehaviourSetFileSize      = (*Adapter)(nil)
	_ winfsp.BehaviourCanDelete        = (*Adapter)(nil)
	_ winfsp.BehaviourCleanup          = (*Adapter)(nil)
	_ winfsp.BehaviourRename           = (*Adapter)(nil)
	_ winfsp.BehaviourGetSecurity      = (*Adapter)(nil)
	_ winfsp.BehaviourSetSecurity      = (*Adapter)(nil)
	_ winfsp.BehaviourReadDirectory    = (*Adapter)(nil)
	_ winfsp.BehaviourGetDirInfoByName = (*Adapter)(nil)

	// The stream-aware wrapper adds NTFS named-stream enumeration; it is the object
	// mounted when native forks are enabled (see Adapter.mountable). The bare *Adapter
	// must NOT satisfy BehaviourGetStreamInfo, or streams-off mounts would advertise
	// streams — that is enforced by keeping GetStreamInfo on streamAdapter only.
	_ winfsp.BehaviourGetStreamInfo = streamAdapter{}
)
