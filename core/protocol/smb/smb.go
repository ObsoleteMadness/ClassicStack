// Package smb holds the SMB (CIFS / SMB1) message codec. M2 provides the
// 32-byte SMB1 header codec and the command / dialect / status constants; the
// per-command parameter and data blocks are decoded by the SMB service (M7),
// which builds on this header.
//
// Ring: CORE (stdlib only, reflection-free). SMB is little-endian on the wire;
// LE helpers are hand-rolled because encoding/binary transitively imports
// reflect.
//
// Reference: [MS-CIFS] §2.2.3.1 (SMB Header).
package smb

import "errors"

// le16 / le32 / putLE16 / putLE32 are hand-rolled little-endian helpers (no
// encoding/binary in core — it transitively imports reflect, §1 / archtest).
func le16(b []byte) uint16 { return uint16(b[0]) | uint16(b[1])<<8 }

func le32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

func putLE16(b []byte, v uint16) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}

func putLE32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

// HeaderLen is the fixed SMB1 header length in bytes ([MS-CIFS] §2.2.3.1).
const HeaderLen = 32

// Protocol is the 4-byte SMB1 protocol identifier "\xffSMB".
var Protocol = [4]byte{0xFF, 'S', 'M', 'B'}

// Header field offsets within the 32-byte SMB1 header. The 8-byte
// SecurityFeatures field (offset 14) overlaps the legacy Key/CID/SequenceNumber
// layout; SequenceNumber sits at offset 20 ([MS-CIFS] §2.2.3.1).
const (
	offProtocol       = 0  // 4 bytes
	offCommand        = 4  // 1 byte
	offStatus         = 5  // 4 bytes (NTSTATUS or DOS error class/code)
	offFlags          = 9  // 1 byte
	offFlags2         = 10 // 2 bytes
	offPIDHigh        = 12 // 2 bytes
	offSecurity       = 14 // 8 bytes
	offSequenceNumber = 20 // 2 bytes (within SecurityFeatures)
	offReserved       = 22 // 2 bytes (must be zero on the wire)
	offTID            = 24 // 2 bytes
	offPIDLow         = 26 // 2 bytes
	offUID            = 28 // 2 bytes
	offMID            = 30 // 2 bytes
)

// SMB1 command codes ([MS-CIFS] §2.2.2.1).
const (
	CommandCreateDirectory       = 0x00
	CommandDeleteDirectory       = 0x01
	CommandOpen                  = 0x02
	CommandCreate                = 0x03
	CommandClose                 = 0x04
	CommandFlush                 = 0x05
	CommandDelete                = 0x06
	CommandRename                = 0x07
	CommandQueryInformation      = 0x08
	CommandSetInformation        = 0x09
	CommandRead                  = 0x0A
	CommandWrite                 = 0x0B
	CommandCheckDirectory        = 0x10
	CommandSeek                  = 0x12
	CommandReadMPX               = 0x1B
	CommandWriteRaw              = 0x1D
	CommandWriteMPX              = 0x1E
	CommandWriteComplete         = 0x20
	CommandSetInformation2       = 0x22
	CommandLockingAndX           = 0x24
	CommandTransaction           = 0x25
	CommandTransactionSecondary  = 0x26
	CommandEcho                  = 0x2B
	CommandOpenAndX              = 0x2D
	CommandReadAndX              = 0x2E
	CommandWriteAndX             = 0x2F
	CommandTransaction2          = 0x32
	CommandTransaction2Secondary = 0x33
	CommandFindClose2            = 0x34
	CommandTreeConnect           = 0x70
	CommandTreeDisconnect        = 0x71
	CommandNegotiate             = 0x72
	CommandSessionSetupAndX      = 0x73
	CommandLogoffAndX            = 0x74
	CommandTreeConnectAndX       = 0x75
	CommandQueryInformationDisk  = 0x80
	CommandSearch                = 0x81
	CommandNtTransact            = 0xA0
	CommandNtTransactSecondary   = 0xA1
	CommandNtCreateAndX          = 0xA2
	CommandNtCancel              = 0xA4
	CommandNoAndXCommand         = 0xFF // AndX terminator
)

// Flags (offset 9) bits ([MS-CIFS] §2.2.3.1).
const (
	FlagReply = 0x80 // SMB_FLAGS_REPLY: message is a server response
)

// Flags2 (offset 10) bits.
const (
	Flags2KnowsLongNames uint16 = 0x0001 // SMB_FLAGS2_KNOWS_LONG_NAMES
	Flags2Unicode        uint16 = 0x8000 // SMB_FLAGS2_UNICODE
	Flags2NTStatus       uint16 = 0x4000 // SMB_FLAGS2_NT_STATUS
)

// DialectNTLM is the NT LM 0.12 dialect string negotiated in SMB_COM_NEGOTIATE.
const DialectNTLM = "NT LM 0.12"

// Common NTSTATUS / DOS status values used by the codec's callers.
const (
	StatusSuccess uint32 = 0x00000000
)

// ErrShort is returned by DecodeHeader when the buffer is shorter than a header.
var ErrShort = errors.New("smb: buffer shorter than SMB header")

// ErrBadProtocol is returned by DecodeHeader when the "\xffSMB" magic is absent.
var ErrBadProtocol = errors.New("smb: missing \\xffSMB protocol identifier")

// Header is the decoded 32-byte SMB1 header.
type Header struct {
	Command  uint8
	Status   uint32
	Flags    uint8
	Flags2   uint16
	PIDHigh  uint16
	Security [8]byte // SecurityFeatures (signature, or Key/CID/SequenceNumber)
	Reserved uint16  // must be zero on the wire; round-tripped for fidelity
	TID      uint16
	PIDLow   uint16
	UID      uint16
	MID      uint16
}

// Encode appends the 32-byte SMB1 header to dst and returns it (append-style →
// caller controls allocation). The protocol identifier is always "\xffSMB".
func (h Header) Encode(dst []byte) []byte {
	var b [HeaderLen]byte
	copy(b[offProtocol:offProtocol+4], Protocol[:])
	b[offCommand] = h.Command
	putLE32(b[offStatus:offStatus+4], h.Status)
	b[offFlags] = h.Flags
	putLE16(b[offFlags2:offFlags2+2], h.Flags2)
	putLE16(b[offPIDHigh:offPIDHigh+2], h.PIDHigh)
	copy(b[offSecurity:offSecurity+8], h.Security[:])
	putLE16(b[offReserved:offReserved+2], h.Reserved)
	putLE16(b[offTID:offTID+2], h.TID)
	putLE16(b[offPIDLow:offPIDLow+2], h.PIDLow)
	putLE16(b[offUID:offUID+2], h.UID)
	putLE16(b[offMID:offMID+2], h.MID)
	return append(dst, b[:]...)
}

// DecodeHeader parses the 32-byte SMB1 header from the front of b. It returns
// ErrShort if b is shorter than HeaderLen, and ErrBadProtocol if the "\xffSMB"
// identifier is absent. Any message body follows at b[HeaderLen:].
func DecodeHeader(b []byte) (Header, error) {
	if len(b) < HeaderLen {
		return Header{}, ErrShort
	}
	if b[0] != Protocol[0] || b[1] != Protocol[1] || b[2] != Protocol[2] || b[3] != Protocol[3] {
		return Header{}, ErrBadProtocol
	}
	var h Header
	h.Command = b[offCommand]
	h.Status = le32(b[offStatus : offStatus+4])
	h.Flags = b[offFlags]
	h.Flags2 = le16(b[offFlags2 : offFlags2+2])
	h.PIDHigh = le16(b[offPIDHigh : offPIDHigh+2])
	copy(h.Security[:], b[offSecurity:offSecurity+8])
	h.Reserved = le16(b[offReserved : offReserved+2])
	h.TID = le16(b[offTID : offTID+2])
	h.PIDLow = le16(b[offPIDLow : offPIDLow+2])
	h.UID = le16(b[offUID : offUID+2])
	h.MID = le16(b[offMID : offMID+2])
	return h, nil
}

// SequenceNumber returns the 2-byte SequenceNumber field (offset 20, within
// SecurityFeatures), used by multiplexed SMB_COM_WRITE_MPX sequences.
func (h Header) SequenceNumber() uint16 {
	return uint16(h.Security[offSequenceNumber-offSecurity]) |
		uint16(h.Security[offSequenceNumber-offSecurity+1])<<8
}

// IsResponse reports whether the SMB_FLAGS_REPLY bit is set.
func (h Header) IsResponse() bool { return h.Flags&FlagReply != 0 }
