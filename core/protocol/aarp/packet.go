package aarp

import (
	"errors"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// AARP fixed-header field values and the EtherTalk hardware/protocol parameters
// (Inside AppleTalk ch.2; values confirmed against the Wireshark dissector).
const (
	// HardwareEthernet is the AARP hardware-type value for Ethernet (ar_hrd).
	HardwareEthernet uint16 = 1
	// ProtocolAppleTalk is the AARP protocol-type value for AppleTalk DDP (ar_pro);
	// the same 0x809B AppleTalk EtherType, used here only as the AARP protocol id.
	ProtocolAppleTalk uint16 = 0x809B

	// HardwareAddrLen / ProtocolAddrLen are the EtherTalk address widths: a 6-byte
	// Ethernet MAC and the 4-byte AppleTalk protocol address (pad + network + node).
	HardwareAddrLen uint8 = 6
	ProtocolAddrLen uint8 = 4
)

// AARP opcodes (the ar_op field). A probe and a request share the wire shape; only the
// opcode and whether the target is the asker's own tentative address differ.
const (
	FuncRequest uint16 = 1 // resolve a known protocol address → hardware address
	FuncReply   uint16 = 2 // answer a request/probe with our hardware address
	FuncProbe   uint16 = 3 // node-claim: is this tentative protocol address in use?
)

// headerLen is the 8-byte fixed AARP header: hwType(2)+protoType(2)+hwLen(1)+protoLen(1)
// +opcode(2). The variable address block follows.
const headerLen = 8

// packetLen is the full EtherTalk AARP packet size: the fixed header plus the uniform
// senderHW(6) · senderProto(4) · targetHW(6) · targetProto(4) block = 8 + 20 = 28.
const packetLen = headerLen + 2*int(HardwareAddrLen) + 2*int(ProtocolAddrLen)

var (
	// ErrShortAARP is returned by Decode when b is smaller than a full EtherTalk AARP
	// packet.
	ErrShortAARP = errors.New("aarp: packet shorter than the EtherTalk AARP header")
	// ErrBadAARP is returned when the fixed header does not describe an EtherTalk AARP
	// packet (wrong hardware/protocol type or address lengths).
	ErrBadAARP = errors.New("aarp: not an EtherTalk AARP packet")
)

// ProtoAddr is an AppleTalk protocol address: a 16-bit network number and an 8-bit node.
// On the wire it is 4 bytes — one zero pad, the network (big-endian), then the node.
type ProtoAddr struct {
	Network uint16
	Node    uint8
}

// Packet is a decoded EtherTalk AARP packet. The four address fields are present for
// every opcode (the uniform sha/spa/tha/tpa layout): a probe/request leaves TargetHw
// zero, a probe sets TargetProto equal to SrcProto (the tentative address).
type Packet struct {
	Function    uint16
	SrcHw       [6]byte
	SrcProto    ProtoAddr
	TargetHw    [6]byte
	TargetProto ProtoAddr
}

// Encode appends the EtherTalk AARP wire form of p to dst and returns it (append-style,
// like ddp.Encode — the caller controls allocation). The fixed header always carries the
// EtherTalk hardware/protocol parameters.
func (p Packet) Encode(dst []byte) []byte {
	dst = bp.AppendBE16(dst, HardwareEthernet)
	dst = bp.AppendBE16(dst, ProtocolAppleTalk)
	dst = append(dst, HardwareAddrLen, ProtocolAddrLen)
	dst = bp.AppendBE16(dst, p.Function)
	dst = append(dst, p.SrcHw[:]...)
	dst = appendProtoAddr(dst, p.SrcProto)
	dst = append(dst, p.TargetHw[:]...)
	dst = appendProtoAddr(dst, p.TargetProto)
	return dst
}

// Decode parses one EtherTalk AARP packet from b. It rejects non-EtherTalk AARP (wrong
// hardware/protocol type or address lengths) so a 802.3/SNAP packet that is not the
// AppleTalk-over-Ethernet form is not misread.
func Decode(b []byte) (Packet, error) {
	if len(b) < packetLen {
		return Packet{}, ErrShortAARP
	}
	if bp.BE16(b[0:2]) != HardwareEthernet || bp.BE16(b[2:4]) != ProtocolAppleTalk {
		return Packet{}, ErrBadAARP
	}
	if b[4] != HardwareAddrLen || b[5] != ProtocolAddrLen {
		return Packet{}, ErrBadAARP
	}
	var p Packet
	p.Function = bp.BE16(b[6:8])
	off := headerLen
	copy(p.SrcHw[:], b[off:off+6])
	off += 6
	p.SrcProto = decodeProtoAddr(b[off : off+4])
	off += 4
	copy(p.TargetHw[:], b[off:off+6])
	off += 6
	p.TargetProto = decodeProtoAddr(b[off : off+4])
	return p, nil
}

// appendProtoAddr writes the 4-byte AppleTalk protocol address (pad + network + node).
func appendProtoAddr(dst []byte, a ProtoAddr) []byte {
	dst = append(dst, 0) // pad
	dst = bp.AppendBE16(dst, a.Network)
	dst = append(dst, a.Node)
	return dst
}

// decodeProtoAddr reads a 4-byte AppleTalk protocol address (pad ignored).
func decodeProtoAddr(b []byte) ProtoAddr {
	return ProtoAddr{Network: bp.BE16(b[1:3]), Node: b[3]}
}

// Probe builds a node-claim probe for a tentative address: the tentative address is the
// source, the target proto repeats it, and the target hardware is left zero (per spec).
func Probe(srcHw [6]byte, tentative ProtoAddr) Packet {
	return Packet{
		Function:    FuncProbe,
		SrcHw:       srcHw,
		SrcProto:    tentative,
		TargetProto: tentative,
	}
}

// Request builds an address-resolution request for a wanted protocol address.
func Request(srcHw [6]byte, src ProtoAddr, want ProtoAddr) Packet {
	return Packet{
		Function:    FuncRequest,
		SrcHw:       srcHw,
		SrcProto:    src,
		TargetProto: want,
	}
}

// Reply builds a response to a requester: our hardware/protocol address as the source,
// the requester's hardware/protocol address as the target.
func Reply(srcHw [6]byte, src ProtoAddr, dstHw [6]byte, dst ProtoAddr) Packet {
	return Packet{
		Function:    FuncReply,
		SrcHw:       srcHw,
		SrcProto:    src,
		TargetHw:    dstHw,
		TargetProto: dst,
	}
}
