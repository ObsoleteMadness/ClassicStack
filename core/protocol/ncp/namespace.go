package ncp

// namespace.go holds the wire DTOs for the NetWare name-space family — NCP
// function 0x57, the "Extended/Name-Space" calls that carry long filenames beyond
// DOS 8.3 (OS/2 and Macintosh name spaces). Wire-format only; the service
// (core/service/ncp) drives these against the storage seam.
//
// Reference: Novell NCP name-space calls; mars_nwe src/namspace.c +
// include/namspace.h (constants and the NW_HPATH / info-mask layouts are taken
// from there — CLAUDE.md #7).

import (
	"errors"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// Name-space IDs (mars_nwe namspace.h). A volume advertises which it serves via
// Get-Name-Spaces-Loaded (0x57/0x18); each request names the name space its path
// and reply name are encoded in.
const (
	NameDOS  uint8 = 0 // 8.3 upper-case (always served)
	NameMAC  uint8 = 1 // Macintosh: 31-char names, MacRoman charset
	NameNFS  uint8 = 2 // Unix: case-sensitive long names
	NameFTAM uint8 = 3 // OSI FTAM (not served)
	NameOS2  uint8 = 4 // OS/2: long names, OEM/ANSI charset
)

// Info-mask bits (mars_nwe namspace.h INFO_MSK_*). A get-info / search request
// carries a 32-bit mask selecting which sections the reply entry includes; the
// reply appends them in ascending bit order (build_dir_info).
const (
	InfoMskEntryName         uint32 = 0x00000001
	InfoMskDataStreamSpace   uint32 = 0x00000002
	InfoMskAttributeInfo     uint32 = 0x00000004
	InfoMskDataStreamSize    uint32 = 0x00000008
	InfoMskTotalDataStreamSz uint32 = 0x00000010
	InfoMskExtAttributes     uint32 = 0x00000020
	InfoMskArchiveInfo       uint32 = 0x00000040
	InfoMskModifyInfo        uint32 = 0x00000080
	InfoMskCreatInfo         uint32 = 0x00000100
	InfoMskNameSpaceInfo     uint32 = 0x00000200
	InfoMskDirEntryInfo      uint32 = 0x00000400
	InfoMskRightsInfo        uint32 = 0x00000800
)

// Open/create mode + action bits (mars_nwe namspace.h OPC_*). The 0x57/0x01
// request carries a mode; the reply reports the action actually taken.
const (
	OpcModeOpen    uint8 = 0x01
	OpcModeReplace uint8 = 0x02
	OpcModeCreat   uint8 = 0x08

	OpcActionOpen    uint8 = 0x01
	OpcActionCreat   uint8 = 0x02
	OpcActionReplace uint8 = 0x04
)

// Name-space subfunctions of function 0x57 (mars_nwe namspace.c handle_func_0x57).
// NOTE: for function 0x57 the subfunction byte is at the FRONT of the request data
// (requestdata[0]), not after a 2-byte length prefix as for 0x16/0x17.
const (
	NSGetNamespaceInfo uint8 = 0x00 // Get name-space info (per-volume)
	NSOpenCreate       uint8 = 0x01 // Open/Create File or Subdir
	NSInitSearch       uint8 = 0x02 // Initialize Search
	NSSearch           uint8 = 0x03 // Search for File or Dir
	NSObtainInfo       uint8 = 0x06 // Obtain File or Subdir Info
	NSGenDirBase       uint8 = 0x16 // Generate Dir Base and Volume Number
	NSGetLoadedList    uint8 = 0x18 // Get Name Spaces Loaded
)

// HPathFlag values for NW_HPATH.Flag (mars_nwe namspace.h): the path is anchored
// by a short dir handle (0), a 4-byte dir base (1), or neither (0xFF).
const (
	HPathFlagHandle uint8 = 0x00
	HPathFlagBase   uint8 = 0x01
	HPathFlagNone   uint8 = 0xFF
)

// ErrShortHPath is returned by ParseHPath for a buffer too short to hold the fixed
// NW_HPATH header or its declared components.
var ErrShortHPath = errors.New("ncp: NW_HPATH shorter than declared")

// HPath is the parsed NetWare handle-path (mars_nwe NW_HPATH): a volume, a 4-byte
// base/handle anchor, a flag selecting handle vs base, and the path Components
// (each a length-prefixed name, Pascal style). The base's low byte is a short dir
// handle when Flag==HPathFlagHandle.
type HPath struct {
	Volume     uint8
	Base       [4]byte
	Flag       uint8
	Components []string
}

// ParseHPath decodes an NW_HPATH at the head of b: volume(1), base[4], flag(1),
// components(1), then `components` Pascal strings (len byte + bytes). It returns
// the parsed path and the number of bytes consumed, or ErrShortHPath on a truncated
// buffer.
func ParseHPath(b []byte) (*HPath, int, error) {
	const fixed = 1 + 4 + 1 + 1 // volume, base[4], flag, components
	if len(b) < fixed {
		return nil, 0, ErrShortHPath
	}
	h := &HPath{Volume: b[0], Flag: b[5]}
	copy(h.Base[:], b[1:5])
	n := int(b[6])
	p := fixed
	for range n {
		if p >= len(b) {
			return nil, 0, ErrShortHPath
		}
		l := int(b[p])
		p++
		if p+l > len(b) {
			return nil, 0, ErrShortHPath
		}
		h.Components = append(h.Components, string(b[p:p+l]))
		p += l
	}
	return h, p, nil
}

// BaseHandle returns the 4-byte base as a uint32 (the dir-base id the service
// allocates and the client echoes). For a short-handle path (Flag==HPathFlagHandle)
// the low byte is the DOS dir handle.
func (h *HPath) BaseHandle() uint32 {
	return uint32(h.Base[0]) | uint32(h.Base[1])<<8 | uint32(h.Base[2])<<16 | uint32(h.Base[3])<<24
}

// DirEntryInfo is the protocol-neutral view of a directory entry the service fills
// for a name-space search/get-info reply; MarshalDirInfo serialises only the
// sections the request's info-mask selects. Times are NetWare DOS date/time words.
type DirEntryInfo struct {
	Name        string // the name in the REQUEST's name space (already encoded by the caller)
	IsDir       bool
	Size        uint32
	Attributes  uint32 // DOS attribute bits (read-only/hidden/system/archive/dir)
	CreateDate  uint16
	CreateTime  uint16
	ModifyDate  uint16
	ModifyTime  uint16
	ArchiveDate uint16
	ArchiveTime uint16
}

// MarshalDirInfo appends a build_dir_info-style reply entry to dst, including only
// the sections selected by infomask, in ascending bit order — matching mars_nwe's
// build_dir_info. The entry-name section (InfoMskEntryName) is a 1-byte length then
// the name bytes; the caller has already encoded Name in the request's name space.
func (e DirEntryInfo) MarshalDirInfo(infomask uint32, dst []byte) []byte {
	if infomask&InfoMskDataStreamSpace != 0 {
		dst = bp.AppendLE32(dst, e.Size) // allocated space (we report logical size)
	}
	if infomask&InfoMskAttributeInfo != 0 {
		dst = bp.AppendLE32(dst, e.Attributes)
	}
	if infomask&InfoMskDataStreamSize != 0 {
		dst = bp.AppendLE32(dst, e.Size)
	}
	if infomask&InfoMskTotalDataStreamSz != 0 {
		dst = bp.AppendLE32(dst, e.Size) // single data stream → total == size
		dst = append(dst, 1)             // number of data streams
	}
	if infomask&InfoMskArchiveInfo != 0 {
		dst = bp.AppendLE16(dst, e.ArchiveDate)
		dst = bp.AppendLE16(dst, e.ArchiveTime)
		dst = bp.AppendLE32(dst, 0) // archiver id
	}
	if infomask&InfoMskModifyInfo != 0 {
		dst = bp.AppendLE16(dst, e.ModifyDate)
		dst = bp.AppendLE16(dst, e.ModifyTime)
		dst = bp.AppendLE32(dst, 0) // modifier id
		dst = bp.AppendLE16(dst, e.ModifyDate)
	}
	if infomask&InfoMskCreatInfo != 0 {
		dst = bp.AppendLE16(dst, e.CreateDate)
		dst = bp.AppendLE16(dst, e.CreateTime)
		dst = bp.AppendLE32(dst, 0) // creator id
	}
	if infomask&InfoMskDirEntryInfo != 0 {
		dst = bp.AppendLE32(dst, 0) // directory entry number
		dst = bp.AppendLE32(dst, 0) // DOS directory entry number
		dst = append(dst, NameDOS)  // name space the entry was created in
		dst = append(dst, 0, 0)     // reserved
	}
	if infomask&InfoMskRightsInfo != 0 {
		dst = bp.AppendLE16(dst, 0xFFFF) // inherited rights mask (all)
	}
	if infomask&InfoMskEntryName != 0 {
		dst = append(dst, byte(len(e.Name)))
		dst = append(dst, e.Name...)
	}
	return dst
}
