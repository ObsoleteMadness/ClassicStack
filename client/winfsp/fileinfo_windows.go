//go:build windows

package winfsp

import (
	iofs "io/fs"

	winfsp "github.com/winfsp/go-winfsp"
	"github.com/winfsp/go-winfsp/filetime"
	"golang.org/x/sys/windows"

	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
)

// fillFileInfo populates a WinFsp FSP_FSCTL_FILE_INFO for the store path from its
// io/fs.FileInfo plus the share's stored DOS attributes/dates. It is the single mapping
// used by GetFileInfo, Open, Create, and every directory entry.
func (a *Adapter) fillFileInfo(info *winfsp.FSP_FSCTL_FILE_INFO, storePath string, fi iofs.FileInfo) {
	isDir := fi.IsDir()

	var attrs uint32
	if isDir {
		attrs |= windows.FILE_ATTRIBUTE_DIRECTORY
	}

	// Layer on stored DOS attributes (read-only/hidden/system/archive map 1:1 onto the
	// FILE_ATTRIBUTE_* low byte — see metastore/dosattr.go).
	dosAttr, hasDOS := a.fsys.Meta().Attrs(storePath)
	if hasDOS {
		attrs |= uint32(dosAttr.Attrs & metastore.DOSStorableMask)
	}
	if a.readOnly {
		attrs |= windows.FILE_ATTRIBUTE_READONLY
	}
	if attrs == 0 {
		attrs = windows.FILE_ATTRIBUTE_NORMAL
	}
	info.FileAttributes = attrs
	info.ReparseTag = 0

	if !isDir {
		info.FileSize = uint64(fi.Size())
		info.AllocationSize = (info.FileSize + 4095) / 4096 * 4096
	}

	mtime := filetime.Timestamp(fi.ModTime())
	info.LastWriteTime = mtime
	info.ChangeTime = mtime

	// Creation time: stored DOS create-time if known, else the mtime.
	if hasDOS && !dosAttr.CreateTime.IsZero() {
		info.CreationTime = filetime.Timestamp(dosAttr.CreateTime)
	} else {
		info.CreationTime = mtime
	}
	// Access time: stored DOS access-time if known, else the write time.
	if hasDOS && !dosAttr.AccessTime.IsZero() {
		info.LastAccessTime = filetime.Timestamp(dosAttr.AccessTime)
	} else {
		info.LastAccessTime = info.LastWriteTime
	}

	// A stable per-file id helps Windows correlate handles; the share's CNID is ideal.
	if cnid, ok := a.fsys.Meta().CNID(storePath); ok {
		info.IndexNumber = uint64(cnid)
	}
	info.HardLinks = 0
	info.EaSize = 0
}

// statFileInfo Stats a store path and fills a FILE_INFO, returning the io/fs.FileInfo too.
func (a *Adapter) statFileInfo(info *winfsp.FSP_FSCTL_FILE_INFO, storePath string) (iofs.FileInfo, error) {
	fi, err := a.fsys.Stat(storePath)
	if err != nil {
		return nil, err
	}
	a.fillFileInfo(info, storePath, fi)
	return fi, nil
}

// dirEntryInfo fills a FILE_INFO for a directory child from its io/fs.DirEntry, avoiding a
// per-entry Stat when the entry already carries an Info() (memfs/local_fs both do).
func (a *Adapter) dirEntryInfo(info *winfsp.FSP_FSCTL_FILE_INFO, dir string, de iofs.DirEntry) error {
	fi, err := de.Info()
	if err != nil {
		return err
	}
	a.fillFileInfo(info, joinStore(dir, de.Name()), fi)
	return nil
}
