package fuse

import (
	iofs "io/fs"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
)

// Stat is the adapter's host-agnostic file metadata. The cgofuse host copies
// these fields into fuse.Stat_t.
type Stat struct {
	IsDir     bool
	Size      int64
	Mode      uint32 // permission bits (0444 / 0644 / 0755)
	Mtime     time.Time
	Atime     time.Time
	Ctime     time.Time
	Birthtime time.Time
	Ino       uint64
	Flags     uint32 // UF_HIDDEN
}

func (a *Adapter) fillStat(storePath string, fi iofs.FileInfo) Stat {
	isDir := fi.IsDir()
	st := Stat{
		IsDir: isDir,
		Size:  fi.Size(),
		Mtime: fi.ModTime(),
	}
	if isDir {
		st.Mode = 0o755
	} else {
		st.Mode = 0o644
	}

	dosAttr, hasDOS := wireDOSAttr(fi)
	if !wireMetaComplete(fi) && !hasDOS {
		dosAttr, hasDOS = a.fsys.Meta().Attrs(storePath)
	}
	if hasDOS && dosAttr.Attrs&metastore.DOSReadOnly != 0 {
		st.Mode &^= 0o222
	}
	if a.readOnly {
		st.Mode &^= 0o222
	}
	if hasDOS && dosAttr.Attrs&metastore.DOSHidden != 0 {
		st.Flags |= ufHidden
	}
	if hasDOS && !dosAttr.CreateTime.IsZero() {
		st.Birthtime = dosAttr.CreateTime
	} else {
		st.Birthtime = st.Mtime
	}
	if hasDOS && !dosAttr.AccessTime.IsZero() {
		st.Atime = dosAttr.AccessTime
	} else {
		st.Atime = st.Mtime
	}
	st.Ctime = st.Mtime

	if a.nativeForks {
		if info, ok := wireFinderInfo(fi); ok {
			applyInvisible(info, &st)
		} else if info, ok, err := a.fsys.ReadFinderInfo(storePath); err == nil && ok {
			applyInvisible(info, &st)
		}
	}

	if cnid, ok := a.fsys.Meta().CNID(storePath); ok {
		st.Ino = uint64(cnid)
	}
	return st
}

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

func applyInvisible(info [32]byte, st *Stat) {
	flags := uint16(info[8])<<8 | uint16(info[9])
	if flags&fdFlagsInvisible != 0 {
		st.Flags |= ufHidden
	}
}

func wireFinderInfo(fi iofs.FileInfo) ([32]byte, bool) {
	if sys := fi.Sys(); sys != nil {
		if fb, ok := sys.(fs.FinderInfoBits); ok {
			return fb.FinderInfo()
		}
	}
	return [32]byte{}, false
}

func wireRsrcLen(fi iofs.FileInfo) (int64, bool) {
	if sys := fi.Sys(); sys != nil {
		if rl, ok := sys.(fs.ResourceLenInfo); ok {
			return rl.ResourceForkLen(), true
		}
	}
	return 0, false
}
