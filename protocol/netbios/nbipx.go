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
	NBIPXFindName        uint8 = 0x00 // also: name service request
	NBIPXNameRecognized  uint8 = 0x01 // also: name service reply (positive)
	NBIPXSessionInit     uint8 = 0x05
	NBIPXSessionConfirm  uint8 = 0x06
	NBIPXSessionEnd      uint8 = 0x07
	NBIPXSessionEndAck   uint8 = 0x08
	NBIPXStatusQuery     uint8 = 0x09
	NBIPXStatusResponse  uint8 = 0x0A
	NBIPXDataAck         uint8 = 0x14
	NBIPXDataOnlyLast    uint8 = 0x15
	NBIPXDataFirstMiddle uint8 = 0x16
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
	ConnCtrlFlag   uint8  // SYS|ACK|ATT|EOM bitfield
	DataStreamType uint8  // NBIPXFindName, NBIPXSessionInit, ...
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

// NBIPXNameServicePacket is the body carried inside an IPX type-20
// broadcast for name claim / query. The on-wire layout has been
// observed to be just a 16-byte NetBIOS name; some implementations
// also include a 1-byte name-type indicator before the name and a
// 1-byte status byte after, but the 16-byte-name-only form is
// what NetBIOS over IPX (NWLink) clients send and accept.
//
// The function code below encodes the operator's intent: "claim",
// "query", or "in conflict" — distinguished only by who sent it
// and whether it was solicited. The codec itself does not emit a
// distinct opcode; the receiver infers from context.
type NBIPXNameServicePacket struct {
	Name Name
}

// EncodeNameService serialises a name-service body to its 16-byte
// wire form. The IPX header (with Type=20) is the caller's job.
func EncodeNameService(p *NBIPXNameServicePacket) []byte {
	out := make([]byte, NameLength)
	copy(out, p.Name[:])
	return out
}

// DecodeNameService parses a 16-byte name-service body. Returns
// ErrShortNBIPX when shorter.
func DecodeNameService(b []byte) (*NBIPXNameServicePacket, error) {
	if len(b) < NameLength {
		return nil, ErrShortNBIPX
	}
	var p NBIPXNameServicePacket
	copy(p.Name[:], b[:NameLength])
	return &p, nil
}

// ErrShortNBIPX indicates an NB-IPX packet body too short to contain
// the header (or, for name-service packets, the name).
var ErrShortNBIPX = errors.New("netbios: short NB-IPX packet")
