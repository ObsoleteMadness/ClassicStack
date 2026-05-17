// Package macipx implements the framing used between Macintosh MacIPX
// clients and a Novell-style MacIPX gateway (MACIPXGW.NLM). The protocol
// rides on top of DDP and is observation-driven: see
// spec/15-macipx-gateway.md for the wire format.
package macipx

import (
	"errors"
	"fmt"
)

const (
	// DDPProtocol is the DDP protocol type byte that carries MacIPX
	// traffic. Both encapsulated IPX and the address-assignment control
	// opcodes share this DDP type.
	DDPProtocol uint8 = 0x4E

	// Socket is the DDP socket the gateway listens on, and the socket
	// MacIPX clients use as their source socket. Both sides use the
	// same socket — there is no asymmetric pairing.
	Socket uint8 = 78

	// NBPType is the NBP type a MacIPX client looks up to discover a
	// gateway (BrRq =:IPX Gateway@<zone>).
	NBPType = "IPX Gateway"
)

// Opcode is the first byte of every DDP-type-0x4E payload.
type Opcode uint8

const (
	// OpcodeData wraps a standard IPX datagram in the remainder of the
	// payload. The IPX checksum field (the first two bytes after the
	// opcode) is preserved verbatim — 0xFFFF when no checksum is in use.
	OpcodeData Opcode = 0x00

	// OpcodeListen registers IPX sockets the client wants broadcast
	// traffic delivered for. Payload is one or more 8-byte
	// (node 6B, socket 2B) pairs; the node is always the IPX
	// broadcast address in observed traffic.
	OpcodeListen Opcode = 0x10

	// OpcodeRegisterReq is a client → gateway request to be assigned an
	// IPX node. Payload is a 6-byte blob (observed value
	// "00 02 00 00 00 01") that the gateway echoes back in the reply.
	OpcodeRegisterReq Opcode = 0x20

	// OpcodeRegisterRsp is the gateway → client reply that grants an IPX
	// node. Payload: the 6-byte request blob echoed back, followed by
	// the low 3 bytes of the assigned IPX node. The implicit high 3
	// bytes are MacIPXNodePrefix; the full assigned node is
	// MacIPXNodePrefix || (3 assigned bytes).
	OpcodeRegisterRsp Opcode = 0x23
)

// MacIPXNodePrefix is the 3-byte prefix every MacIPX-assigned IPX
// node carries on the wire. The gateway implicitly prepends this to
// the 3-byte assignment delivered in the register reply (opcode 0x23).
var MacIPXNodePrefix = [3]byte{0x7A, 0x00, 0x00}

// ErrEmptyFrame is returned by DecodeFrame when the DDP payload is empty.
var ErrEmptyFrame = errors.New("macipx: empty frame")

// DecodeFrame splits a DDP-type-0x4E payload into its opcode and the
// remaining bytes. The remainder is aliased into the input slice — callers
// that need ownership must copy it.
func DecodeFrame(payload []byte) (Opcode, []byte, error) {
	if len(payload) == 0 {
		return 0, nil, ErrEmptyFrame
	}
	return Opcode(payload[0]), payload[1:], nil
}

// EncodeData wraps a fully-formed IPX datagram (30-byte header + payload)
// for transmission inside a DDP-type-0x4E frame.
func EncodeData(ipxDatagram []byte) []byte {
	out := make([]byte, 1+len(ipxDatagram))
	out[0] = byte(OpcodeData)
	copy(out[1:], ipxDatagram)
	return out
}

// EncodeRegisterReply builds an opcode-0x23 frame: the 6-byte request
// blob from the client echoed back, followed by the low 3 bytes of the
// assigned IPX node. The high 3 bytes are implicitly MacIPXNodePrefix
// on the wire; this function does not check that assignedNode actually
// starts with that prefix — the caller is responsible.
//
//	Wire layout: 23 | request[0..6] | assignedNode[3..6]
//
// Example — assigning node 7a:00:00:00:01:01 in response to request
// "00 02 00 00 00 01":
//
//	23 00 02 00 00 00 01 00 01 01
func EncodeRegisterReply(request [6]byte, assignedNode [6]byte) []byte {
	out := make([]byte, 1+6+3)
	out[0] = byte(OpcodeRegisterRsp)
	copy(out[1:7], request[:])
	copy(out[7:10], assignedNode[3:6])
	return out
}

// DecodeRegisterRequest extracts the 6-byte request blob from an
// opcode-0x20 payload (the bytes *after* the opcode).
func DecodeRegisterRequest(rest []byte) ([6]byte, error) {
	var blob [6]byte
	if len(rest) < 6 {
		return blob, fmt.Errorf("macipx: register request too short (%d bytes)", len(rest))
	}
	copy(blob[:], rest[:6])
	return blob, nil
}

// DecodeRegisterReply extracts the assigned IPX node from an opcode-0x23
// payload (the bytes *after* the opcode). It returns the full 6-byte
// node formed by MacIPXNodePrefix || rest[6..9].
func DecodeRegisterReply(rest []byte) ([6]byte, error) {
	var node [6]byte
	if len(rest) < 9 {
		return node, fmt.Errorf("macipx: register reply too short (%d bytes)", len(rest))
	}
	copy(node[0:3], MacIPXNodePrefix[:])
	copy(node[3:6], rest[6:9])
	return node, nil
}

// ListenEntry is one (node, socket) pair in an opcode-0x10 listen
// registration. The Mac client uses node = broadcast (FF:FF:FF:FF:FF:FF)
// to mean "deliver any IPX broadcast addressed to this socket to me";
// other node values have not been observed.
type ListenEntry struct {
	Node   [6]byte
	Socket [2]byte
}

// DecodeListen parses the payload of an opcode-0x10 frame (the bytes
// *after* the opcode) into a list of (node, socket) entries. Each
// entry is 8 bytes: 6-byte node + 2-byte big-endian socket. A single
// 0x10 frame may carry multiple entries — for example a frame that
// subscribes to both the NetWare diagnostic responder (socket 0x0456)
// and a game's discovery socket (e.g. 0xDEAD for Duke3D).
func DecodeListen(rest []byte) ([]ListenEntry, error) {
	if len(rest)%8 != 0 {
		return nil, fmt.Errorf("macipx: listen payload not a multiple of 8 (%d bytes)", len(rest))
	}
	entries := make([]ListenEntry, 0, len(rest)/8)
	for off := 0; off < len(rest); off += 8 {
		var e ListenEntry
		copy(e.Node[:], rest[off:off+6])
		copy(e.Socket[:], rest[off+6:off+8])
		entries = append(entries, e)
	}
	return entries, nil
}

// AssignedNodeForDDP synthesizes the IPX node the gateway should
// associate with a given DDP source address. The encoding mirrors what
// real NetWare gateways hand out in the opcode-0x23 reply:
// MacIPXNodePrefix followed by 0x00, the low byte of the AT network,
// and the AT node.
//
// Examples:
//
//	AT 1.1   → 7a:00:00:00:01:01
//	AT 3.62  → 7a:00:00:00:03:3e
//
// Note: the encoding uses only the low byte of the AT network, so this
// scheme cannot uniquely address two clients on different AT networks
// whose network numbers happen to share their low byte. NetWare appears
// to live with that ambiguity; if it becomes a problem in practice we
// can fall back to a per-client counter for collisions.
func AssignedNodeForDDP(atNetwork uint16, atNode uint8) [6]byte {
	return [6]byte{
		MacIPXNodePrefix[0],
		MacIPXNodePrefix[1],
		MacIPXNodePrefix[2],
		0x00,
		byte(atNetwork & 0xFF),
		atNode,
	}
}
