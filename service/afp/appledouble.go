package afp

// Package-level support for AppleDouble (._filename) files.
//
// AppleDouble is the format used by macOS, netatalk 4.x, and Samba/CIFS to store
// resource forks and Finder metadata alongside regular files on non-HFS filesystems.
// The sidecar file is named "._" + original filename and lives in the same directory.
//
// References:
//   - AppleDouble / AppleSingle Formats, Apple II File Type Note $E0/0000
//   - netatalk 4.x source (afpd/unix.c, libatalk/adouble/)
//   - macOS copyfile(3) / xattr behavior on SMB/CIFS mounts

import (
	"encoding/binary"
	"io"
	"path/filepath"
)

const (
	adMagic   uint32 = 0x00051607
	adVersion uint32 = 0x00020000

	// AppleDouble entry IDs (AppleSingle/AppleDouble spec).
	adEntryIDDataFork     = uint32(1)
	adEntryIDFinderInfo   = uint32(9)
	adEntryIDResourceFork = uint32(2)
	adEntryIDComment      = uint32(4)
	// adEntryIDIconBW is the AppleSingle/AppleDouble entry ID for a classic
	// 32x32 1-bit Macintosh icon (see netatalk adouble.h AD_ICON). The payload
	// is 128 bytes of bitmap with no mask.
	adEntryIDIconBW = uint32(5)

	adHeaderSize = 26 // magic(4)+version(4)+filler(16)+numEntries(2)
	adEntrySize  = 12 // id(4)+offset(4)+length(4)

	// Offsets for a standard 2-entry AppleDouble (FinderInfo + ResourceFork).
	adFinderInfoOffset  = uint32(adHeaderSize + 2*adEntrySize) // 50
	adResourceForkStart = adFinderInfoOffset + 32              // 82

	// Byte offset of the resource-fork entry's "length" field within the file
	// for a canonical two-entry file (FinderInfo + ResourceFork).
	adRsrcLenFileOffset = int64(adHeaderSize + adEntrySize + 8) // 46
)

// appleDoublePath returns the modern (._name) sidecar path for filePath.
// Backend code may choose a different layout (for example legacy .AppleDouble).
func appleDoublePath(filePath string) string {
	return filepath.Join(filepath.Dir(filePath), "._"+filepath.Base(filePath))
}

// appleDoubleData holds the parsed contents of an AppleDouble sidecar file.
type appleDoubleData struct {
	finderInfo     [32]byte
	rsrcOffset     int64
	rsrcLength     int64
	rsrcLenFieldAt int64 // file offset of the ResourceFork entry's length field
	hasRsrc        bool
}

type parsedAppleDouble struct {
	finderInfo [32]byte
	comment    []byte
	rsrc       []byte
	iconBW     []byte
	rsrcOffset int64
	rsrcLenAt  int64
	hasFinder  bool
	hasComment bool
	hasRsrc    bool
	hasIconBW  bool
}

func parseAppleDoubleBytes(b []byte) (parsedAppleDouble, error) {
	var out parsedAppleDouble
	if len(b) < adHeaderSize {
		return out, io.ErrUnexpectedEOF
	}
	if binary.BigEndian.Uint32(b[0:4]) != adMagic {
		return out, io.ErrUnexpectedEOF
	}
	numEntries := int(binary.BigEndian.Uint16(b[24:26]))
	entriesStart := adHeaderSize
	entriesLen := numEntries * adEntrySize
	if len(b) < entriesStart+entriesLen {
		return out, io.ErrUnexpectedEOF
	}

	for i := 0; i < numEntries; i++ {
		off := entriesStart + i*adEntrySize
		id := binary.BigEndian.Uint32(b[off : off+4])
		eOff := int(binary.BigEndian.Uint32(b[off+4 : off+8]))
		eLen := int(binary.BigEndian.Uint32(b[off+8 : off+12]))
		if eOff < 0 || eLen < 0 || eOff+eLen > len(b) {
			continue
		}
		switch id {
		case adEntryIDFinderInfo:
			if eLen >= 32 {
				copy(out.finderInfo[:], b[eOff:eOff+32])
				out.hasFinder = true
			}
		case adEntryIDComment:
			if eLen > 0 {
				out.comment = append([]byte(nil), b[eOff:eOff+eLen]...)
				out.hasComment = true
			}
		case adEntryIDResourceFork:
			out.rsrcOffset = int64(eOff)
			out.rsrcLenAt = int64(off + 8)
			if eLen > 0 {
				out.rsrc = append([]byte(nil), b[eOff:eOff+eLen]...)
			} else {
				out.rsrc = nil
			}
			out.hasRsrc = true
		case adEntryIDIconBW:
			if eLen > 0 {
				out.iconBW = append([]byte(nil), b[eOff:eOff+eLen]...)
				out.hasIconBW = true
			}
		case adEntryIDDataFork:
			// Not used by our server; ignore.
		}
	}
	return out, nil
}

func buildAppleDoubleBytes(p parsedAppleDouble, includeCommentEntry bool, commentLen uint32) []byte {
	// We always write FinderInfo and ResourceFork entries.
	numEntries := 2
	if includeCommentEntry {
		numEntries = 3
	}
	headerLen := adHeaderSize + numEntries*adEntrySize

	finderOff := uint32(headerLen)
	finderLen := uint32(32)
	cur := finderOff + finderLen

	var commentOff uint32
	if includeCommentEntry {
		commentOff = cur
		cur += commentLen
	}

	rsrcOff := cur
	rsrcLen := uint32(len(p.rsrc))
	total := int(rsrcOff + rsrcLen)
	if total < int(rsrcOff) {
		total = int(rsrcOff)
	}
	out := make([]byte, total)

	// Header
	binary.BigEndian.PutUint32(out[0:4], adMagic)
	binary.BigEndian.PutUint32(out[4:8], adVersion)
	// filler [8:24] stays zero
	binary.BigEndian.PutUint16(out[24:26], uint16(numEntries))

	// Entries
	entriesStart := adHeaderSize
	putEntry := func(i int, id, off, ln uint32) {
		base := entriesStart + i*adEntrySize
		binary.BigEndian.PutUint32(out[base:base+4], id)
		binary.BigEndian.PutUint32(out[base+4:base+8], off)
		binary.BigEndian.PutUint32(out[base+8:base+12], ln)
	}

	putEntry(0, adEntryIDFinderInfo, finderOff, finderLen)
	if includeCommentEntry {
		putEntry(1, adEntryIDComment, commentOff, commentLen)
		putEntry(2, adEntryIDResourceFork, rsrcOff, rsrcLen)
	} else {
		putEntry(1, adEntryIDResourceFork, rsrcOff, rsrcLen)
	}

	// FinderInfo payload
	if p.hasFinder {
		copy(out[finderOff:finderOff+finderLen], p.finderInfo[:])
	}

	// Comment payload (if present)
	if includeCommentEntry && commentLen > 0 && len(p.comment) > 0 {
		copy(out[commentOff:commentOff+commentLen], p.comment[:commentLen])
	}

	// Resource fork payload (if present)
	if rsrcLen > 0 {
		copy(out[rsrcOff:rsrcOff+rsrcLen], p.rsrc)
	}

	return out
}
