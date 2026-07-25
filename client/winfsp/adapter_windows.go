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
	fsys     fs.ForkFS
	readOnly bool
	volLabel string
	handles  *handleTable
}

// newAdapter builds an Adapter over an already-connected ForkFS. The mount is read-only
// when the ForkFS itself is read-only OR the caller forced it via Options.ReadOnly.
func newAdapter(fsys fs.ForkFS, opts Options) *Adapter {
	label := opts.VolumeLabel
	if label == "" {
		label = "ClassicStack"
	}
	return &Adapter{
		fsys:     fsys,
		readOnly: opts.ReadOnly || fsys.Capabilities().ReadOnly,
		volLabel: label,
		handles:  newHandleTable(),
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
	h := &openFile{path: storePath, isDir: fi.IsDir()}
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
	storePath, err := toStorePath(name)
	if err != nil {
		return 0, err
	}
	return a.openStore(storePath, a.flagFor(grantedAccess), info)
}

// Close releases an open handle. Only a directory handle can own a WinFsp DirBuffer, so we
// only release one for a directory (calling DirBuffer.Delete reaches into the WinFsp DLL,
// which is present only under a real mount — a data file never allocates one).
func (a *Adapter) Close(_ *winfsp.FileSystemRef, file uintptr) {
	if h, ok := a.handles.remove(file); ok {
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

// --- BehaviourGetSecurityByName --------------------------------------------------------

func (a *Adapter) GetSecurityByName(
	_ *winfsp.FileSystemRef, name string, _ winfsp.GetSecurityByNameFlags,
) (uint32, *windows.SECURITY_DESCRIPTOR, error) {
	storePath, err := toStorePath(name)
	if err != nil {
		return 0, nil, err
	}
	fi, err := a.fsys.Stat(storePath)
	if err != nil {
		return 0, nil, err
	}
	var attrs uint32 = windows.FILE_ATTRIBUTE_NORMAL
	if fi.IsDir() {
		attrs = windows.FILE_ATTRIBUTE_DIRECTORY
	}
	if a.readOnly {
		attrs |= windows.FILE_ATTRIBUTE_READONLY
	}
	return attrs, staticSD, nil
}

// --- BehaviourCreate -------------------------------------------------------------------

func (a *Adapter) Create(
	_ *winfsp.FileSystemRef, name string,
	createOptions, grantedAccess, _ uint32,
	_ *windows.SECURITY_DESCRIPTOR,
	_ uint64, info *winfsp.FSP_FSCTL_FILE_INFO,
) (uintptr, error) {
	if a.readOnly {
		return 0, os.ErrPermission
	}
	storePath, err := toStorePath(name)
	if err != nil {
		return 0, err
	}
	if createOptions&fileDirectoryFile != 0 {
		if err := a.fsys.CreateDir(storePath); err != nil {
			return 0, err
		}
		fi, err := a.fsys.Stat(storePath)
		if err != nil {
			return 0, err
		}
		a.fillFileInfo(info, storePath, fi)
		return a.handles.add(&openFile{path: storePath, isDir: true}), nil
	}
	f, err := a.fsys.CreateFile(storePath)
	if err != nil {
		return 0, err
	}
	h := &openFile{path: storePath, f: f}
	fi, err := a.fsys.Stat(storePath)
	if err != nil {
		_ = f.Close()
		return 0, err
	}
	a.fillFileInfo(info, storePath, fi)
	return a.handles.add(h), nil
}

// --- BehaviourOverwrite ----------------------------------------------------------------

func (a *Adapter) Overwrite(
	_ *winfsp.FileSystemRef, file uintptr,
	_ uint32, _ bool, _ uint64, info *winfsp.FSP_FSCTL_FILE_INFO,
) error {
	h, ok := a.handles.get(file)
	if !ok || h.f == nil {
		return os.ErrInvalid
	}
	if a.readOnly {
		return os.ErrPermission
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
	h, ok := a.handles.get(file)
	if !ok || h.f == nil {
		return 0, os.ErrInvalid
	}
	n, err := h.f.ReadAt(buf, int64(offset))
	if errors.Is(err, iofs.ErrClosed) {
		return n, err
	}
	if err != nil && n > 0 {
		// A short read that still returned bytes is success to WinFsp.
		return n, nil
	}
	return n, err
}

func (a *Adapter) Write(
	_ *winfsp.FileSystemRef, file uintptr,
	buf []byte, offset uint64,
	writeToEndOfFile, _ bool,
	info *winfsp.FSP_FSCTL_FILE_INFO,
) (int, error) {
	h, ok := a.handles.get(file)
	if !ok || h.f == nil {
		return 0, os.ErrInvalid
	}
	if a.readOnly {
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
		return n, err
	}
	_, _ = a.statFileInfo(info, h.path)
	return n, nil
}

// --- BehaviourFlush --------------------------------------------------------------------

func (a *Adapter) Flush(_ *winfsp.FileSystemRef, file uintptr, info *winfsp.FSP_FSCTL_FILE_INFO) error {
	h, ok := a.handles.get(file)
	if !ok || h.f == nil {
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
	h, ok := a.handles.get(file)
	if !ok {
		return os.ErrInvalid
	}
	_, err := a.statFileInfo(info, h.path)
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
		return err
	}
	_, err := a.statFileInfo(info, h.path)
	return err
}

// --- BehaviourSetFileSize --------------------------------------------------------------

func (a *Adapter) SetFileSize(
	_ *winfsp.FileSystemRef, file uintptr,
	newSize uint64, setAllocationSize bool,
	info *winfsp.FSP_FSCTL_FILE_INFO,
) error {
	h, ok := a.handles.get(file)
	if !ok || h.f == nil {
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
	// Close the data handle before removing so the backend can unlink cleanly.
	if h.f != nil {
		_ = h.f.Close()
		h.f = nil
	}
	_ = a.fsys.Remove(h.path)
}

// --- BehaviourRename -------------------------------------------------------------------

func (a *Adapter) Rename(_ *winfsp.FileSystemRef, _ uintptr, source, target string, _ bool) error {
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
	return a.fsys.Rename(src, dst)
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
		return os.ErrInvalid
	}
	entries, err := a.fsys.ReadDir(h.path)
	if err != nil {
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
	for _, de := range entries {
		var info winfsp.FSP_FSCTL_FILE_INFO
		if err := a.dirEntryInfo(&info, h.path, de); err != nil {
			continue // skip an entry we cannot stat rather than fail the whole listing
		}
		ok, err := fill(de.Name(), &info)
		if err != nil || !ok {
			return err
		}
	}
	return nil
}

// --- BehaviourGetDirInfoByName ---------------------------------------------------------

func (a *Adapter) GetDirInfoByName(
	_ *winfsp.FileSystemRef, parentDirFile uintptr, name string, dirInfo *winfsp.FSP_FSCTL_DIR_INFO,
) error {
	h, ok := a.handles.get(parentDirFile)
	if !ok {
		return os.ErrInvalid
	}
	child := joinStore(h.path, name)
	fi, err := a.fsys.Stat(child)
	if err != nil {
		return err
	}
	a.fillFileInfo(&dirInfo.FileInfo, child, fi)
	return nil
}

// mountOptions returns the go-winfsp Options for this adapter, passed to winfsp.Mount.
func (a *Adapter) mountOptions() []winfsp.Option {
	return []winfsp.Option{
		winfsp.CaseSensitive(false),
		winfsp.FileSystemName("ClassicStack"),
	}
}

// Compile-time assertions that the Adapter satisfies every delegate it means to.
var (
	_ winfsp.BehaviourBase              = (*Adapter)(nil)
	_ winfsp.BehaviourGetVolumeInfo     = (*Adapter)(nil)
	_ winfsp.BehaviourGetSecurityByName = (*Adapter)(nil)
	_ winfsp.BehaviourCreate            = (*Adapter)(nil)
	_ winfsp.BehaviourOverwrite         = (*Adapter)(nil)
	_ winfsp.BehaviourRead              = (*Adapter)(nil)
	_ winfsp.BehaviourWrite             = (*Adapter)(nil)
	_ winfsp.BehaviourFlush             = (*Adapter)(nil)
	_ winfsp.BehaviourGetFileInfo       = (*Adapter)(nil)
	_ winfsp.BehaviourSetBasicInfo      = (*Adapter)(nil)
	_ winfsp.BehaviourSetFileSize       = (*Adapter)(nil)
	_ winfsp.BehaviourCanDelete         = (*Adapter)(nil)
	_ winfsp.BehaviourCleanup           = (*Adapter)(nil)
	_ winfsp.BehaviourRename            = (*Adapter)(nil)
	_ winfsp.BehaviourGetSecurity       = (*Adapter)(nil)
	_ winfsp.BehaviourSetSecurity       = (*Adapter)(nil)
	_ winfsp.BehaviourReadDirectory     = (*Adapter)(nil)
	_ winfsp.BehaviourGetDirInfoByName  = (*Adapter)(nil)
)
