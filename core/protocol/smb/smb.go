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
	FlagCaseInsensitive   = 0x08 // SMB_FLAGS_CASE_INSENSITIVE: paths are case-insensitive
	FlagCanonicalizePaths = 0x10 // SMB_FLAGS_CANONICALIZED_PATHS: paths are in canonical form
	FlagReply             = 0x80 // SMB_FLAGS_REPLY: message is a server response
)

// FlagsRequest is the Flags byte a client request carries: canonicalized, case-insensitive
// paths — what every DOS/Windows redirector sets (0x18). Ground truth captures/nt-98-nbf.pcap
// frame 217: the MS redirector's SESSION_SETUP has Flags 0x18; our Flags 0x00 was one of the
// header differences a strict Win98 server did not answer.
const FlagsRequest = FlagCaseInsensitive | FlagCanonicalizePaths

// Flags2 (offset 10) bits.
const (
	Flags2KnowsLongNames uint16 = 0x0001 // SMB_FLAGS2_KNOWS_LONG_NAMES
	Flags2EAS            uint16 = 0x0002 // SMB_FLAGS2_EAS: extended attributes supported
	Flags2Unicode        uint16 = 0x8000 // SMB_FLAGS2_UNICODE
	Flags2NTStatus       uint16 = 0x4000 // SMB_FLAGS2_NT_STATUS
)

// NEGOTIATE SecurityMode bits ([MS-CIFS] §2.2.4.52.2). NT dialect uses an 8-bit
// field; LANMAN uses 16-bit. Bit 0/1 are the ones this client surfaces.
const (
	SecurityModeUser    uint16 = 0x0001 // NEGOTIATE_USER_SECURITY (else share-level)
	SecurityModeEncrypt uint16 = 0x0002 // NEGOTIATE_ENCRYPT_PASSWORDS (else plaintext)
)

// NEGOTIATE/SESSION_SETUP Capabilities bits ([MS-CIFS] §2.2.4.52.2 SMB_CAP_*).
// CapUnicode / CapLargeFiles / CapNTSMBs / CapNTStatus / CapNTFind are the ones
// this stack reasons about; the rest are named so a client can display what the
// server advertised.
const (
	CapRawMode           uint32 = 0x00000001 // CAP_RAW_MODE
	CapMPXMode           uint32 = 0x00000002 // CAP_MPX_MODE
	CapUnicode           uint32 = 0x00000004 // CAP_UNICODE: server/client speak UTF-16LE strings
	CapLargeFiles        uint32 = 0x00000008 // CAP_LARGE_FILES: 64-bit file offsets
	CapNTSMBs            uint32 = 0x00000010 // CAP_NT_SMBS: the NT-family request set
	CapRPCRemoteAPIs     uint32 = 0x00000020 // CAP_RPC_REMOTE_APIS
	CapNTStatus          uint32 = 0x00000040 // CAP_STATUS32: 32-bit NTSTATUS in headers (else DOS codes)
	CapLevelIIOplocks    uint32 = 0x00000080 // CAP_LEVEL_II_OPLOCKS
	CapLockAndRead       uint32 = 0x00000100 // CAP_LOCK_AND_READ
	CapNTFind            uint32 = 0x00000200 // CAP_NT_FIND: TRANS2 FIND_FIRST2/FIND_NEXT2
	CapDFS               uint32 = 0x00001000 // CAP_DFS
	CapInfoLevelPassthru uint32 = 0x00002000 // CAP_INFOLEVEL_PASSTHRU
	CapLargeReadX        uint32 = 0x00004000 // CAP_LARGE_READX
	CapLargeWriteX       uint32 = 0x00008000 // CAP_LARGE_WRITEX
	CapUnix              uint32 = 0x00800000 // CAP_UNIX
	CapExtendedSecurity  uint32 = 0x80000000 // CAP_EXTENDED_SECURITY
)

// capabilityNames is CAP_* bit → short display name, in [MS-CIFS] bit order.
var capabilityNames = []struct {
	bit  uint32
	name string
}{
	{CapRawMode, "Raw mode"},
	{CapMPXMode, "MPX mode"},
	{CapUnicode, "Unicode"},
	{CapLargeFiles, "Large files"},
	{CapNTSMBs, "NT SMBs"},
	{CapRPCRemoteAPIs, "RPC APIs"},
	{CapNTStatus, "NT status"},
	{CapLevelIIOplocks, "Level II oplocks"},
	{CapLockAndRead, "Lock and read"},
	{CapNTFind, "NT Find"},
	{CapDFS, "DFS"},
	{CapInfoLevelPassthru, "Info-level passthru"},
	{CapLargeReadX, "Large ReadX"},
	{CapLargeWriteX, "Large WriteX"},
	{CapUnix, "UNIX extensions"},
	{CapExtendedSecurity, "Extended security"},
}

// CapabilityNames returns the CAP_* flags set in caps as short display names
// ([MS-CIFS] §2.2.4.52.2). Unknown bits are omitted.
func CapabilityNames(caps uint32) []string {
	if caps == 0 {
		return nil
	}
	out := make([]string, 0, 8)
	for _, c := range capabilityNames {
		if caps&c.bit != 0 {
			out = append(out, c.name)
		}
	}
	return out
}

// SMB dialect strings ([MS-CIFS] 2.2.4.52; [smb6.0] §"list of SMB protocol dialects").
// Ordered least→most functional. The NEGOTIATE response format is keyed by which of
// these the server selects (see DialectFamily): Core → WCT=1, any LANMAN 1.0..2.1 →
// WCT=13, NT LM 0.12 → WCT=17.
const (
	DialectPCNetwork1 = "PC NETWORK PROGRAM 1.0"      // the core protocol
	DialectXenixCore  = "XENIX CORE"                  // core protocol, XENIX flavour
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
	case DialectPCNetwork1, DialectXenixCore, DialectMSNet103, DialectPCLAN10:
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
	case DialectXenixCore:
		// Core family, ranked just above PC NETWORK PROGRAM 1.0. A real OS/2 LAN
		// Requester offers it second in its list — golden capture
		// spec/captures/nbf-os2-win98.pcap frame 100: PC NETWORK PROGRAM 1.0,
		// XENIX CORE, LANMAN1.0, LM1.2X002, LANMAN2.1. Without a rank it scored 0 and
		// was never selectable, so an OS/2 client offering ONLY the two core dialects
		// would have been answered DialectIndex 0xFFFF ("nothing in common").
		return 15
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

// --- raw-message header accessors ---
//
// A transport frequently needs two or three header fields out of a message it is
// only relaying (the command byte to spot NEGOTIATE/ECHO, the FLAGS reply bit to
// tell a request from a response, the MID to correlate a reply with the request in
// flight) and has no reason to decode the whole Header. Both the direct-hosted-IPX
// CLIENT (client/smb/ipx.go) and the SERVER's DirectIPX (core/service/smb/
// directipx.go) did exactly that, each against its OWN private copy of the offsets
// — the same drift that left SequenceNumber unwritten on the client. These
// accessors are the single definition; a message shorter than the field reads 0
// (or false), so a truncated buffer never panics.

// WordCountOffset is the offset of the WordCount (WCT) byte: immediately after the
// 32-byte header, i.e. the first body byte ([MS-CIFS] §2.2.3.2).
const WordCountOffset = HeaderLen

// HasProtocolID reports whether msg starts with the "\xffSMB" protocol identifier
// and is at least a whole header long — the "is this an SMB message at all" test a
// datagram transport applies before dispatching.
func HasProtocolID(msg []byte) bool {
	if len(msg) < HeaderLen {
		return false
	}
	return msg[0] == Protocol[0] && msg[1] == Protocol[1] && msg[2] == Protocol[2] && msg[3] == Protocol[3]
}

// MessageCommand returns the Command byte (offset 4) of a raw SMB message.
func MessageCommand(msg []byte) uint8 {
	if len(msg) <= offCommand {
		return 0
	}
	return msg[offCommand]
}

// MessageStatus returns the Status field (offset 5, NTSTATUS or DOS class/code) of
// a raw SMB message.
func MessageStatus(msg []byte) uint32 {
	if len(msg) < offStatus+4 {
		return 0
	}
	return bp.LE32(msg[offStatus : offStatus+4])
}

// MessageFlags returns the Flags byte (offset 9) of a raw SMB message.
func MessageFlags(msg []byte) uint8 {
	if len(msg) <= offFlags {
		return 0
	}
	return msg[offFlags]
}

// IsResponseMessage reports whether a raw SMB message carries SMB_FLAGS_REPLY —
// i.e. it is a server response rather than a client request.
func IsResponseMessage(msg []byte) bool {
	return MessageFlags(msg)&FlagReply != 0
}

// MessageMID returns the MID (multiplex id, offset 30) of a raw SMB message. A
// connectionless transport correlates a response to the request in flight by
// (Command, MID).
func MessageMID(msg []byte) uint16 {
	if len(msg) < offMID+2 {
		return 0
	}
	return bp.LE16(msg[offMID : offMID+2])
}

// --- connectionless (direct-hosted IPX) header helpers ---
//
// On a connectionless transport the 8-byte SecurityFeatures field is NOT a signature:
// it carries Key(4) | CID(2) | SequenceNumber(2) ([MS-CIFS] §2.2.3.1). Both the
// direct-hosted-IPX client (client/smb/ipx.go) and the server's DirectIPX
// (core/service/smb/directipx.go) read and write those two words, so the accessors
// live HERE rather than being hand-poked at literal byte offsets on each side — the
// two used to keep private copies of the offsets and drifted (the client never wrote
// SequenceNumber at all).
const (
	// ConnectionlessCIDOffset is the CID word's offset in the SMB header.
	ConnectionlessCIDOffset = offSecurity + 4 // 18
	// ConnectionlessSeqOffset is the SequenceNumber word's offset.
	ConnectionlessSeqOffset = offSequenceNumber // 20
)

// ConnectionlessCIDReserved is the reserved high Connection ID (0xFFFF). Together with
// 0x0000 it bookends the allocatable range: the server allocates from 1 and wraps
// before this value, and neither end treats a reserved CID a peer echoed as a real
// circuit id. Both sides kept their own copy (the server's cidReservedHi, a bare
// literal on the client).
const ConnectionlessCIDReserved uint16 = 0xFFFF

// FirstSequenceNumber is the SequenceNumber a client puts on its FIRST connectionless
// request. ERRATA: it is 1, not 0 — golden capture spec/captures/nwlink-win98.pcap
// frame 16 (a real NWLink redirector's NEGOTIATE) carries SequenceNumber 1 with CID 0,
// and it increments per request from there.
const FirstSequenceNumber uint16 = 1

// StampConnectionless writes the CID and SequenceNumber words into an SMB message's
// SecurityFeatures field. A message shorter than the header is left untouched.
func StampConnectionless(msg []byte, cid, seq uint16) {
	if len(msg) < HeaderLen {
		return
	}
	msg[ConnectionlessCIDOffset] = byte(cid)
	msg[ConnectionlessCIDOffset+1] = byte(cid >> 8)
	msg[ConnectionlessSeqOffset] = byte(seq)
	msg[ConnectionlessSeqOffset+1] = byte(seq >> 8)
}

// ConnectionlessCID reads the CID word from an SMB message (0 when too short).
func ConnectionlessCID(msg []byte) uint16 {
	if len(msg) < HeaderLen {
		return 0
	}
	return uint16(msg[ConnectionlessCIDOffset]) | uint16(msg[ConnectionlessCIDOffset+1])<<8
}

// ConnectionlessSequence reads the SequenceNumber word from an SMB message (0 when
// too short).
func ConnectionlessSequence(msg []byte) uint16 {
	if len(msg) < HeaderLen {
		return 0
	}
	return uint16(msg[ConnectionlessSeqOffset]) | uint16(msg[ConnectionlessSeqOffset+1])<<8
}

// NameTrailerLen is the length of the direct-hosted-IPX NEGOTIATE name trailer: two
// 16-byte NetBIOS names (core/protocol/netbios.NameLength each, restated here as a
// plain length so this package stays free of a netbios import).
const NameTrailerLen = 2 * 16

// AppendNameTrailer appends the direct-hosted-SMB-over-IPX NEGOTIATE name trailer —
// [SOURCE][DESTINATION], 16 bytes each — to an SMB_COM_NEGOTIATE message.
//
// ERRATA. Direct-hosted SMB over IPX has NO NetBIOS session layer, so nothing before
// NEGOTIATE ever names the machine being addressed; the names ride in the NEGOTIATE
// datagram itself, AFTER the SMB message and OUTSIDE ByteCount. Golden capture
// spec/captures/nwlink-win98.pcap frame 16: BCC is 0x0077 = 119 and covers only the
// dialect list, ending at the NUL after "NT LM 0.12", yet the IPX datagram runs 32
// bytes further and carries "WIN98-IPX-1    \x00" (the source, NameTypeWorkstation)
// followed by "WIN98-IPX-2    \x20" (the destination, NameTypeFileServer). The trailer
// is on NEGOTIATE ONLY — golden frames 18/20/22/24 (SESSION_SETUP+TREE_CONNECT,
// TRANS, ECHO, TREE_DISCONNECT) all end at their byte area, because by then the
// server-assigned CID identifies the circuit.
//
// Name order is [SOURCE][DESTINATION], the same order as the NBIPX SESSION_INITIALIZE
// name pair. Without the trailer a Win98 direct-hosted server answers NEGOTIATE with
// ERRSRV/18 — it has no way to tell which of its names the datagram is for.
func AppendNameTrailer(msg []byte, source, destination [16]byte) []byte {
	out := append(msg, source[:]...)
	return append(out, destination[:]...)
}

// SplitNameTrailer splits a direct-hosted-IPX NEGOTIATE datagram into the SMB message
// and the [SOURCE][DESTINATION] names the sender appended (see AppendNameTrailer). It
// reports false when the datagram carries no trailer, in which case msg is returned
// unchanged — a peer that omits it still gets its NEGOTIATE parsed.
func SplitNameTrailer(datagram []byte) (msg []byte, source, destination [16]byte, ok bool) {
	// The trailer starts where the SMB message ends, which WCT and BCC give exactly:
	// header + WordCount byte + words + ByteCount word + byte area. Trusting the
	// datagram length instead would mistake a long byte area for a trailer.
	if len(datagram) < HeaderLen+1 {
		return datagram, source, destination, false
	}
	bccOff := HeaderLen + 1 + 2*int(datagram[HeaderLen])
	if len(datagram) < bccOff+2 {
		return datagram, source, destination, false
	}
	end := bccOff + 2 + int(bp.LE16(datagram[bccOff:bccOff+2]))
	if end > len(datagram) || len(datagram)-end < NameTrailerLen {
		return datagram, source, destination, false
	}
	copy(source[:], datagram[end:end+16])
	copy(destination[:], datagram[end+16:end+NameTrailerLen])
	return datagram[:end], source, destination, true
}
