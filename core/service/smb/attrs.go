package smb

import (
	stdfs "io/fs"
	"time"
)

// --- shared FS-command vocabulary: DOS file-attribute bits, the additional
// NTSTATUS codes the FS engine returns, and the FILETIME conversion. These are
// the pieces every FS handler (fileio/pathops/trans2) reaches for; the
// session-establishment statuses live in dispatch.go. ---

// DOS/SMB file-attribute bits ([MS-CIFS] §2.2.1.2.3 SMB_FILE_ATTRIBUTES).
const (
	attrReadOnly  uint16 = 0x0001
	attrHidden    uint16 = 0x0002
	attrSystem    uint16 = 0x0004
	attrVolume    uint16 = 0x0008
	attrDirectory uint16 = 0x0010
	attrArchive   uint16 = 0x0020
)

// Additional NTSTATUS codes the FS command engine returns ([MS-ERREF]). The
// session spine's statuses (success/not-supported/bad-network-name/…) live in
// dispatch.go; these are the file-operation outcomes. toWireStatus maps each to
// its DOS class/code for CORE-dialect clients that did not set NT_STATUS.
const (
	statusObjectNameNotFound  uint32 = 0xC0000034 // STATUS_OBJECT_NAME_NOT_FOUND
	statusObjectNameCollision uint32 = 0xC0000035 // STATUS_OBJECT_NAME_COLLISION
	statusObjectNameInvalid   uint32 = 0xC0000033 // STATUS_OBJECT_NAME_INVALID
	statusObjectPathNotFound  uint32 = 0xC000003A // STATUS_OBJECT_PATH_NOT_FOUND
	statusFileIsADirectory    uint32 = 0xC00000BA // STATUS_FILE_IS_A_DIRECTORY
	statusNotADirectory       uint32 = 0xC0000103 // STATUS_NOT_A_DIRECTORY
	statusDirectoryNotEmpty   uint32 = 0xC0000101 // STATUS_DIRECTORY_NOT_EMPTY
	statusNoMoreFiles         uint32 = 0x80000006 // STATUS_NO_MORE_FILES (informational)
	statusInvalidHandle       uint32 = 0xC0000008 // STATUS_INVALID_HANDLE
	statusUnsuccessful        uint32 = 0xC0000001 // STATUS_UNSUCCESSFUL (generic)
)

// windowsFiletimeEpoch is the 100-ns interval count between the FILETIME epoch
// (1601-01-01) and the Unix epoch (1970-01-01). It mirrors the
// windowsFiletimeOffset the NEGOTIATE handler already uses; kept named here so
// the time helpers read clearly.
const windowsFiletimeEpoch = windowsFiletimeOffset

// dosAttrs renders a FileInfo's DOS attribute word. A directory carries
// FILE_ATTRIBUTE_DIRECTORY; a regular file carries FILE_ATTRIBUTE_ARCHIVE (the
// "modified since last backup" convention DOS/Win9x expect on every plain file);
// a write-denied mode adds FILE_ATTRIBUTE_READONLY.
func dosAttrs(info stdfs.FileInfo) uint16 {
	if info.IsDir() {
		return attrDirectory
	}
	a := attrArchive
	if info.Mode().Perm()&0o222 == 0 {
		a |= attrReadOnly
	}
	return a
}

// fileTime converts a Go time to a Windows FILETIME (100-ns intervals since
// 1601). A zero or pre-epoch time renders as the epoch itself rather than a
// negative value, which legacy clients reject.
func fileTime(t time.Time) uint64 {
	if t.IsZero() {
		return windowsFiletimeEpoch
	}
	ns := t.UTC().UnixNano()
	if ns < 0 {
		return windowsFiletimeEpoch
	}
	return uint64(ns)/100 + windowsFiletimeEpoch
}

// allocSize rounds a file size up to the 4 KiB cluster the STANDARD/ALL info
// levels report as AllocationSize. A directory or empty file allocates nothing.
func allocSize(size uint64, isDir bool) uint64 {
	if isDir || size == 0 {
		return 0
	}
	const cluster = 4096
	return (size + cluster - 1) / cluster * cluster
}
