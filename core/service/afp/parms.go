package afp

import (
	stdfs "io/fs"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// File/directory parameter bitmap bits (Inside Macintosh: Networking, "File
// parameters" / "Directory parameters"). The file and directory bitmaps share
// the low bits (Attributes…ShortName); they diverge at bit 8 (file: FileNum /
// dir: DirID), bit 9 (file: DataForkLen / dir: OffspringCount) and above, where
// the directory carries owner/group/access rights the file does not.
const (
	// Shared low bits (same meaning in both the file and directory bitmaps).
	fdBitmapAttributes uint16 = 1 << 0 // attribute flags
	fdBitmapParentDID  uint16 = 1 << 1 // parent directory id
	fdBitmapCreateDate uint16 = 1 << 2 // creation date
	fdBitmapModDate    uint16 = 1 << 3 // modification date
	fdBitmapBackupDate uint16 = 1 << 4 // backup date
	fdBitmapFinderInfo uint16 = 1 << 5 // 32-byte Finder info
	fdBitmapLongName   uint16 = 1 << 6 // long name (offset pointer)
	fdBitmapShortName  uint16 = 1 << 7 // short name (offset pointer)

	// File-only bits (bit 8 and up).
	fileBitmapFileNum     uint16 = 1 << 8  // file number (CNID)
	fileBitmapDataForkLen uint16 = 1 << 9  // data-fork length
	fileBitmapRsrcForkLen uint16 = 1 << 10 // resource-fork length
	fileBitmapProDOSInfo  uint16 = 1 << 13 // 6-byte ProDOS info

	// Directory-only bits (bit 8 and up).
	dirBitmapDirID        uint16 = 1 << 8  // directory id (CNID)
	dirBitmapOffspring    uint16 = 1 << 9  // offspring (child) count
	dirBitmapOwnerID      uint16 = 1 << 10 // owner id
	dirBitmapGroupID      uint16 = 1 << 11 // group id
	dirBitmapAccessRights uint16 = 1 << 12 // access-rights bitmap
	dirBitmapProDOSInfo   uint16 = 1 << 13 // 6-byte ProDOS info
)

// dirAccessRights / dirAccessRightsReadOnly are the access-rights longword AFP
// advertises for a directory (owner/group/everyone/user RWS bits packed as
// 0xUUOOGGEE). 0x87070707 grants read+write+search to everyone with the
// "owner == user" flag set (0x80); the read-only form drops the write bits to
// 0x03 (read+search) so a read-only volume tells the Finder not to offer writes.
// These match the legacy service/afp packer for bug-for-bug parity.
const (
	dirAccessRights         uint32 = 0x87070707
	dirAccessRightsReadOnly uint32 = 0x87030303
)

// fileDirParams packs one catalog entry's file or directory parameters into out
// in ascending bitmap-bit order. It mirrors the AFP 2.x parameter-block layout:
// fixed-size fields first (in bit order), variable-length names appended after,
// each name field carrying a 2-byte offset (from the start of the parameter
// block) into the variable area. Fields the bitmap does not request are omitted,
// so the caller's advertised bitmap exactly describes what is packed.
//
// store is the entry's '/'-separated store path; info its Stat; bitmap the file
// bitmap (when !info.IsDir) or directory bitmap (when info.IsDir); pathType the
// request's path-type byte, threaded into the FilenameCodec for the name fields.
func (v *Volume) fileDirParams(out []byte, store string, info stdfs.FileInfo, bitmap uint16, pathType uint8) []byte {
	if info.IsDir() {
		return v.packDirParams(out, store, info, bitmap, pathType)
	}
	return v.packFileParams(out, store, info, bitmap, pathType)
}

// packFileParams packs the file-parameter block (info.IsDir() == false).
func (v *Volume) packFileParams(out []byte, store string, info stdfs.FileInfo, bitmap uint16, pathType uint8) []byte {
	fixedSize := fileParamsFixedSize(bitmap)
	var names []byte // variable area, appended after the fixed fields

	if bitmap&fdBitmapAttributes != 0 {
		out = bp.AppendBE16(out, 0) // no attribute flags surfaced yet
	}
	if bitmap&fdBitmapParentDID != 0 {
		out = bp.AppendBE32(out, v.ParentCNID(store))
	}
	if bitmap&fdBitmapCreateDate != 0 {
		out = bp.AppendBE32(out, macTime(v.createTime(store, info)))
	}
	if bitmap&fdBitmapModDate != 0 {
		out = bp.AppendBE32(out, macTime(info.ModTime()))
	}
	if bitmap&fdBitmapBackupDate != 0 {
		out = bp.AppendBE32(out, noBackupDate)
	}
	if bitmap&fdBitmapFinderInfo != 0 {
		fi, _ := v.FinderInfo(store)
		out = append(out, fi[:]...)
	}
	if bitmap&fdBitmapLongName != 0 {
		out, names = v.appendName(out, names, fixedSize, v.MediumName(store), pathType)
	}
	if bitmap&fdBitmapShortName != 0 {
		out, names = v.appendName(out, names, fixedSize, v.ShortName(store), pathType)
	}
	if bitmap&fileBitmapFileNum != 0 {
		out = bp.AppendBE32(out, v.CNID(store))
	}
	if bitmap&fileBitmapDataForkLen != 0 {
		n, _ := v.ForkLen(store, fs.DataFork)
		out = bp.AppendBE32(out, uint32(n))
	}
	if bitmap&fileBitmapRsrcForkLen != 0 {
		n, _ := v.ForkLen(store, fs.ResourceFork)
		out = bp.AppendBE32(out, uint32(n))
	}
	if bitmap&fileBitmapProDOSInfo != 0 {
		out = append(out, make([]byte, 6)...)
	}
	return append(out, names...)
}

// packDirParams packs the directory-parameter block (info.IsDir() == true).
func (v *Volume) packDirParams(out []byte, store string, info stdfs.FileInfo, bitmap uint16, pathType uint8) []byte {
	fixedSize := dirParamsFixedSize(bitmap)
	var names []byte

	if bitmap&fdBitmapAttributes != 0 {
		out = bp.AppendBE16(out, 0)
	}
	if bitmap&fdBitmapParentDID != 0 {
		out = bp.AppendBE32(out, v.ParentCNID(store))
	}
	if bitmap&fdBitmapCreateDate != 0 {
		out = bp.AppendBE32(out, macTime(v.createTime(store, info)))
	}
	if bitmap&fdBitmapModDate != 0 {
		out = bp.AppendBE32(out, macTime(info.ModTime()))
	}
	if bitmap&fdBitmapBackupDate != 0 {
		out = bp.AppendBE32(out, noBackupDate)
	}
	if bitmap&fdBitmapFinderInfo != 0 {
		fi, _ := v.FinderInfo(store)
		out = append(out, fi[:]...)
	}
	if bitmap&fdBitmapLongName != 0 {
		out, names = v.appendName(out, names, fixedSize, v.MediumName(store), pathType)
	}
	if bitmap&fdBitmapShortName != 0 {
		out, names = v.appendName(out, names, fixedSize, v.ShortName(store), pathType)
	}
	if bitmap&dirBitmapDirID != 0 {
		out = bp.AppendBE32(out, v.CNID(store))
	}
	if bitmap&dirBitmapOffspring != 0 {
		out = bp.AppendBE16(out, v.offspringCount(store))
	}
	if bitmap&dirBitmapOwnerID != 0 {
		out = bp.AppendBE32(out, 0)
	}
	if bitmap&dirBitmapGroupID != 0 {
		out = bp.AppendBE32(out, 0)
	}
	if bitmap&dirBitmapAccessRights != 0 {
		rights := dirAccessRights
		if v.FS().Capabilities().ReadOnly {
			rights = dirAccessRightsReadOnly
		}
		out = bp.AppendBE32(out, rights)
	}
	if bitmap&dirBitmapProDOSInfo != 0 {
		out = append(out, make([]byte, 6)...)
	}
	return append(out, names...)
}

// appendName packs one variable-length name field: a 2-byte offset (from the
// start of the parameter block: fixedSize + the bytes already in the variable
// area) written into the fixed area, with the encoded name pushed onto the
// variable area. A name unrepresentable in the wire charset is emitted empty
// rather than mangled. Returns the grown fixed and variable buffers.
func (v *Volume) appendName(out, names []byte, fixedSize int, name string, pathType uint8) (fixed, variable []byte) {
	offset := uint16(fixedSize + len(names))
	out = bp.AppendBE16(out, offset)
	if wire, err := v.EncodeName(name, pathType); err == nil {
		names = putPString(names, wire)
	} else {
		names = putPString(names, nil)
	}
	return out, names
}

// offspringCount counts a directory's catalog children, skipping metadata
// shadows (._ sidecars, EA/stream paths) so the count matches what Enumerate
// would surface.
func (v *Volume) offspringCount(store string) uint16 {
	var count uint16
	if kids, err := v.Enumerate(store); err == nil {
		for _, k := range kids {
			if !isMetadataName(k.Name()) {
				count++
			}
		}
	}
	return count
}

// fileParamsFixedSize returns the byte length of the fixed-field area of a file
// parameter block for bitmap (name fields contribute their 2-byte offset
// pointer; the names themselves live in the trailing variable area).
func fileParamsFixedSize(bitmap uint16) int {
	size := 0
	size += fixedFieldsLow(bitmap)
	if bitmap&fileBitmapFileNum != 0 {
		size += 4
	}
	if bitmap&fileBitmapDataForkLen != 0 {
		size += 4
	}
	if bitmap&fileBitmapRsrcForkLen != 0 {
		size += 4
	}
	if bitmap&fileBitmapProDOSInfo != 0 {
		size += 6
	}
	return size
}

// dirParamsFixedSize returns the byte length of the fixed-field area of a
// directory parameter block for bitmap.
func dirParamsFixedSize(bitmap uint16) int {
	size := 0
	size += fixedFieldsLow(bitmap)
	if bitmap&dirBitmapDirID != 0 {
		size += 4
	}
	if bitmap&dirBitmapOffspring != 0 {
		size += 2
	}
	if bitmap&dirBitmapOwnerID != 0 {
		size += 4
	}
	if bitmap&dirBitmapGroupID != 0 {
		size += 4
	}
	if bitmap&dirBitmapAccessRights != 0 {
		size += 4
	}
	if bitmap&dirBitmapProDOSInfo != 0 {
		size += 6
	}
	return size
}

// fixedFieldsLow sizes the low bits shared by the file and directory bitmaps
// (Attributes…ShortName). Name fields count as their 2-byte offset pointer.
func fixedFieldsLow(bitmap uint16) int {
	size := 0
	if bitmap&fdBitmapAttributes != 0 {
		size += 2
	}
	if bitmap&fdBitmapParentDID != 0 {
		size += 4
	}
	if bitmap&fdBitmapCreateDate != 0 {
		size += 4
	}
	if bitmap&fdBitmapModDate != 0 {
		size += 4
	}
	if bitmap&fdBitmapBackupDate != 0 {
		size += 4
	}
	if bitmap&fdBitmapFinderInfo != 0 {
		size += 32
	}
	if bitmap&fdBitmapLongName != 0 {
		size += 2
	}
	if bitmap&fdBitmapShortName != 0 {
		size += 2
	}
	return size
}
