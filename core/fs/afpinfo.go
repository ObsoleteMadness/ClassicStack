package fs

import (
	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// AfpInfo is the 60-byte Services-for-Macintosh (SFM) "AFP_AfpInfo" metadata
// record (spec/16 §1b). NT SFM and the SMB2 SMB2_FS_ATTRIBUTE mapping surface a
// file's 32-byte Finder info (and a few Apple/ProDOS fields) through a named
// stream carrying this record, so a fork written by ClassicStack is readable by
// Windows SFM/SMB and vice-versa. The ads fork engine (fork_ads.go) stores it in
// the "name:AFP_AfpInfo" ADS; the WinFsp mount client re-exposes it under the
// same stream name.
//
// Layout (all multi-byte fields big-endian, the AFP on-the-wire order):
//
//	 0  signature  uint32  'AFP\0' (0x41465000)
//	 4  version    uint32  0x00010000
//	 8  reserved1  uint32
//	12  backupTime uint32
//	16  finderInfo [32]byte
//	48  prodosInfo [6]byte
//	54  reserved2  [6]byte
//
// Only FinderInfo is meaningful to the ForkEngine seam today; BackupTime and
// ProDOSInfo are preserved on round-trip so a record written by Windows SFM is
// not clobbered.
type AfpInfo struct {
	BackupTime uint32
	FinderInfo [32]byte
	ProDOSInfo [6]byte
}

// AfpInfo record constants (spec/16 §1b).
const (
	// AfpInfoSize is the fixed on-disk size of the AFP_AfpInfo record.
	AfpInfoSize = 60

	afpInfoSignature uint32 = 0x41465000 // 'A''F''P''\0'
	afpInfoVersion   uint32 = 0x00010000

	afpInfoFinderOff = 16 // FinderInfo[32] starts here
	afpInfoFinderLen = 32
)

// Marshal builds a canonical 60-byte AFP_AfpInfo record.
func (a AfpInfo) Marshal() []byte {
	b := make([]byte, AfpInfoSize)
	bp.PutBE32(b[0:4], afpInfoSignature)
	bp.PutBE32(b[4:8], afpInfoVersion)
	// b[8:12] reserved1, b[12:16] backupTime.
	bp.PutBE32(b[12:16], a.BackupTime)
	copy(b[afpInfoFinderOff:afpInfoFinderOff+afpInfoFinderLen], a.FinderInfo[:])
	copy(b[48:54], a.ProDOSInfo[:])
	// b[54:60] reserved2.
	return b
}

// UnmarshalAfpInfo decodes a 60-byte AFP_AfpInfo record, validating the
// signature. A short buffer or wrong signature returns ErrBadAfpInfo; callers
// that mirror SFM tolerance treat that as "no FinderInfo present" rather than a
// fatal error.
func UnmarshalAfpInfo(b []byte) (AfpInfo, error) {
	var a AfpInfo
	if len(b) < AfpInfoSize {
		return a, ErrBadAfpInfo
	}
	if bp.BE32(b[0:4]) != afpInfoSignature {
		return a, ErrBadAfpInfo
	}
	a.BackupTime = bp.BE32(b[12:16])
	copy(a.FinderInfo[:], b[afpInfoFinderOff:afpInfoFinderOff+afpInfoFinderLen])
	copy(a.ProDOSInfo[:], b[48:54])
	return a, nil
}
