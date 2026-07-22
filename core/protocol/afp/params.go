package afp

import (
	"time"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// FileDirParams is the parsed form of one catalog entry's file OR directory parameter
// block — the client-direction counterpart to the server's packFileParams/packDirParams
// (core/service/afp/parms.go). Only the fields the reply's bitmap requested are
// populated; the rest stay zero. IsDir selects which bitmap governed the block.
//
// Names come from the 2-byte offset pointers into the block's trailing variable area:
// the raw wire bytes (MacRoman or UTF-8 per the request path-type) are returned in
// LongName/ShortName without decoding, so the caller applies the share codec.
type FileDirParams struct {
	IsDir bool

	Attributes uint16
	ParentDID  uint32
	CreateDate time.Time
	ModDate    time.Time
	BackupDate time.Time
	FinderInfo [32]byte
	LongName   []byte // raw wire bytes (undecoded)
	ShortName  []byte // raw wire bytes (undecoded)

	// File-only.
	FileNum     uint32
	DataForkLen uint32
	RsrcForkLen uint32

	// Directory-only.
	DirID        uint32
	Offspring    uint16
	OwnerID      uint32
	GroupID      uint32
	AccessRights uint32
}

// ParseFileDirParams decodes a packed parameter block governed by bitmap, for a file
// (isDir=false) or directory (isDir=true). block is the parameter block ONLY — the
// bytes after the reply's fixed header (e.g. after FPGetFileDirParms' fileBitmap/
// dirBitmap/type-pair, or an Enumerate entry's length/type bytes). Offsets in the name
// pointers are relative to the start of block, matching how the server packs them.
//
// It is tolerant: a field whose bytes run past the end of block is left zero rather
// than erroring, so a truncated reply still yields the fields that did fit.
func ParseFileDirParams(block []byte, bitmap uint16, isDir bool) FileDirParams {
	p := FileDirParams{IsDir: isDir}
	off := 0

	read16 := func() uint16 {
		if off+2 > len(block) {
			off = len(block) + 1
			return 0
		}
		v := bp.BE16(block[off : off+2])
		off += 2
		return v
	}
	read32 := func() uint32 {
		if off+4 > len(block) {
			off = len(block) + 1
			return 0
		}
		v := bp.BE32(block[off : off+4])
		off += 4
		return v
	}
	// nameAt reads the Pascal string at a variable-area offset (relative to block
	// start), for a name pointer.
	nameAt := func(ptr uint16) []byte {
		if int(ptr) >= len(block) {
			return nil
		}
		s, _, ok := PString(block, int(ptr))
		if !ok {
			return nil
		}
		return append([]byte(nil), s...)
	}

	// Shared low bits, in ascending bit order.
	if bitmap&FDBitmapAttributes != 0 {
		p.Attributes = read16()
	}
	if bitmap&FDBitmapParentDID != 0 {
		p.ParentDID = read32()
	}
	if bitmap&FDBitmapCreateDate != 0 {
		p.CreateDate = FromMacTime(read32())
	}
	if bitmap&FDBitmapModDate != 0 {
		p.ModDate = FromMacTime(read32())
	}
	if bitmap&FDBitmapBackupDate != 0 {
		p.BackupDate = FromMacTime(read32())
	}
	if bitmap&FDBitmapFinderInfo != 0 {
		if off+32 <= len(block) {
			copy(p.FinderInfo[:], block[off:off+32])
		}
		off += 32
	}
	var longPtr, shortPtr uint16
	var haveLong, haveShort bool
	if bitmap&FDBitmapLongName != 0 {
		longPtr = read16()
		haveLong = true
	}
	if bitmap&FDBitmapShortName != 0 {
		shortPtr = read16()
		haveShort = true
	}

	if !isDir {
		if bitmap&FileBitmapFileNum != 0 {
			p.FileNum = read32()
		}
		if bitmap&FileBitmapDataForkLen != 0 {
			p.DataForkLen = read32()
		}
		if bitmap&FileBitmapRsrcForkLen != 0 {
			p.RsrcForkLen = read32()
		}
		if bitmap&FileBitmapProDOSInfo != 0 {
			off += 6
		}
	} else {
		if bitmap&DirBitmapDirID != 0 {
			p.DirID = read32()
		}
		if bitmap&DirBitmapOffspring != 0 {
			p.Offspring = read16()
		}
		if bitmap&DirBitmapOwnerID != 0 {
			p.OwnerID = read32()
		}
		if bitmap&DirBitmapGroupID != 0 {
			p.GroupID = read32()
		}
		if bitmap&DirBitmapAccessRights != 0 {
			p.AccessRights = read32()
		}
		if bitmap&DirBitmapProDOSInfo != 0 {
			off += 6
		}
	}

	// Names last: the pointers were captured in the fixed area; resolve them into the
	// variable area now that the fixed section length is known.
	if haveLong {
		p.LongName = nameAt(longPtr)
	}
	if haveShort {
		p.ShortName = nameAt(shortPtr)
	}
	return p
}
