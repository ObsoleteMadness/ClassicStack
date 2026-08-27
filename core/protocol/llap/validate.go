package llap

import (
	"errors"

	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
)

// This file holds the cheap STRUCTURAL check a transport runs on a frame the
// moment it arrives, before anything downstream sees it.
//
// WHY it exists, and why at ingress rather than at decode: real LocalTalk closes
// every frame with a CRC, so a receiver that mis-frames, truncates, or hands over
// a stale buffer is caught on the wire. LToUDP has no such trailer — a datagram is
// whatever the peer put in it, and a peer with a buffer-reuse bug will happily
// deliver a frame header followed by leftover bytes from an earlier frame. The
// LLAP/DDP headers carry enough redundancy to catch most of that for free: the DDP
// length field states, in the frame itself, how long the frame is supposed to be.
// A frame whose declared length disagrees with the datagram that carried it is
// malformed by construction, whatever it decodes to.
//
// The framer above (adapter/link/framing) already refuses to DECODE such a frame,
// so nothing downstream was ever at risk from the length-inconsistent ones. What
// ingress validation buys is different and twofold: the drop happens before the
// capture tee, so a .pcap stays readable instead of filling with junk records; and
// the transport can name the PEER that sent it, which the framer — which sees only
// frames, never addresses — structurally cannot.
//
// This is not an integrity check. A frame corrupted WITHIN its declared length
// still passes, because nothing in LLAP-over-UDP can detect that; see Validate.

// Short/long DDP header sizes, as they appear immediately after the LLAP header.
const (
	// ShortDDPHeaderLen is the short-header DDP prefix: length(2) + destSocket(1)
	// + srcSocket(1) + ddpType(1). Node and network numbers are implied by the
	// LLAP header and the receiving port's network, so they are not on the wire.
	ShortDDPHeaderLen = 5
	// LongDDPHeaderLen is the long-header DDP prefix: flags+length(2) +
	// checksum(2) + destNet(2) + srcNet(2) + destNode(1) + srcNode(1) +
	// destSocket(1) + srcSocket(1) + ddpType(1).
	LongDDPHeaderLen = 13
)

// MaxFrameLen is the largest well-formed LLAP frame: the 3-byte LLAP header plus a
// full long-header DDP datagram.
const MaxFrameLen = HeaderLen + LongDDPHeaderLen + ddp.MaxDataLength

var (
	// ErrBadType is returned for a type byte that is neither a DDP-data type
	// (short/long) nor a node-claim control type (ENQ/ACK).
	ErrBadType = errors.New("llap: unrecognised frame type")
	// ErrShortDDP is returned when a DDP-data frame is too short to hold the DDP
	// header its type byte promises.
	ErrShortDDP = errors.New("llap: frame too short for its DDP header")
	// ErrReservedBits is returned when the reserved high bits of the DDP length
	// word are set. On a short header all six are reserved; on a long header the
	// top two are, with four hop-count bits between them and the length.
	ErrReservedBits = errors.New("llap: reserved bits set in DDP length word")
	// ErrBadLength is returned when the DDP length field disagrees with the frame
	// that carries it — the signature of a truncated frame or a stale send buffer.
	ErrBadLength = errors.New("llap: DDP length disagrees with frame length")
	// ErrControlPayload is returned for an ENQ/ACK carrying payload bytes; the
	// node-claim control frames are header-only.
	ErrControlPayload = errors.New("llap: control frame carries a payload")
	// ErrControlAddress is returned for an ENQ/ACK that is not self-addressed.
	// Both control frames name the contested node in BOTH header slots (see Enq
	// and Ack), so dst != src means the frame did not come from a claim engine.
	ErrControlAddress = errors.New("llap: control frame is not self-addressed")
)

// Validate reports whether frame is a structurally well-formed LLAP frame,
// returning nil when it is and a specific error naming the defect when it is not.
//
// It checks only what the frame asserts about ITSELF: that the type byte is one
// this link layer defines, that a DDP frame is long enough for the header its type
// implies, that the reserved bits of the length word are clear, and that the
// declared DDP length is exactly the length of the payload carried. Every one of
// those is a pure arithmetic check on bytes already in hand — no allocation, no
// decode, safe to run on every frame at ingress.
//
// It deliberately does NOT check: node numbers on data frames (a router legitimately
// forwards for nodes this segment has never seen), the DDP checksum (optional, and
// almost always zero in practice), or anything about the payload. A frame whose
// bytes are corrupted but whose declared length still matches WILL pass — LLAP over
// UDP carries no CRC, so that corruption is undetectable at this layer and must be
// caught, if at all, by the protocol that reads the payload.
//
// Control frames are held to the node-claim rules (header-only, self-addressed)
// because ENQ and ACK are the only control types meaningful on a datagram
// transport: RTS/CTS arbitrate access to a physical LocalTalk wire and have no
// counterpart on a UDP multicast group, so a peer has no reason to send one.
func Validate(frame []byte) error {
	dst, src, typ, ok := Header(frame)
	if !ok {
		return ErrShortLLAP
	}
	payload := frame[HeaderLen:]

	switch typ {
	case TypeENQ, TypeACK:
		if len(payload) != 0 {
			return ErrControlPayload
		}
		// Self-addressed AND a claimable unicast node: 0 (unclaimed) and 0xFF
		// (broadcast) are never the subject of a claim.
		if dst != src || dst < MinNode || dst > MaxNode {
			return ErrControlAddress
		}
		return nil

	case TypeShortDDP:
		return validateDDP(payload, ShortDDPHeaderLen, 0xFC)

	case TypeLongDDP:
		// The long header's first byte is flags(2) + hops(4) + the length's high
		// 2 bits, so only the top two bits are reserved (ddp.Decode agrees).
		return validateDDP(payload, LongDDPHeaderLen, 0xC0)

	default:
		return ErrBadType
	}
}

// validateDDP checks a DDP payload's self-declared length against its actual
// length. hdrLen is the header size the LLAP type implies and reservedMask selects
// the bits of the first byte that must be clear for that header form.
func validateDDP(payload []byte, hdrLen int, reservedMask byte) error {
	if len(payload) < hdrLen {
		return ErrShortDDP
	}
	if payload[0]&reservedMask != 0 {
		return ErrReservedBits
	}
	// The length is 10 bits: the low 2 of byte 0 and all of byte 1. It counts the
	// DDP header itself, so it must equal the whole payload, not just the data.
	length := int(payload[0]&0x03)<<8 | int(payload[1])
	if length != len(payload) {
		return ErrBadLength
	}
	if length > hdrLen+ddp.MaxDataLength {
		return ErrBadLength
	}
	return nil
}
