package asp

// client.go adds the CLIENT-direction ASP codecs — the mirror half of asp.go, which
// carries the server direction (Parse* for client→server requests, Marshal* for
// server→client replies). A workstation (our AFP client) needs the opposite: marshal
// the request UserData it sends, and parse the reply UserData/data it receives.
//
// UserData byte layout (MSB first, the 4-byte ATP UserData) is identical to asp.go:
//   [0] SPFunction  [1] SessionID (or WSSSocket for OpenSess)  [2:3] SeqNum/Version/...
//
// Ring: CORE (wire-format only).

// MarshalUserData encodes an OpenSess REQUEST into the 4-byte ATP UserData a
// workstation sends: [0] SPFuncOpenSess [1] WSSSocket [2:3] VersionNum. This is the
// mirror of ParseOpenSessPacket.
func (p OpenSessPacket) MarshalUserData() uint32 {
	return (uint32(SPFuncOpenSess) << 24) |
		(uint32(p.WSSSocket) << 16) |
		uint32(p.VersionNum)
}

// ParseOpenSessReply decodes the OpenSess REPLY UserData a workstation receives:
// [0] SSSSocket [1] SessionID [2:3] ErrorCode. Mirror of
// OpenSessReplyPacket.MarshalUserData.
func ParseOpenSessReply(userData uint32) OpenSessReplyPacket {
	return OpenSessReplyPacket{
		SSSSocket: uint8((userData >> 24) & 0xFF),
		SessionID: uint8((userData >> 16) & 0xFF),
		ErrorCode: int16(userData & 0xFFFF),
	}
}

// MarshalUserData encodes a Command REQUEST into the 4-byte ATP UserData a workstation
// sends: [0] SPFuncCommand [1] SessionID [2:3] SeqNum. The command block travels as the
// ATP data payload. Mirror of ParseCommandPacket.
func (p CommandPacket) MarshalUserData() uint32 {
	return (uint32(SPFuncCommand) << 24) |
		(uint32(p.SessionID) << 16) |
		uint32(p.SeqNum)
}

// MarshalUserData encodes a Write REQUEST (phase 1) into the 4-byte ATP UserData:
// [0] SPFuncWrite [1] SessionID [2:3] SeqNum. Mirror of ParseWritePacket.
func (p WritePacket) MarshalUserData() uint32 {
	return (uint32(SPFuncWrite) << 24) |
		(uint32(p.SessionID) << 16) |
		uint32(p.SeqNum)
}

// MarshalUserData encodes a CloseSess REQUEST a workstation sends to end its own
// session: [0] SPFuncCloseSess [1] SessionID [2:3] 0. (The existing
// CloseSessPacket.MarshalUserData in asp.go is the SERVER-initiated close, which has
// the same wire shape; this method name-collides with it, so the request form is a
// free function instead.)
func MarshalCloseSessRequest(sessionID uint8) uint32 {
	return (uint32(SPFuncCloseSess) << 24) | (uint32(sessionID) << 16)
}

// MarshalGetStatusRequest encodes an ASPGetStatus REQUEST: [0] SPFuncGetStatus, rest 0.
// GetStatus carries no session (it precedes OpenSession) and no data.
func MarshalGetStatusRequest() uint32 {
	return uint32(SPFuncGetStatus) << 24
}

// MarshalTickleRequest encodes a Tickle a workstation sends to keep its session alive:
// [0] SPFuncTickle [1] SessionID [2:3] 0. (TicklePacket.MarshalUserData in asp.go is
// identical; provided as a free function for symmetry with the other request builders.)
func MarshalTickleRequest(sessionID uint8) uint32 {
	return (uint32(SPFuncTickle) << 24) | (uint32(sessionID) << 16)
}

// AttentionInfo is the parsed form of a server→workstation Attention: the SPFunction
// (should be SPFuncAttention), the session id, and the 16-bit attention code (whose
// bits are the AspAttn* set in asp.go).
type AttentionInfo struct {
	SessionID     uint8
	AttentionCode uint16
}

// ParseAttention decodes an Attention UserData a workstation receives:
// [0] SPFuncAttention [1] SessionID [2:3] AttentionCode. Mirror of
// AttentionPacket.MarshalUserData. ok is false when the function byte is not
// SPFuncAttention.
func ParseAttention(userData uint32) (AttentionInfo, bool) {
	if uint8(userData>>24) != SPFuncAttention {
		return AttentionInfo{}, false
	}
	return AttentionInfo{
		SessionID:     uint8((userData >> 16) & 0xFF),
		AttentionCode: uint16(userData & 0xFFFF),
	}, true
}

// ParseWriteContinue decodes a WriteContinue REQUEST a workstation receives during a
// two-phase Write: [0] SPFuncWriteContinue [1] SessionID [2:3] SeqNum, with the
// server's buffer size in the 2-byte ATP data payload. Mirror of the server's
// WriteContinuePacket.Marshal*. ok is false on a malformed packet.
func ParseWriteContinue(userData uint32, data []byte) (WriteContinuePacket, bool) {
	if uint8(userData>>24) != SPFuncWriteContinue {
		return WriteContinuePacket{}, false
	}
	p := WriteContinuePacket{
		SessionID: uint8((userData >> 16) & 0xFF),
		SeqNum:    uint16(userData & 0xFFFF),
	}
	if len(data) >= 2 {
		p.BufferSize = uint16(data[0])<<8 | uint16(data[1])
	}
	return p, true
}
