// Package etherdfs holds the EtherDFS ("The Ethernet DOS File System", by
// Mateusz Viste) wire-format codec: the layer-2 frame header, the AL_* function
// opcodes, DOS error codes, FAT attribute bits, and the FCB / path helpers. It
// is wire-format only — no I/O, no filesystem state.
//
// Ring: CORE (stdlib only, reflection-free). EtherDFS is little-endian on the
// wire (the DOS client is a real-mode x86 TSR); LE integer codecs come from
// core/binaryprimitives, because encoding/binary transitively imports reflect.
//
// Reference: the EtherDFS protocol description (etherdfs.txt) and the reference
// server implementations github.com/unterwulf/etherdfs (etherdfs.txt),
// github.com/BrianHoldsworth/etherdfs-server and github.com/oerg866/ethersrv-866
// (E. Voirin, M. Viste). Opcode values and the FCB/attribute conventions are
// taken from those servers; deviations observed on the wire are recorded in
// spec/errata.md (CLAUDE.md rule #5).
package etherdfs

import "strings"

// EtherType is the custom EtherType that carries EtherDFS frames (0xEDF5).
const EtherType = 0xEDF5

// ProtocolVersion is the EtherDFS protocol version the client and server must
// agree on (the low 7 bits of the version+flags byte at frame offset 56).
const ProtocolVersion = 2

// AL_* function opcodes (frame offset 59). These map to the DOS network-redirector
// subfunctions the EtherDFS client TSR hooks; the names follow the reference server.
const (
	OpInstallChk uint8 = 0x00 // AL_INSTALLCHK: broadcast install check / server probe
	OpRmdir      uint8 = 0x01 // AL_RMDIR: remove directory
	OpMkdir      uint8 = 0x03 // AL_MKDIR: make directory
	OpChdir      uint8 = 0x05 // AL_CHDIR: change directory (validate path exists)
	OpClsfil     uint8 = 0x06 // AL_CLSFIL: close file
	OpCmmtfil    uint8 = 0x07 // AL_CMMTFIL: commit (flush) file
	OpReadfil    uint8 = 0x08 // AL_READFIL: read from file
	OpWritefil   uint8 = 0x09 // AL_WRITEFIL: write to file
	OpLockfil    uint8 = 0x0A // AL_LOCKFIL: lock region (no-op)
	OpUnlockfil  uint8 = 0x0B // AL_UNLOCKFIL: unlock region (no-op)
	OpDiskspace  uint8 = 0x0C // AL_DISKSPACE: query free/total disk space
	OpSetattr    uint8 = 0x0E // AL_SETATTR: set file attributes (FAT only)
	OpGetattr    uint8 = 0x0F // AL_GETATTR: get file attributes/time/size
	OpRename     uint8 = 0x11 // AL_RENAME: rename/move file
	OpDelete     uint8 = 0x13 // AL_DELETE: delete file
	OpOpen       uint8 = 0x16 // AL_OPEN: open existing file
	OpCreate     uint8 = 0x17 // AL_CREATE: create/truncate file
	OpFindFirst  uint8 = 0x1B // AL_FINDFIRST: find first matching directory entry
	OpFindNext   uint8 = 0x1C // AL_FINDNEXT: find next matching directory entry
	OpSkfmend    uint8 = 0x21 // AL_SKFMEND: seek from end of file
	OpSpopnfil   uint8 = 0x2E // AL_SPOPNFIL: special (extended) open file
)

// DOS error codes returned in the reply's leading AX status word. 0 means
// success; the rest are INT 21h error codes the redirector forwards to DOS.
const (
	ErrNone          uint16 = 0x00 // success
	ErrFileNotFound  uint16 = 0x02 // file not found
	ErrPathNotFound  uint16 = 0x03 // path not found
	ErrAccessDenied  uint16 = 0x05 // access denied / read-only / dest exists
	ErrInvalidHandle uint16 = 0x06 // invalid file handle
	ErrFileExists    uint16 = 0x50 // file already exists
	ErrNoMoreFiles   uint16 = 0x12 // no more files (find exhausted)
	ErrWriteFault    uint16 = 0x1D // write fault
	ErrReadFault     uint16 = 0x1E // read fault
)

// FAT attribute bits (the single attribute byte carried by GETATTR/SETATTR and
// the FCB find replies).
const (
	AttrReadOnly  uint8 = 0x01 // FILE_ATTRIBUTE_READONLY
	AttrHidden    uint8 = 0x02 // FILE_ATTRIBUTE_HIDDEN
	AttrSystem    uint8 = 0x04 // FILE_ATTRIBUTE_SYSTEM
	AttrVolume    uint8 = 0x08 // FILE_ATTRIBUTE_VOLUME_ID (volume label)
	AttrDirectory uint8 = 0x10 // FILE_ATTRIBUTE_DIRECTORY
	AttrArchive   uint8 = 0x20 // FILE_ATTRIBUTE_ARCHIVE
)

// FCBNameLen is the length of an 8.3 FCB filename on the wire: 8 base + 3
// extension bytes, space-padded, no embedded dot.
const FCBNameLen = 11

// FilenameToFCB renders an 8.3 short name (e.g. "REPORT~1.XLS") into the 11-byte
// space-padded FCB form ("REPORT~1XLS") the find replies carry. The base is
// padded/truncated to 8 bytes and the extension to 3; both are upper-cased. A
// name with no extension leaves the extension field all spaces.
func FilenameToFCB(name string) [FCBNameLen]byte {
	var fcb [FCBNameLen]byte
	for i := range fcb {
		fcb[i] = ' '
	}
	name = strings.ToUpper(strings.TrimSpace(name))
	base, ext := name, ""
	if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
		base, ext = name[:dot], name[dot+1:]
	}
	copy(fcb[0:8], base)
	copy(fcb[8:11], ext)
	return fcb
}

// FCBToFilename converts an 11-byte FCB name back to an 8.3 "BASE.EXT" string:
// the space-padded base and extension are trimmed and rejoined with a dot. A
// name with an empty extension has no trailing dot.
func FCBToFilename(fcb [FCBNameLen]byte) string {
	base := strings.TrimRight(string(fcb[0:8]), " ")
	ext := strings.TrimRight(string(fcb[8:11]), " ")
	if ext == "" {
		return base
	}
	return base + "." + ext
}

// NormalizePath converts an EtherDFS wire path to the '/'-separated, drive-less
// store path the filesystem seam uses: backslashes become forward slashes, a
// leading drive letter ("C:") is stripped, and a leading separator is removed so
// the result is relative to the share root. An empty or root path yields "".
func NormalizePath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	// Strip a leading drive letter ("C:/foo" or "C:foo").
	if len(p) >= 2 && p[1] == ':' && isDriveLetter(p[0]) {
		p = p[2:]
	}
	p = strings.TrimLeft(p, "/")
	return p
}

// isDriveLetter reports whether b is an ASCII letter usable as a DOS drive
// letter.
func isDriveLetter(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}
