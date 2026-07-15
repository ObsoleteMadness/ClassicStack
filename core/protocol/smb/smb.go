// Package smb holds the SMB (CIFS / SMB1) message codec. M2 provides the
// 32-byte SMB1 header codec and the command / dialect / status constants; the
// per-command parameter and data blocks are decoded by the SMB service (M7),
// which builds on this header.
//
// Ring: CORE (stdlib only, reflection-free). SMB is little-endian on the wire;
// LE integer codecs come from core/binaryprimitives, because encoding/binary
// transitively imports reflect.
//
// Reference: [MS-CIFS] §2.2.3.1 (SMB Header).
package smb

import (
	"errors"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

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
	CommandQueryInformation2     = 0x23
	CommandLockingAndX           = 0x24
	CommandTransaction           = 0x25
	CommandTransactionSecondary  = 0x26
	CommandEcho                  = 0x2B
	CommandWriteAndClose         = 0x2C
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

// CommandName returns the mnemonic for an SMB1 command byte ("SMB_COM_NEGOTIATE"
// etc.), or "SMB_COM_0xNN" for an unrecognised command. It is a diagnostics helper
// for debug/trace logging — the dispatcher keys off the numeric const, not this.
func CommandName(cmd uint8) string {
	switch cmd {
	case CommandCreateDirectory:
		return "SMB_COM_CREATE_DIRECTORY"
	case CommandDeleteDirectory:
		return "SMB_COM_DELETE_DIRECTORY"
	case CommandOpen:
		return "SMB_COM_OPEN"
	case CommandCreate:
		return "SMB_COM_CREATE"
	case CommandClose:
		return "SMB_COM_CLOSE"
	case CommandFlush:
		return "SMB_COM_FLUSH"
	case CommandDelete:
		return "SMB_COM_DELETE"
	case CommandRename:
		return "SMB_COM_RENAME"
	case CommandQueryInformation:
		return "SMB_COM_QUERY_INFORMATION"
	case CommandSetInformation:
		return "SMB_COM_SET_INFORMATION"
	case CommandRead:
		return "SMB_COM_READ"
	case CommandWrite:
		return "SMB_COM_WRITE"
	case CommandCheckDirectory:
		return "SMB_COM_CHECK_DIRECTORY"
	case CommandSeek:
		return "SMB_COM_SEEK"
	case CommandReadMPX:
		return "SMB_COM_READ_MPX"
	case CommandWriteRaw:
		return "SMB_COM_WRITE_RAW"
	case CommandWriteMPX:
		return "SMB_COM_WRITE_MPX"
	case CommandWriteComplete:
		return "SMB_COM_WRITE_COMPLETE"
	case CommandSetInformation2:
		return "SMB_COM_SET_INFORMATION2"
	case CommandQueryInformation2:
		return "SMB_COM_QUERY_INFORMATION2"
	case CommandLockingAndX:
		return "SMB_COM_LOCKING_ANDX"
	case CommandTransaction:
		return "SMB_COM_TRANSACTION"
	case CommandTransactionSecondary:
		return "SMB_COM_TRANSACTION_SECONDARY"
	case CommandEcho:
		return "SMB_COM_ECHO"
	case CommandWriteAndClose:
		return "SMB_COM_WRITE_AND_CLOSE"
	case CommandOpenAndX:
		return "SMB_COM_OPEN_ANDX"
	case CommandReadAndX:
		return "SMB_COM_READ_ANDX"
	case CommandWriteAndX:
		return "SMB_COM_WRITE_ANDX"
	case CommandTransaction2:
		return "SMB_COM_TRANSACTION2"
	case CommandTransaction2Secondary:
		return "SMB_COM_TRANSACTION2_SECONDARY"
	case CommandFindClose2:
		return "SMB_COM_FIND_CLOSE2"
	case CommandTreeConnect:
		return "SMB_COM_TREE_CONNECT"
	case CommandTreeDisconnect:
		return "SMB_COM_TREE_DISCONNECT"
	case CommandNegotiate:
		return "SMB_COM_NEGOTIATE"
	case CommandSessionSetupAndX:
		return "SMB_COM_SESSION_SETUP_ANDX"
	case CommandLogoffAndX:
		return "SMB_COM_LOGOFF_ANDX"
	case CommandTreeConnectAndX:
		return "SMB_COM_TREE_CONNECT_ANDX"
	case CommandQueryInformationDisk:
		return "SMB_COM_QUERY_INFORMATION_DISK"
	case CommandSearch:
		return "SMB_COM_SEARCH"
	case CommandNtTransact:
		return "SMB_COM_NT_TRANSACT"
	case CommandNtTransactSecondary:
		return "SMB_COM_NT_TRANSACT_SECONDARY"
	case CommandNtCreateAndX:
		return "SMB_COM_NT_CREATE_ANDX"
	case CommandNtCancel:
		return "SMB_COM_NT_CANCEL"
	default:
		return "SMB_COM_0x" + hexByte(cmd)
	}
}

// hexByte formats a byte as two uppercase hex digits (avoids importing fmt/strconv
// in the core protocol ring for one diagnostics call).
func hexByte(b uint8) string {
	const digits = "0123456789ABCDEF"
	return string([]byte{digits[b>>4], digits[b&0x0F]})
}

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

// SMB dialect strings ([MS-CIFS] 2.2.4.52; [smb6.0] §"list of SMB protocol dialects").
// Ordered least→most functional. The NEGOTIATE response format is keyed by which of
// these the server selects (see DialectFamily): Core → WCT=1, any LANMAN 1.0..2.1 →
// WCT=13, NT LM 0.12 → WCT=17.
const (
	DialectPCNetwork1 = "PC NETWORK PROGRAM 1.0"      // the core protocol
	DialectMSNet103   = "MICROSOFT NETWORKS 1.03"     // MS-NET 1.03
	DialectMSNet30    = "MICROSOFT NETWORKS 3.0"      // DOS LANMAN 1.0
	DialectLANMAN10   = "LANMAN1.0"                   // LAN Manager 1.0
	DialectLM12X002   = "LM1.2X002"                   // LAN Manager 2.0
	DialectDOSLM12    = "DOS LM1.2X002"               // DOS LAN Manager 2.0
	DialectDOSLANMAN2 = "DOS LANMAN2.1"               // DOS LAN Manager 2.1
	DialectLANMAN21   = "LANMAN2.1"                   // OS/2 LAN Manager 2.1
	DialectWfW311     = "Windows for Workgroups 3.1a" // WfW
	DialectNTLM       = "NT LM 0.12"                  // NT LAN Manager
)

// DialectFamily groups the dialects by NEGOTIATE-response wire format ([MS-CIFS]
// 2.2.4.52.2: WordCount MUST match the selected dialect family).
type DialectFamily int

const (
	// DialectFamilyUnknown means none of the offered dialects were recognised; the
	// server answers with the core WCT=1 shape and DialectIndex 0xFFFF.
	DialectFamilyUnknown DialectFamily = iota
	DialectFamilyCore                  // PC NETWORK PROGRAM 1.0 (also MS-NET 1.03) → WCT=1
	DialectFamilyLanMan                // LANMAN 1.0 .. LANMAN 2.1 / WfW 3.1a → WCT=13
	DialectFamilyNT                    // NT LM 0.12 → WCT=17
)

// dialectFamily maps a dialect string to its response-format family. Anything not
// listed is DialectFamilyCore (the safe common-minimum WCT=1 shape).
func dialectFamily(name string) DialectFamily {
	switch name {
	case DialectNTLM:
		return DialectFamilyNT
	case DialectMSNet30, DialectLANMAN10, DialectLM12X002, DialectDOSLM12,
		DialectDOSLANMAN2, DialectLANMAN21, DialectWfW311:
		return DialectFamilyLanMan
	case DialectPCNetwork1, DialectMSNet103, DialectPCLAN10:
		return DialectFamilyCore
	default:
		return DialectFamilyCore
	}
}

// DialectPCLAN10 is an alternate spelling some MS-NET builds use for the core dialect.
const DialectPCLAN10 = "PCLAN1.0"

// dialectRank orders dialects by capability (higher = more recent/functional), so the
// server can select the most recent dialect the client offered ([smb6.0]: "SMB servers
// select the most recent version of the protocol known to both client and server").
// Unlisted strings rank 0 (below every known dialect but still selectable as core).
func dialectRank(name string) int {
	switch name {
	case DialectNTLM:
		return 100
	case DialectWfW311:
		return 90
	case DialectLANMAN21:
		return 80
	case DialectDOSLANMAN2:
		return 70
	case DialectDOSLM12:
		return 60
	case DialectLM12X002:
		return 50
	case DialectLANMAN10:
		return 40
	case DialectMSNet30:
		return 30
	case DialectMSNet103:
		return 20
	case DialectPCNetwork1, DialectPCLAN10:
		return 10
	default:
		return 0
	}
}

// SelectDialect chooses the most-recent dialect from the client's offered list (a slice
// of dialect strings in the order they appeared in the NEGOTIATE request) that this
// server supports, and returns its 0-based index, the dialect string, and its response
// family. If the list is empty or none is recognised it returns index 0xFFFF /
// DialectFamilyUnknown ([MS-CIFS] 2.2.4.52.2: DialectIndex 0xFFFF when nothing matches).
func SelectDialect(offered []string) (index uint16, name string, family DialectFamily) {
	bestRank := 0
	bestIdx := -1
	for i, d := range offered {
		if r := dialectRank(d); r > bestRank {
			bestRank = r
			bestIdx = i
		}
	}
	if bestIdx < 0 {
		return 0xFFFF, "", DialectFamilyUnknown
	}
	return uint16(bestIdx), offered[bestIdx], dialectFamily(offered[bestIdx])
}

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
	bp.PutLE32(b[offStatus:offStatus+4], h.Status)
	b[offFlags] = h.Flags
	bp.PutLE16(b[offFlags2:offFlags2+2], h.Flags2)
	bp.PutLE16(b[offPIDHigh:offPIDHigh+2], h.PIDHigh)
	copy(b[offSecurity:offSecurity+8], h.Security[:])
	bp.PutLE16(b[offReserved:offReserved+2], h.Reserved)
	bp.PutLE16(b[offTID:offTID+2], h.TID)
	bp.PutLE16(b[offPIDLow:offPIDLow+2], h.PIDLow)
	bp.PutLE16(b[offUID:offUID+2], h.UID)
	bp.PutLE16(b[offMID:offMID+2], h.MID)
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
	h.Status = bp.LE32(b[offStatus : offStatus+4])
	h.Flags = b[offFlags]
	h.Flags2 = bp.LE16(b[offFlags2 : offFlags2+2])
	h.PIDHigh = bp.LE16(b[offPIDHigh : offPIDHigh+2])
	copy(h.Security[:], b[offSecurity:offSecurity+8])
	h.Reserved = bp.LE16(b[offReserved : offReserved+2])
	h.TID = bp.LE16(b[offTID : offTID+2])
	h.PIDLow = bp.LE16(b[offPIDLow : offPIDLow+2])
	h.UID = bp.LE16(b[offUID : offUID+2])
	h.MID = bp.LE16(b[offMID : offMID+2])
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
