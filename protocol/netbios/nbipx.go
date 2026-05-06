package netbios

import (
	"encoding/binary"
	"errors"
)

// NetBIOS-over-IPX uses two IPX packet types depending on the
// purpose:
//
//   - IPX type 20 ("NetBIOS broadcast / forwarding") for name
//     service operations: name claim, name query, name in conflict.
//     Travels broadcast and may traverse up to 8 routers.
//
//   - IPX type 4 ("Packet Exchange Protocol") on socket 0x0455 for
//     session-layer traffic: session establishment, data, teardown.
//     Carries the 16-byte NB-IPX session header below.
//
// The session-header constants and the name-service packet shape are
// the same on the wire whether the sender is OS/2 LAN Server, Win95,
// or NetWare-based. The codec below captures the agreed-upon layout.

// IPXTypeNetBIOS is the IPX packet-type code (0x14 = 20) used for
// NetBIOS-over-IPX broadcast forwarding (name claim / query).
const IPXTypeNetBIOS uint8 = 0x14

// IPXTypePEP is the IPX packet-type code (0x04) used for the NB-IPX
// session protocol on socket 0x0455.
const IPXTypePEP uint8 = 0x04

// NB-IPX session header: data_stream_type values seen on the wire.
// Each names what the packet means at the session layer; the
// per-flag connection-control byte refines behaviour (ACK, EOM, ...).
const (
	NBIPXFindName         uint8 = 0x01 // name service request
	NBIPXNameRecognized   uint8 = 0x02 // name service reply (positive)
	NBIPXCheckName        uint8 = 0x03
	NBIPXNameInUse        uint8 = 0x04
	NBIPXDeregisterName   uint8 = 0x05
	NBIPXSessionInit      uint8 = 0x05
	NBIPXSessionConfirm   uint8 = 0x06
	NBIPXSessionEnd       uint8 = 0x07
	NBIPXSessionEndAck    uint8 = 0x08
	NBIPXStatusQuery      uint8 = 0x09
	NBIPXStatusResponse   uint8 = 0x0A
	NBIPXDirectedDatagram uint8 = 0x0B
	NBIPXDataAck          uint8 = 0x14
	NBIPXDataOnlyLast     uint8 = 0x15
	NBIPXDataFirstMiddle  uint8 = 0x16
)

// NB-IPX session header: connection-control flag bits (high nibble
// of conn_ctrl_flag).
const (
	NBIPXConnFlagSYS uint8 = 0x80 // system packet
	NBIPXConnFlagACK uint8 = 0x40 // requesting an ACK
	NBIPXConnFlagATT uint8 = 0x20 // attention
	NBIPXConnFlagEOM uint8 = 0x10 // end of message
)

// NBIPXSessionHeader is the 16-byte session header that prefixes
// every NB-IPX session-family payload (everything carried over IPX
// type 4 on socket 0x0455).
type NBIPXSessionHeader struct {
	ConnCtrlFlag   uint8 // SYS|ACK|ATT|EOM bitfield
	DataStreamType uint8 // NBIPXFindName, NBIPXSessionInit, ...
	SourceConnID   uint16
	DestConnID     uint16
	SendSeq        uint16
	TotalDataLen   uint16
	Offset         uint16
	DataLen        uint16
	ConnCtrlByte   uint8
	Reserved       uint8
}

// NBIPXSessionHeaderLen is the wire length of NBIPXSessionHeader.
const NBIPXSessionHeaderLen = 16

// EncodeSessionHeader serialises an NB-IPX session header. The header
// is followed by DataLen bytes of payload, but encoding the payload
// is the caller's job — callers typically build a single buffer
// `[header || payload]` so they can write it as one IPX datagram body.
func EncodeSessionHeader(h *NBIPXSessionHeader) []byte {
	out := make([]byte, NBIPXSessionHeaderLen)
	out[0] = h.ConnCtrlFlag
	out[1] = h.DataStreamType
	binary.BigEndian.PutUint16(out[2:4], h.SourceConnID)
	binary.BigEndian.PutUint16(out[4:6], h.DestConnID)
	binary.BigEndian.PutUint16(out[6:8], h.SendSeq)
	binary.BigEndian.PutUint16(out[8:10], h.TotalDataLen)
	binary.BigEndian.PutUint16(out[10:12], h.Offset)
	binary.BigEndian.PutUint16(out[12:14], h.DataLen)
	out[14] = h.ConnCtrlByte
	out[15] = h.Reserved
	return out
}

// DecodeSessionHeader parses the first 16 bytes of an NB-IPX session
// payload. Returns ErrShortNBIPX when input is shorter than the
// header.
func DecodeSessionHeader(b []byte) (*NBIPXSessionHeader, error) {
	if len(b) < NBIPXSessionHeaderLen {
		return nil, ErrShortNBIPX
	}
	return &NBIPXSessionHeader{
		ConnCtrlFlag:   b[0],
		DataStreamType: b[1],
		SourceConnID:   binary.BigEndian.Uint16(b[2:4]),
		DestConnID:     binary.BigEndian.Uint16(b[4:6]),
		SendSeq:        binary.BigEndian.Uint16(b[6:8]),
		TotalDataLen:   binary.BigEndian.Uint16(b[8:10]),
		Offset:         binary.BigEndian.Uint16(b[10:12]),
		DataLen:        binary.BigEndian.Uint16(b[12:14]),
		ConnCtrlByte:   b[14],
		Reserved:       b[15],
	}, nil
}

const (
	NBIPXWANRouterCount       = 8
	NBIPXWANRouterBytes       = 4 * NBIPXWANRouterCount
	NBIPXNameServiceHeaderLen = 2 // NameTypeFlag + DataStreamType
	NBIPXNameServiceLen       = NBIPXWANRouterBytes + NBIPXNameServiceHeaderLen + NameLength
	NMPIFixedHeaderLen        = NBIPXWANRouterBytes + 1 + 1 + 2 + NameLength + NameLength
)

const (
	// NMPI opcodes used on sockets 0x0551/0x0553.
	NMPIOpNameClaim    uint8 = 0xF1
	NMPIOpNameDelete   uint8 = 0xF2
	NMPIOpNameQuery    uint8 = 0xF3
	NMPIOpNameFound    uint8 = 0xF4
	NMPIOpMsgHangup    uint8 = 0xF5
	NMPIOpMailslotSend uint8 = 0xFC
	NMPIOpMailslotFind uint8 = 0xFD
	NMPIOpMailslotName uint8 = 0xFE
)

const (
	NMPINameTypeMachine   uint8 = 0x01
	NMPINameTypeWorkgroup uint8 = 0x02
	NMPINameTypeBrowser   uint8 = 0x03
)

// NMPIPacket is the Name Management Protocol over IPX payload layout
// used by browser mailslot and name-query traffic on sockets 0x0551/0x0553.
type NMPIPacket struct {
	Routers       [NBIPXWANRouterCount][4]byte
	Opcode        uint8
	NameType      uint8
	MessageID     uint16 // little-endian on wire
	RequestedName Name
	SourceName    Name
	Payload       []byte
}

// EncodeNMPIPacket serializes an NMPI packet using the fixed 52-byte
// header followed by optional payload.
func EncodeNMPIPacket(p *NMPIPacket) []byte {
	out := make([]byte, NMPIFixedHeaderLen+len(p.Payload))
	off := 0
	for i := range NBIPXWANRouterCount {
		copy(out[off:off+4], p.Routers[i][:])
		off += 4
	}
	out[off] = p.Opcode
	off++
	out[off] = p.NameType
	off++
	binary.LittleEndian.PutUint16(out[off:off+2], p.MessageID)
	off += 2
	copy(out[off:off+NameLength], p.RequestedName[:])
	off += NameLength
	copy(out[off:off+NameLength], p.SourceName[:])
	off += NameLength
	copy(out[off:], p.Payload)
	return out
}

// DecodeNMPIPacket parses an NMPI packet from the fixed 52-byte
// header plus optional trailing payload.
func DecodeNMPIPacket(b []byte) (*NMPIPacket, error) {
	if len(b) < NMPIFixedHeaderLen {
		return nil, ErrShortNBIPX
	}
	var p NMPIPacket
	off := 0
	for i := range NBIPXWANRouterCount {
		copy(p.Routers[i][:], b[off:off+4])
		off += 4
	}
	p.Opcode = b[off]
	off++
	p.NameType = b[off]
	off++
	p.MessageID = binary.LittleEndian.Uint16(b[off : off+2])
	off += 2
	copy(p.RequestedName[:], b[off:off+NameLength])
	off += NameLength
	copy(p.SourceName[:], b[off:off+NameLength])
	off += NameLength
	p.Payload = make([]byte, len(b)-off)
	copy(p.Payload, b[off:])
	return &p, nil
}

// NBIPXNameServicePacket is the body carried inside an IPX type-20
// WAN-broadcast name packet:
//
//	32 bytes: 8 router network numbers (4 bytes each)
//	1 byte:  NameTypeFlag
//	1 byte:  DataStreamType (NBIPXFindName, NBIPXNameRecognized, ...)
//	16 bytes: NetBIOS name
//
// Router entries are zero-filled for same-segment broadcasts.
type NBIPXNameServicePacket struct {
	Routers        [NBIPXWANRouterCount][4]byte
	NameTypeFlag   uint8
	DataStreamType uint8
	Name           Name
}

// EncodeNameService serialises a name-service body to the canonical
// WAN-broadcast wire form (50 bytes). The IPX header (with Type=20)
// is the caller's job.
func EncodeNameService(p *NBIPXNameServicePacket) []byte {
	out := make([]byte, NBIPXNameServiceLen)
	off := 0
	for i := range NBIPXWANRouterCount {
		copy(out[off:off+4], p.Routers[i][:])
		off += 4
	}
	out[off] = p.NameTypeFlag
	off++
	out[off] = p.DataStreamType
	off++
	copy(out[off:off+NameLength], p.Name[:])
	return out
}

// DecodeNameService parses a name-service body. It accepts both the
// canonical 50-byte WAN-broadcast form and the legacy 16-byte
// name-only form for compatibility with earlier builds.
func DecodeNameService(b []byte) (*NBIPXNameServicePacket, error) {
	if len(b) < NameLength {
		return nil, ErrShortNBIPX
	}
	var p NBIPXNameServicePacket
	if len(b) >= NBIPXNameServiceLen {
		off := 0
		for i := range NBIPXWANRouterCount {
			copy(p.Routers[i][:], b[off:off+4])
			off += 4
		}
		p.NameTypeFlag = b[off]
		off++
		p.DataStreamType = b[off]
		off++
		copy(p.Name[:], b[off:off+NameLength])
		return &p, nil
	}

	// Legacy: payload carried only the 16-byte NetBIOS name.
	p.DataStreamType = NBIPXFindName
	copy(p.Name[:], b[:NameLength])
	return &p, nil
}

// ErrShortNBIPX indicates an NB-IPX packet body too short to contain
// the header (or, for name-service packets, the name).
var ErrShortNBIPX = errors.New("netbios: short NB-IPX packet")
