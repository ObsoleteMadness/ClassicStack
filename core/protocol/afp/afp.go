// Package afp is the AFP (Apple Filing Protocol) 2.x wire codec: exported command
// constants, bitmap/Finder-info constants, path-type bytes, result codes, and the
// per-command request-marshal + reply-parse DTOs a CLIENT uses to drive an AFP server.
//
// The SERVER side (core/service/afp) builds its request-parse / reply-marshal bodies
// inline against unexported constants; this package is the mirror half — the
// client-direction codec — with the same wire layouts lifted from Inside Macintosh:
// Networking (AFP 2.x §5–6) and the server handlers, so the two directions cannot
// drift. Round-trip tests cross-check that the server's own parser accepts what this
// package marshals (see afp_test.go).
//
// This package is wire-format only: no I/O, no session state, no goroutines. The ASP
// session, ATP requester, and the fs.FileSystem adapter live in the client/ ring.
//
// Ring: CORE (stdlib only, reflection-free — big-endian via core/binaryprimitives).
//
// References:
//   - Inside Macintosh: Networking, "AFP 2.x command reference"
//   - core/service/afp/{dispatch,handlers,parms,forkio}.go (the server mirror)
package afp

import "time"

// AFP command codes (Inside Macintosh: Networking, AFP 2.x §6 "AFP command summary").
// These are the EXPORTED mirror of the unexported cmd* set in
// core/service/afp/dispatch.go; the values are identical.
const (
	CmdByteRangeLock   uint8 = 1  // FPByteRangeLock
	CmdCloseVol        uint8 = 2  // FPCloseVol
	CmdCloseDir        uint8 = 3  // FPCloseDir
	CmdCloseFork       uint8 = 4  // FPCloseFork
	CmdCopyFile        uint8 = 5  // FPCopyFile
	CmdCreateDir       uint8 = 6  // FPCreateDir
	CmdCreateFile      uint8 = 7  // FPCreateFile
	CmdDelete          uint8 = 8  // FPDelete
	CmdEnumerate       uint8 = 9  // FPEnumerate
	CmdFlush           uint8 = 10 // FPFlush
	CmdFlushFork       uint8 = 11 // FPFlushFork
	CmdGetDirParms     uint8 = 12 // FPGetDirParms
	CmdGetFileParms    uint8 = 13 // FPGetFileParms
	CmdGetForkParms    uint8 = 14 // FPGetForkParms
	CmdGetSrvrInfo     uint8 = 15 // FPGetSrvrInfo
	CmdGetSrvrParms    uint8 = 16 // FPGetSrvrParms
	CmdGetVolParms     uint8 = 17 // FPGetVolParms
	CmdLogin           uint8 = 18 // FPLogin
	CmdLoginCont       uint8 = 19 // FPLoginCont
	CmdLogout          uint8 = 20 // FPLogout
	CmdMapID           uint8 = 21 // FPMapID
	CmdMapName         uint8 = 22 // FPMapName
	CmdMoveAndRename   uint8 = 23 // FPMoveAndRename
	CmdOpenVol         uint8 = 24 // FPOpenVol
	CmdOpenDir         uint8 = 25 // FPOpenDir
	CmdOpenFork        uint8 = 26 // FPOpenFork
	CmdRead            uint8 = 27 // FPRead
	CmdRename          uint8 = 28 // FPRename
	CmdSetDirParms     uint8 = 29 // FPSetDirParms
	CmdSetFileParms    uint8 = 30 // FPSetFileParms
	CmdSetForkParms    uint8 = 31 // FPSetForkParms
	CmdSetVolParms     uint8 = 32 // FPSetVolParms
	CmdWrite           uint8 = 33 // FPWrite
	CmdGetFileDirParms uint8 = 34 // FPGetFileDirParms
	CmdSetFileDirParms uint8 = 35 // FPSetFileDirParms
	CmdGetSrvrMsg      uint8 = 38 // FPGetSrvrMsg
)

// AFP path-type bytes (Inside Macintosh: Networking, AFP 2.x §5). The path-type byte
// prefixes every AFP pathname argument. Mirrors core/service/afp/pathtype.go.
const (
	PathTypeShortNames uint8 = 1 // 8.3 short name (MacRoman)
	PathTypeLongNames  uint8 = 2 // 31-byte long name (MacRoman)
	PathTypeUTF8Names  uint8 = 3 // kFPUTF8Name (UTF-8)
)

// Fork-type flag byte for FPOpenFork (Inside Macintosh: Networking, "OpenFork"). The
// high bit selects the resource fork; clear selects the data fork.
const (
	ForkFlagData     uint8 = 0x00
	ForkFlagResource uint8 = 0x80
)

// FPOpenFork access-mode bits (AFP 2.x "OpenFork access mode").
const (
	AccessRead  uint16 = 0x01
	AccessWrite uint16 = 0x02
)

// FromEndFlag is the high bit of the FPRead/FPWrite flag byte: the offset is measured
// from the end of the fork rather than the start.
const FromEndFlag uint8 = 0x80

// Volume-parameter bitmap bits (Inside Macintosh: Networking, "Volume bitmap").
const (
	VolBitmapAttributes uint16 = 1 << 0
	VolBitmapSignature  uint16 = 1 << 1
	VolBitmapCreateDate uint16 = 1 << 2
	VolBitmapModDate    uint16 = 1 << 3
	VolBitmapBackupDate uint16 = 1 << 4
	VolBitmapID         uint16 = 1 << 5
	VolBitmapBytesFree  uint16 = 1 << 6
	VolBitmapBytesTotal uint16 = 1 << 7
	VolBitmapName       uint16 = 1 << 8
)

// File/directory parameter bitmap bits (Inside Macintosh: Networking, "File
// parameters" / "Directory parameters"). Mirrors core/service/afp/parms.go. The file
// and directory bitmaps share the low bits (Attributes…ShortName) and diverge at bit 8.
const (
	// Shared low bits.
	FDBitmapAttributes uint16 = 1 << 0
	FDBitmapParentDID  uint16 = 1 << 1
	FDBitmapCreateDate uint16 = 1 << 2
	FDBitmapModDate    uint16 = 1 << 3
	FDBitmapBackupDate uint16 = 1 << 4
	FDBitmapFinderInfo uint16 = 1 << 5
	FDBitmapLongName   uint16 = 1 << 6
	FDBitmapShortName  uint16 = 1 << 7

	// File-only bits.
	FileBitmapFileNum     uint16 = 1 << 8
	FileBitmapDataForkLen uint16 = 1 << 9
	FileBitmapRsrcForkLen uint16 = 1 << 10
	FileBitmapProDOSInfo  uint16 = 1 << 13

	// Directory-only bits.
	DirBitmapDirID        uint16 = 1 << 8
	DirBitmapOffspring    uint16 = 1 << 9
	DirBitmapOwnerID      uint16 = 1 << 10
	DirBitmapGroupID      uint16 = 1 << 11
	DirBitmapAccessRights uint16 = 1 << 12
	DirBitmapProDOSInfo   uint16 = 1 << 13
)

// AFP file/directory Attributes bits (the FDBitmapAttributes word — Inside Macintosh:
// Networking, "File attributes" / "Directory attributes"). Only the ones with a DOS
// analogue are named here.
const (
	AttrInvisible    uint16 = 1 << 0 // kFPInvisibleBit — maps to DOS Hidden
	AttrMultiUser    uint16 = 1 << 1 // kFPMultiUserBit (dir)
	AttrSystem       uint16 = 1 << 2 // kFPSystemBit — maps to DOS System
	AttrWriteInhibit uint16 = 1 << 5 // kFPWriteInhibitBit — maps to DOS ReadOnly
)

// AFP volume signature values (Inside Macintosh: Networking, "Volume signature").
const (
	VolSignatureFlat       uint16 = 1
	VolSignatureFixedDirID uint16 = 2
	VolSignatureVarDirID   uint16 = 3
)

// CNIDRoot is the well-known directory id of the volume root (AFP dirID 2). A client
// resolves volume-relative paths against it.
const CNIDRoot uint32 = 2

// The Mac epoch: AFP timestamps count seconds since 1 Jan 2000, 00:00 GMT (Inside
// Macintosh: Networking, "AFP date/time").
var Epoch = time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)

// NoBackupDate is the AFP "never backed up" sentinel date (0x80000000).
const NoBackupDate uint32 = 0x80000000

// MacTime converts a wall-clock time to the signed 32-bit AFP timestamp.
func MacTime(t time.Time) uint32 { return uint32(int32(t.Sub(Epoch) / time.Second)) }

// FromMacTime converts a signed 32-bit AFP timestamp back to a wall-clock time. The
// NoBackupDate sentinel maps to the zero time.
func FromMacTime(mt uint32) time.Time {
	if mt == NoBackupDate {
		return time.Time{}
	}
	return Epoch.Add(time.Duration(int32(mt)) * time.Second)
}

// UAM names for FPLogin (Inside Macintosh: Networking, "User Authentication Methods").
// The client supports the two single-step UAMs the server accepts.
const (
	UAMNoUserAuthent = "No User Authent"
	UAMCleartext     = "Cleartxt Passwrd"
)

// AFP version strings the client offers at FPLogin. AFP2.1 is the classic baseline the
// server's default set advertises; AFPVersion21 is the safest single choice.
const (
	AFPVersion11 = "AFPVersion 1.1"
	AFPVersion20 = "AFPVersion 2.0"
	AFPVersion21 = "AFPVersion 2.1"
	AFPVersion22 = "AFP2.2"
)

// AFP result codes (kFP*; Inside Macintosh: Networking, "AFP result codes"). Signed
// 32-bit OSErr values carried in the ASP/ATP reply UserData. Exported so a client can
// interpret failures; mirror of the unexported set in core/service/afp/dispatch.go.
const (
	NoErr            int32 = 0
	ErrAccessDenied  int32 = -5000
	ErrBadUAM        int32 = -5002
	ErrBadVersNum    int32 = -5003
	ErrBitmapErr     int32 = -5004
	ErrCantMove      int32 = -5005
	ErrDiskFull      int32 = -5008
	ErrEOFErr        int32 = -5009
	ErrLockErr       int32 = -5013
	ErrMiscErr       int32 = -5014
	ErrNoMoreLocks   int32 = -5015
	ErrObjectExists  int32 = -5017
	ErrObjectNotFnd  int32 = -5018
	ErrParamErr      int32 = -5019
	ErrRangeNotLockd int32 = -5020
	ErrRangeOverlap  int32 = -5021
	ErrUserNotAuth   int32 = -5023
	ErrCallNotSuppt  int32 = -5024
	ErrObjectTypeErr int32 = -5025
	ErrDirNotFound   int32 = -5029
)

// ResultName renders an AFP result code for diagnostics.
func ResultName(code int32) string {
	switch code {
	case NoErr:
		return "kFPNoErr"
	case ErrAccessDenied:
		return "kFPAccessDenied"
	case ErrBadUAM:
		return "kFPBadUAM"
	case ErrBadVersNum:
		return "kFPBadVersNum"
	case ErrBitmapErr:
		return "kFPBitmapErr"
	case ErrCantMove:
		return "kFPCantMove"
	case ErrDiskFull:
		return "kFPDiskFull"
	case ErrEOFErr:
		return "kFPEOFErr"
	case ErrLockErr:
		return "kFPLockErr"
	case ErrMiscErr:
		return "kFPMiscErr"
	case ErrObjectExists:
		return "kFPObjectExists"
	case ErrObjectNotFnd:
		return "kFPObjectNotFound"
	case ErrParamErr:
		return "kFPParamErr"
	case ErrUserNotAuth:
		return "kFPUserNotAuth"
	case ErrCallNotSuppt:
		return "kFPCallNotSupported"
	case ErrObjectTypeErr:
		return "kFPObjectTypeErr"
	case ErrDirNotFound:
		return "kFPDirNotFound"
	default:
		return "kFP#" + itoa(int(code))
	}
}

// itoa renders a possibly-negative int without importing strconv (keeps the doc.go
// stdlib-only claim tidy; the value range is small).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
