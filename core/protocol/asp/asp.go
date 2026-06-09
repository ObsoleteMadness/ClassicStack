// Package asp holds the AppleTalk Session Protocol (ASP) codec: SPFunction
// codes, error codes, version, the per-message packet types and their
// (un)marshallers, and ATP-derived size constants.
//
// ASP runs on top of ATP (TReq/TResp) and provides session-oriented
// client/server communication; AFP is its primary user. This package is
// wire-format only — no I/O, no goroutines, no state. The ASP server,
// session state machine, and tickle/attention timers live in the service ring.
//
// Ring: CORE (stdlib only, reflection-free).
//
// References:
//   - Inside AppleTalk, 2nd Edition, Chapter 11
//   - Inside Macintosh: Networking, Chapter 8
package asp

import "time"

// ---------------------------------------------------------------------------
// SPFunction codes — first byte (MSB) of ATP UserData in every ASP packet.
// Inside AppleTalk, 2nd Edition, Chapter 11, §"SPFunction values".
// ---------------------------------------------------------------------------

const (
	SPFuncCloseSess     = 1 // workstation → server
	SPFuncCommand       = 2 // workstation → server
	SPFuncGetStatus     = 3 // workstation → server
	SPFuncOpenSess      = 4 // workstation → server
	SPFuncTickle        = 5 // both directions
	SPFuncWrite         = 6 // workstation → server  (phase 1 of two-phase write)
	SPFuncWriteContinue = 7 // server → workstation  (phase 2: server requests write data)
	SPFuncAttention     = 8 // server → workstation
)

// Version is the ASP protocol version number carried in the OpenSess packet's
// 2-byte version field. Inside AppleTalk §"Opening a session".
const Version uint16 = 0x0100

// Timer values — §"Timeouts and retry counts" / §"Maintaining the session".
const (
	// TickleInterval is the period between keep-alive tickle packets (spec: 30 s).
	TickleInterval = 30 * time.Second
	// SessionMaintenanceTimeout is the inactivity duration after which a
	// session is assumed dead (spec: 2 minutes).
	SessionMaintenanceTimeout = 2 * time.Minute
)

// ASP error codes — Inside Macintosh: Networking, Chapter 8.
const (
	SPErrorNoError        = 0     // $00   — no error (both ends)
	SPErrorBadVersNum     = -1066 // $FBD6 — workstation end only
	SPErrorBufTooSmall    = -1067 // $FBD5 — workstation end only
	SPErrorNoMoreSessions = -1068 // $FBD4 — both ends
	SPErrorNoServers      = -1069 // $FBD3 — workstation end only
	SPErrorParamErr       = -1070 // $FBD2 — both ends
	SPErrorServerBusy     = -1071 // $FBD1 — workstation end only
	SPErrorSessClosed     = -1072 // $FBD0 — both ends
	SPErrorSizeErr        = -1073 // $FBCF — both ends
	SPErrorTooManyClients = -1074 // $FBCE — server end only
	SPErrorNoAck          = -1075 // $FBCD — server end only
)

// AspAttnServerGoingDown is the AFP attention word sent via SPFuncAttention
// when the server is shutting down; bit 15 is the "server is going down" flag.
const AspAttnServerGoingDown uint16 = 0x8000

// ATP-derived size constants.
const (
	// ATPMaxData is the maximum data payload per ATP response packet
	// (DDP max data 586 - 8-byte ATP header).
	ATPMaxData = 578
	// ATPMaxPackets is the maximum number of response packets in a single
	// ATP transaction (the bitmap has 8 bits).
	ATPMaxPackets = 8
	// QuantumSize is the maximum reply block (or SPWrtContinue write data) on a
	// standard AppleTalk network: 8 × 578 = 4624 bytes.
	QuantumSize = ATPMaxData * ATPMaxPackets
)

// GetParmsResult holds the values returned by an SPGetParms local call (no
// network packet). On a standard AppleTalk network MaxCmdSize = 578 and
// QuantumSize = 4624.
type GetParmsResult struct {
	MaxCmdSize  uint16
	QuantumSize uint16
}

// ===================================================================
// Packet types — one per SPFunction.
//
// UserData byte layout (MSB first, 4 bytes in the ATP header):
//
//	[0] SPFunction
//	[1] SessionID (or WSSSocket for OpenSess request)
//	[2:3] SeqNum / VersionNum / AttentionCode / 0
// ===================================================================

// OpenSessPacket represents an incoming ASP OpenSess request.
type OpenSessPacket struct {
	WSSSocket  uint8  // workstation session socket
	VersionNum uint16 // ASP version (expected: Version = 0x0100)
}

// ParseOpenSessPacket extracts fields from the ATP UserData of an OpenSess TReq.
func ParseOpenSessPacket(userData uint32) OpenSessPacket {
	return OpenSessPacket{
		WSSSocket:  uint8((userData >> 16) & 0xFF),
		VersionNum: uint16(userData & 0xFFFF),
	}
}

// OpenSessReplyPacket represents an outgoing ASP OpenSess reply.
type OpenSessReplyPacket struct {
	SSSSocket uint8 // server session socket
	SessionID uint8
	ErrorCode int16 // 0 = success; SPErrorBadVersNum, SPErrorServerBusy, SPErrorTooManyClients
}

// MarshalUserData encodes the reply into the 4-byte ATP UserData field:
// [0] SSSSocket [1] SessionID [2:3] ErrorCode (big-endian).
func (p OpenSessReplyPacket) MarshalUserData() uint32 {
	return (uint32(p.SSSSocket) << 24) |
		(uint32(p.SessionID) << 16) |
		uint32(uint16(p.ErrorCode))
}

// CloseSessPacket represents an incoming ASP CloseSess request.
type CloseSessPacket struct {
	SessionID uint8
}

// ParseCloseSessPacket extracts fields from the ATP UserData of a CloseSess TReq.
func ParseCloseSessPacket(userData uint32) CloseSessPacket {
	return CloseSessPacket{SessionID: uint8((userData >> 16) & 0xFF)}
}

// CloseSessReplyUserData returns the ATP UserData for a CloseSess reply (zero).
func CloseSessReplyUserData() uint32 { return 0 }

// GetStatusPacket represents an incoming ASP GetStatus request. UserData beyond
// the SPFunction is zero per spec.
type GetStatusPacket struct{}

// ParseGetStatusPacket is provided for completeness; UserData is unused.
func ParseGetStatusPacket(_ uint32) GetStatusPacket { return GetStatusPacket{} }

// CommandPacket represents an incoming ASP Command request.
type CommandPacket struct {
	SessionID uint8
	SeqNum    uint16
	CmdBlock  []byte // AFP command block (ATP data payload)
}

// ParseCommandPacket extracts fields from the ATP UserData and payload.
func ParseCommandPacket(userData uint32, payload []byte) CommandPacket {
	return CommandPacket{
		SessionID: uint8((userData >> 16) & 0xFF),
		SeqNum:    uint16(userData & 0xFFFF),
		CmdBlock:  payload,
	}
}

// WritePacket represents an incoming ASP Write request (same layout as Command).
type WritePacket struct {
	SessionID uint8
	SeqNum    uint16
	CmdBlock  []byte // AFP command block (e.g. FPWrite header)
}

// ParseWritePacket extracts fields from the ATP UserData and payload.
func ParseWritePacket(userData uint32, payload []byte) WritePacket {
	return WritePacket{
		SessionID: uint8((userData >> 16) & 0xFF),
		SeqNum:    uint16(userData & 0xFFFF),
		CmdBlock:  payload,
	}
}

// WriteContinuePacket represents an outgoing ASP WriteContinue request.
type WriteContinuePacket struct {
	SessionID  uint8
	SeqNum     uint16 // same sequence number as the original Write
	BufferSize uint16 // available buffer size (bytes the server wants)
}

// MarshalUserData encodes the WriteContinue into the 4-byte ATP UserData:
// [0] SPFuncWriteContinue [1] SessionID [2:3] SeqNum.
func (p WriteContinuePacket) MarshalUserData() uint32 {
	return (uint32(SPFuncWriteContinue) << 24) |
		(uint32(p.SessionID) << 16) |
		uint32(p.SeqNum)
}

// MarshalData returns the 2-byte ATP data payload (buffer size, big-endian).
func (p WriteContinuePacket) MarshalData() []byte {
	return []byte{byte(p.BufferSize >> 8), byte(p.BufferSize)}
}

// TicklePacket represents an outgoing ASP Tickle.
type TicklePacket struct {
	SessionID uint8
}

// MarshalUserData encodes the Tickle into the 4-byte ATP UserData:
// [0] SPFuncTickle [1] SessionID [2:3] 0.
func (p TicklePacket) MarshalUserData() uint32 {
	return (uint32(SPFuncTickle) << 24) | (uint32(p.SessionID) << 16)
}

// AttentionPacket represents an outgoing ASP Attention.
type AttentionPacket struct {
	SessionID     uint8
	AttentionCode uint16 // must be non-zero per spec
}

// MarshalUserData encodes the Attention into the 4-byte ATP UserData:
// [0] SPFuncAttention [1] SessionID [2:3] AttentionCode.
func (p AttentionPacket) MarshalUserData() uint32 {
	return (uint32(SPFuncAttention) << 24) |
		(uint32(p.SessionID) << 16) |
		uint32(p.AttentionCode)
}
