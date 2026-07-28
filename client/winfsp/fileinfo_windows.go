//go:build windows

package winfsp

import (
	iofs "io/fs"
	"time"

	winfsp "github.com/winfsp/go-winfsp"
	"github.com/winfsp/go-winfsp/filetime"
	"golang.org/x/sys/windows"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
)

// fatEpochFiletime is the WinFsp FILETIME for the FAT epoch (1980-01-01 UTC), used as the
// timestamp for a backend that surfaces no real date — a plausible value Explorer renders,
// versus the ~1754 garbage a zero time.Time would produce through filetime.Timestamp.
var fatEpochFiletime = filetime.Timestamp(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))

// filetimeOr converts t to a WinFsp FILETIME, or returns fallback when t is the zero time.
func filetimeOr(t time.Time, fallback uint64) uint64 {
	if t.IsZero() {
		return fallback
	}
	return filetime.Timestamp(t)
}

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
	// FILE_ATTRIBUTE_* low byte — see metastore/dosattr.go). When the FileInfo already
	// carries wire metadata (AFP enumerate, projected sidecar entry) do not call
	// Meta().Attrs — that would Stat every listing entry and materialise sidecars.
	dosAttr, hasDOS := wireDOSAttr(fi)
	if !wireMetaComplete(fi) && !hasDOS {
		dosAttr, hasDOS = a.fsys.Meta().Attrs(storePath)
	} else if wireMetaComplete(fi) {
		hasDOS = dosAttr.Attrs != 0 || !dosAttr.CreateTime.IsZero() || !dosAttr.AccessTime.IsZero()
	}
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

	// Timestamps. A zero time.Time must NOT be fed to filetime.Timestamp — it maps to a
	// bogus year (~1754), which Explorer then displays. Fall back to a fixed sane epoch
	// (the FAT epoch, 1980-01-01) so a backend that does not surface a given time shows a
	// plausible date rather than garbage.
	mtime := filetimeOr(fi.ModTime(), fatEpochFiletime)
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

// wireMetaComplete reports whether fi.Sys() already came from the wire or a
// synthesised listing entry and must not trigger Meta().Attrs → Stat.
func wireMetaComplete(fi iofs.FileInfo) bool {
	if sys := fi.Sys(); sys != nil {
		if _, ok := sys.(fs.WireMetaComplete); ok {
			return true
		}
		_, ok := sys.(fs.DOSAttrInfo)
		return ok
	}
	return false
}

// wireDOSAttr reads DOS attribute bits from fi.Sys() when the backend attached them.
func wireDOSAttr(fi iofs.FileInfo) (metastore.DOSAttr, bool) {
	sys := fi.Sys()
	if sys == nil {
		return metastore.DOSAttr{}, false
	}
	var attr metastore.DOSAttr
	var has bool
	if da, ok := sys.(fs.DOSAttrInfo); ok {
		attr.Attrs = da.DOSAttrs() & metastore.DOSStorableMask
		if attr.Attrs != 0 {
			has = true
		}
	}
	if ct, ok := sys.(fs.DOSCreateTimeInfo); ok {
		if t := ct.DOSCreateTime(); !t.IsZero() {
			attr.CreateTime = t
			has = true
		}
	}
	return attr, has
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
