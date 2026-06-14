// Package browser is the wire codec for the Microsoft NetBIOS browser protocol
// ([MS-BRWS]): the \MAILSLOT\BROWSE frames a browser server and clients exchange
// — host/domain/local-master announcements, the master-browser election, and the
// GetBackupList request/response. Each frame is a self-serialising DTO (Marshal /
// Unmarshal, per the project DTO rule) so callers never decode bytes inline.
//
// The frames ride inside an SMB_COM_TRANSACTION mailslot write addressed to a
// browser group name (WORKGROUP<1D>/<1E>); the MailslotTransaction DTO wraps that
// SMB transaction envelope. The browser SERVICE (core/service/browser) is the
// state machine over these codecs; this package holds no state.
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

// Mailslot names browser traffic is written to.
const (
	MailslotBrowse = "\\MAILSLOT\\BROWSE"
	MailslotLANMAN = "\\MAILSLOT\\LANMAN"
)

// Server-type bits ([MS-BRWS] §2.2 SV_TYPE_*), used in announcements and the
// browse-list / backup-list filtering.
const (
	ServerTypeWorkstation    uint32 = 0x00000001
	ServerTypeServer         uint32 = 0x00000002
	ServerTypeWorkstationSet uint32 = 0x00402003 // the type ClassicStack announces
	ServerTypeBackupBrowser  uint32 = 0x00020000
	ServerTypeMasterBrowser  uint32 = 0x00040000
	ServerTypeDomainEnum     uint32 = 0x80000000
)

// Election criteria + the browser/OS version bytes ClassicStack advertises.
const (
	ElectionCriteriaMaster uint32 = 0x00000004
	ElectionVersion        uint8  = 0x01
	BrowserVersionMajor    uint8  = 0x0F
	BrowserVersionMinor    uint8  = 0x01
	AnnounceVersionMajor   uint8  = 0x15
	AnnounceVersionMinor   uint8  = 0x04
	Signature              uint16 = 0xAA55
)

// NameTypeMasterBrowser is the NetBIOS suffix (<1D>) of the local-master-browser
// name a GetBackupList response sources from.
const NameTypeMasterBrowser uint8 = 0x1D

// errors returned by the Unmarshal methods.
var (
	ErrShort    = errors.New("browser: frame too short")
	ErrBadOp    = errors.New("browser: wrong opcode for frame type")
	ErrEnvelope = errors.New("browser: invalid mailslot transaction envelope")
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

// UnwrapPayload extracts the browser opcode + frame from a mailslot payload,
// tolerating the two-byte Win9x preamble (e.g. 01 03 / 0f 06) some clients prefix
// before the opcode. Returns the opcode, the frame starting at the opcode, and ok.
func UnwrapPayload(payload []byte) (op uint8, frame []byte, ok bool) {
	if len(payload) == 0 {
		return 0, nil, false
	}
	if len(payload) >= 3 && IsCommandByte(payload[2]) {
		if (payload[0] == 0x01 && payload[1] == 0x03) || (payload[0] == 0x0f && payload[1] == 0x06) {
			return payload[2], payload[2:], true
		}
	}
	if IsCommandByte(payload[0]) {
		return payload[0], payload, true
	}
	if len(payload) >= 3 && IsCommandByte(payload[2]) {
		return payload[2], payload[2:], true
	}
	return 0, nil, false
}
