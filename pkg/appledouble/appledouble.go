// Package appledouble implements the AppleDouble v2 sidecar file
// format used by macOS, netatalk 4.x, and Samba/CIFS to store
// resource forks and Finder metadata alongside regular files on
// non-HFS filesystems. The sidecar file is named "._<original>"
// and lives in the same directory.
//
// This package is the format only: parse, build, and the constants.
// I/O strategy (where the sidecar lives, how it is opened, how
// metadata is grafted onto a host file) belongs to the caller.
//
// References:
//   - AppleDouble / AppleSingle Formats, Apple II File Type Note $E0/0000
//   - netatalk 4.x source (afpd/unix.c, libatalk/adouble/)
//   - macOS copyfile(3) / xattr behavior on SMB/CIFS mounts
package appledouble

import (
	"encoding/binary"
	"io"
	"path/filepath"
)

// Magic and version numbers from the AppleDouble spec.
const (
	Magic   uint32 = 0x00051607
	Version uint32 = 0x00020000
)

// Entry IDs from the AppleSingle/AppleDouble spec.
const (
	EntryIDDataFork     uint32 = 1
	EntryIDResourceFork uint32 = 2
	EntryIDComment      uint32 = 4
	// EntryIDIconBW is the entry ID for a classic 32x32 1-bit
	// Macintosh icon (netatalk adouble.h AD_ICON). The payload is
	// 128 bytes of bitmap with no mask.
	EntryIDIconBW    uint32 = 5
	EntryIDFinderInfo uint32 = 9
)

// Layout sizes.
const (
	HeaderSize = 26 // magic(4)+version(4)+filler(16)+numEntries(2)
	EntrySize  = 12 // id(4)+offset(4)+length(4)

	// FinderInfoOffset is the byte offset of the FinderInfo payload
	// in a canonical two-entry sidecar (FinderInfo + ResourceFork).
	FinderInfoOffset uint32 = HeaderSize + 2*EntrySize // 50

	// ResourceForkStart is the byte offset of the ResourceFork
	// payload in a canonical two-entry sidecar.
	ResourceForkStart uint32 = FinderInfoOffset + 32 // 82

	// ResourceLenFileOffset is the byte offset of the ResourceFork
	// entry's "length" field within the file for a canonical
	// two-entry sidecar (FinderInfo + ResourceFork).
	ResourceLenFileOffset int64 = HeaderSize + EntrySize + 8 // 46
)

// SidecarPath returns the modern (._name) sidecar path for filePath.
// Backend code may choose a different layout (e.g. legacy .AppleDouble).
func SidecarPath(filePath string) string {
	return filepath.Join(filepath.Dir(filePath), "._"+filepath.Base(filePath))
}

// Parsed holds the contents of a decoded AppleDouble sidecar.
type Parsed struct {
	FinderInfo [32]byte
	Comment    []byte
	Resource   []byte
	IconBW     []byte
	// ResourceOffset is the byte offset within the sidecar at which
	// the ResourceFork payload begins.
	ResourceOffset int64
	// ResourceLenAt is the byte offset of the ResourceFork entry's
	// length field within the sidecar header. Useful when patching
	// resource length without rewriting the whole file.
	ResourceLenAt int64
	HasFinder     bool
	HasComment    bool
	HasResource   bool
	HasIconBW     bool
}

// Parse decodes an AppleDouble sidecar's bytes. Returns
// io.ErrUnexpectedEOF for a short or malformed buffer.
func Parse(b []byte) (Parsed, error) {
	var out Parsed
	if len(b) < HeaderSize {
		return out, io.ErrUnexpectedEOF
	}
	if binary.BigEndian.Uint32(b[0:4]) != Magic {
		return out, io.ErrUnexpectedEOF
	}
	numEntries := int(binary.BigEndian.Uint16(b[24:26]))
	entriesStart := HeaderSize
	entriesLen := numEntries * EntrySize
	if len(b) < entriesStart+entriesLen {
		return out, io.ErrUnexpectedEOF
	}

	for i := 0; i < numEntries; i++ {
		off := entriesStart + i*EntrySize
		id := binary.BigEndian.Uint32(b[off : off+4])
		eOff := int(binary.BigEndian.Uint32(b[off+4 : off+8]))
		eLen := int(binary.BigEndian.Uint32(b[off+8 : off+12]))
		if eOff < 0 || eLen < 0 || eOff+eLen > len(b) {
			continue
		}
		switch id {
		case EntryIDFinderInfo:
			if eLen >= 32 {
				copy(out.FinderInfo[:], b[eOff:eOff+32])
				out.HasFinder = true
			}
		case EntryIDComment:
			if eLen > 0 {
				out.Comment = append([]byte(nil), b[eOff:eOff+eLen]...)
				out.HasComment = true
			}
		case EntryIDResourceFork:
			out.ResourceOffset = int64(eOff)
			out.ResourceLenAt = int64(off + 8)
			if eLen > 0 {
				out.Resource = append([]byte(nil), b[eOff:eOff+eLen]...)
			} else {
				out.Resource = nil
			}
			out.HasResource = true
		case EntryIDIconBW:
			if eLen > 0 {
				out.IconBW = append([]byte(nil), b[eOff:eOff+eLen]...)
				out.HasIconBW = true
			}
		case EntryIDDataFork:
			// Not used by AFP servers; ignore.
		}
	}
	return out, nil
}

// Build encodes p into a canonical AppleDouble sidecar. The result
// always contains a FinderInfo entry and a ResourceFork entry; if
// includeCommentEntry is true, a Comment entry of commentLen bytes
// is inserted between them.
func Build(p Parsed, includeCommentEntry bool, commentLen uint32) []byte {
	numEntries := 2
	if includeCommentEntry {
		numEntries = 3
	}
	headerLen := HeaderSize + numEntries*EntrySize

	finderOff := uint32(headerLen)
	finderLen := uint32(32)
	cur := finderOff + finderLen

	var commentOff uint32
	if includeCommentEntry {
		commentOff = cur
		cur += commentLen
	}

	rsrcOff := cur
	rsrcLen := uint32(len(p.Resource))
	total := int(rsrcOff + rsrcLen)
	if total < int(rsrcOff) {
		total = int(rsrcOff)
	}
	out := make([]byte, total)

	binary.BigEndian.PutUint32(out[0:4], Magic)
	binary.BigEndian.PutUint32(out[4:8], Version)
	binary.BigEndian.PutUint16(out[24:26], uint16(numEntries))

	entriesStart := HeaderSize
	putEntry := func(i int, id, off, ln uint32) {
		base := entriesStart + i*EntrySize
		binary.BigEndian.PutUint32(out[base:base+4], id)
		binary.BigEndian.PutUint32(out[base+4:base+8], off)
		binary.BigEndian.PutUint32(out[base+8:base+12], ln)
	}

	putEntry(0, EntryIDFinderInfo, finderOff, finderLen)
	if includeCommentEntry {
		putEntry(1, EntryIDComment, commentOff, commentLen)
		putEntry(2, EntryIDResourceFork, rsrcOff, rsrcLen)
	} else {
		putEntry(1, EntryIDResourceFork, rsrcOff, rsrcLen)
	}

	if p.HasFinder {
		copy(out[finderOff:finderOff+finderLen], p.FinderInfo[:])
	}
	if includeCommentEntry && commentLen > 0 && len(p.Comment) > 0 {
		copy(out[commentOff:commentOff+commentLen], p.Comment[:commentLen])
	}
	if rsrcLen > 0 {
		copy(out[rsrcOff:rsrcOff+rsrcLen], p.Resource)
	}

	return out
}
