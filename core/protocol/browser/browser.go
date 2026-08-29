// Package browser is the wire codec for the Microsoft NetBIOS browser protocol
// ([MS-BRWS]): the \MAILSLOT\BROWSE frames a browser server and clients exchange
// — host/domain/local-master announcements, the master-browser election, and the
// GetBackupList request/response. Each frame is a self-serialising DTO (Marshal /
// Unmarshal, per the project DTO rule) so callers never decode bytes inline.
//
// These are the BARE browser frames only — the SMB_COM_TRANSACTION mailslot
// envelope that carries them on \MAILSLOT\BROWSE is core/protocol/mailslot
// (§3-quater), wrapped/unwrapped by the mailslot dispatch layer, never here. The
// browser SERVICE (core/service/browser) is the state machine over these codecs;
// this package holds no state and no envelope.
//
// Ring: CORE (stdlib only, reflection-free). Fixed-width fields use
// core/binaryprimitives; all multi-byte fields are little-endian (SMB wire order).
package browser

import (
	"errors"
	"strings"
)

// Browser frame opcodes ([MS-BRWS] §2.2), the first byte of a browser payload.
const (
	OpHostAnnouncement    uint8 = 0x01
	OpAnnouncementRequest uint8 = 0x02
	OpRequestElection     uint8 = 0x08
	OpGetBackupListReq    uint8 = 0x09
	OpGetBackupListResp   uint8 = 0x0A
	OpDomainAnnouncement  uint8 = 0x0C
	OpLocalMasterAnnounce uint8 = 0x0F
)

// Server-type bits ([MS-BRWS] §2.2 SV_TYPE_*), used in announcements and the
// browse-list / backup-list filtering.
const (
	ServerTypeWorkstation      uint32 = 0x00000001
	ServerTypeServer           uint32 = 0x00000002
	ServerTypeWfW              uint32 = 0x00002000
	ServerTypePotentialBrowser uint32 = 0x00010000
	ServerTypeBackupBrowser    uint32 = 0x00020000
	ServerTypeMasterBrowser    uint32 = 0x00040000
	ServerTypeDomainMaster     uint32 = 0x00080000
	ServerTypeWindows95Plus    uint32 = 0x00400000
	ServerTypeLocalListOnly    uint32 = 0x40000000
	ServerTypeDomainEnum       uint32 = 0x80000000
)

// Composite server types ClassicStack announces, each byte-for-byte a value a real
// Win98 station puts on the wire.
const (
	// ServerTypeWorkstationSet (0x00402003) is the base type on every ClassicStack
	// Host/LocalMaster announcement: Workstation | Server | WfW | Windows 95+. It is
	// exactly what WIN98-NBF-2 announces in spec/captures/nbf-win98.pcap frame 62 and
	// WIN98-IPX-2 in spec/captures/nwlink-win98.pcap frame 7. Role bits
	// (Potential/Backup/Master Browser) are ORed on top per announcement.
	ServerTypeWorkstationSet uint32 = ServerTypeWorkstation | ServerTypeServer |
		ServerTypeWfW | ServerTypeWindows95Plus

	// ServerTypeDomainAnnounce (0x80402000) is the type field of a DomainAnnouncement
	// (0x0C): Domain Enum | WfW | Windows 95+, and NOT the Workstation/Server bits —
	// the frame describes the WORKGROUP, not the announcing host. Observed identically
	// in spec/captures/nbf-win98.pcap frames 141/745 and
	// spec/captures/nbipx-win98.pcap frames 24/274.
	ServerTypeDomainAnnounce uint32 = ServerTypeDomainEnum | ServerTypeWfW | ServerTypeWindows95Plus
)

// Election criteria + the browser/OS version bytes ClassicStack advertises.
const (
	ElectionVersion      uint8  = 0x01
	BrowserVersionMajor  uint8  = 0x0F
	BrowserVersionMinor  uint8  = 0x01
	AnnounceVersionMajor uint8  = 0x15
	AnnounceVersionMinor uint8  = 0x04
	Signature            uint16 = 0xAA55
)

// Election-criteria component bytes ([MS-BRWS] §2.2.17). The criteria is ONE
// unsigned 32-bit value compared whole (Compare), which is why the fields are
// packed most-significant-first in precedence order: OS beats version, version
// beats desire.
//
//	bits 24-31  Election OS
//	bits 16-23  browser protocol MINOR version
//	bits  8-15  browser protocol MAJOR version
//	bits  0-7   Election Desire
//
// ERRATA (captures/ipx.pcap 2026-08-19 frame 163): a Win98 station advertises
// criteria 0x01041500 — OS 0x01 (WfW), minor 0x04, major 0x15, desire 0x00 —
// which confirms this byte order against the announcement version pair we already
// emit (AnnounceVersionMajor 0x15 / AnnounceVersionMinor 0x04).
const (
	ElectionOSWfW        uint8 = 0x01 // Windows for Workgroups — what our announcements claim
	ElectionOSNTWorkstn  uint8 = 0x10
	ElectionOSNTServer   uint8 = 0x20
	ElectionDesireBackup uint8 = 0x01
	ElectionDesireMaster uint8 = 0x04
)

// ElectionCriteria packs the four criteria bytes into the comparable 32-bit value.
func ElectionCriteria(os, major, minor, desire uint8) uint32 {
	return uint32(os)<<24 | uint32(minor)<<16 | uint32(major)<<8 | uint32(desire)
}

// ElectionCriteriaMaster is the candidacy ClassicStack advertises: the same OS and
// browser-protocol version its Host/LocalMaster announcements already claim, with
// the Master desire bit set.
//
// It used to be the bare constant 0x00000004 — desire only, with OS and version
// left zero. Against a real Win9x/WfW peer (0x01041500) that lost the FIRST and
// highest-precedence comparison every time, so ClassicStack could never hold the
// master role on a segment with any Windows station on it, and the two flapped
// (captures/ipx.pcap 2026-08-19: both declared Local Master, repeatedly).
var ElectionCriteriaMaster = ElectionCriteria(
	ElectionOSWfW, AnnounceVersionMajor, AnnounceVersionMinor, ElectionDesireMaster)

// Browser NetBIOS name suffixes ([MS-BRWS] §2.1.1). Every golden capture agrees on
// which frame goes to which suffix:
//
//	<1B>  domain master browser  — GetBackupList probe for a domain master
//	<1D>  local master browser   — HostAnnouncement (0x01), GetBackupList request (0x09)
//	<1E>  browser election group — RequestElection (0x08), LocalMasterAnnouncement (0x0F),
//	                               AnnouncementRequest (0x02)
//	<01>  __MSBROWSE__ segment master group — DomainAnnouncement (0x0C)
//
// (spec/captures/nbf-win98.pcap frames 18/22/32/41/59/60/61/141.)
const (
	NameTypeSegmentMaster uint8 = 0x01 // the __MSBROWSE__ suffix
	NameTypeDomainMaster  uint8 = 0x1B
	NameTypeMasterBrowser uint8 = 0x1D
	NameTypeElection      uint8 = 0x1E // == netbios.NameTypeGroup
)

// MSBrowseName is the special segment-master group name every local master browser
// registers ([MS-BRWS] §2.1.1): the 15 visible bytes are 0x01 0x02 "__MSBROWSE__"
// 0x02 and the suffix is <01>. A DomainAnnouncement is addressed to it, so every
// master browser on the segment learns the workgroup (spec/captures/nbf-win98.pcap
// frame 141; spec/captures/nbf-os2-win98.pcap frames 73/76/84).
//
// It is built as raw bytes rather than through a name constructor, which would
// upper-case and space-pad and so corrupt the 0x01/0x02 framing bytes. It is a bare
// [16]byte so this package stays free of a NetBIOS-name import; callers convert.
var MSBrowseName = func() [16]byte {
	var n [16]byte
	n[0] = 0x01
	n[1] = 0x02
	copy(n[2:], "__MSBROWSE__")
	n[14] = 0x02
	n[15] = NameTypeSegmentMaster
	return n
}()

// errors returned by the Unmarshal methods.
var (
	ErrShort = errors.New("browser: frame too short")
	ErrBadOp = errors.New("browser: wrong opcode for frame type")
)

// --- name helpers (browser names are 16-byte fixed, space/NUL trimmed) ---

// NormalizeName upper-cases, trims, and caps a browser/server name at 15 bytes.
func NormalizeName(name string) string {
	upper := strings.ToUpper(strings.TrimSpace(name))
	if len(upper) > 15 {
		upper = upper[:15]
	}
	return upper
}

// fixedName renders name into a 16-byte zero-padded field.
func fixedName(name string) [16]byte {
	var out [16]byte
	copy(out[:], NormalizeName(name))
	return out
}

// appendName appends a NUL-terminated normalised name.
func appendName(dst []byte, name string) []byte {
	return append(append(dst, NormalizeName(name)...), 0)
}

// parseName reads a NUL-terminated (or field-bounded) browser string.
func parseName(b []byte) string {
	if i := indexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return strings.TrimRight(string(b), "\x00 ")
}

func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

// IsCommandByte reports whether b is a recognised browser opcode (used to detect
// the browser payload inside an optional Win9x preamble).
func IsCommandByte(b uint8) bool {
	switch b {
	case OpHostAnnouncement, OpAnnouncementRequest, OpRequestElection,
		OpGetBackupListReq, OpGetBackupListResp, OpLocalMasterAnnounce, OpDomainAnnouncement:
		return true
	}
	return false
}

// Minimum body length of each browser frame, used by UnwrapPayload to tell a real
// opcode from a leading pad byte that happens to look like one. Each equals the
// smallest form the corresponding Unmarshal accepts.
const (
	AnnouncementRequestMinLen = 2  // opcode + reserved
	GetBackupListMinLen       = 6  // opcode + count + 4-byte token
	ElectionMinLen            = 15 // 14 fixed + a NUL-terminated (possibly empty) name
	AnnouncementMinLen        = 33 // 32 fixed + a NUL-terminated (possibly empty) comment
	DomainAnnouncementMinLen  = 33 // 32 fixed + a NUL-terminated local-master name

	// win9xPadLen is the length of the two-byte SMB_COM_TRANSACTION pad some Win9x
	// stacks leave between the mailslot name and the data block. A well-formed
	// envelope names the data with DataOffset/DataCount so the pad is skipped by
	// core/protocol/mailslot; this only covers a caller that hands us the raw tail.
	win9xPadLen = 2
)

// minFrameLen is the smallest well-formed body for a browser opcode, or 0 for a byte
// that is not an opcode at all.
func minFrameLen(op uint8) int {
	switch op {
	case OpAnnouncementRequest:
		return AnnouncementRequestMinLen
	case OpGetBackupListReq, OpGetBackupListResp:
		return GetBackupListMinLen
	case OpRequestElection:
		return ElectionMinLen
	case OpHostAnnouncement, OpLocalMasterAnnounce:
		return AnnouncementMinLen
	case OpDomainAnnouncement:
		return DomainAnnouncementMinLen
	}
	return 0
}

// isFrameAt reports whether b begins with an opcode AND is long enough to be that
// frame — the length test is what separates a genuine opcode from a stray pad byte.
func isFrameAt(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	min := minFrameLen(b[0])
	return min > 0 && len(b) >= min
}

// UnwrapPayload extracts the browser opcode + frame from a mailslot payload,
// tolerating the two-byte pad some Win9x stacks leave ahead of the data block.
// Returns the opcode, the frame starting at the opcode, and ok.
//
// ERRATA (spec/captures/nbf-win98.pcap frames 438/440/746, nbipx-win98.pcap frames
// 25/268): that pad is NOT the fixed 01 03 / 0f 06 preamble this used to allow-list —
// it is whatever two bytes the sender's buffer last held. Real Win98 emitted
// `0f 07`, `0c 00`, `00 07` and `33 42` ahead of a GetBackupList request in these
// captures. Two of those (0F LocalMasterAnnouncement, 0C DomainAnnouncement) are
// themselves valid opcodes, so a pad-blind "first byte wins" test decoded a 7-byte
// GetBackupList as a truncated announcement and dropped it. The opcode is therefore
// chosen by opcode-AND-length, and only then does the pad skip apply.
func UnwrapPayload(payload []byte) (op uint8, frame []byte, ok bool) {
	if isFrameAt(payload) {
		return payload[0], payload, true
	}
	if len(payload) > win9xPadLen && isFrameAt(payload[win9xPadLen:]) {
		return payload[win9xPadLen], payload[win9xPadLen:], true
	}
	return 0, nil, false
}
