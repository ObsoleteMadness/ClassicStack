package llap

import "errors"

// LLAP frame constants (spec/09-port-localtalk-base.md §"LLAP Frame Format"). An
// LLAP frame is a 3-byte header — destination node, source node, type — followed
// (for DDP types only) by a DDP datagram. The DDP-data types live here as named
// constants so the adapter framer and this control core agree on one set.
const (
	// HeaderLen is the fixed LLAP header: dest(1) + src(1) + type(1).
	HeaderLen = 3

	// BroadcastNode is the LLAP destination selecting every node on the segment.
	BroadcastNode uint8 = 0xFF

	// LLAP type codes carried in the third header byte.
	TypeShortDDP uint8 = 0x01 // short-header DDP (intra-network; net numbers implicit)
	TypeLongDDP  uint8 = 0x02 // long-header DDP (inter-network; full DDP header)
	TypeENQ      uint8 = 0x81 // node-claim probe (control; no payload)
	TypeACK      uint8 = 0x82 // node-claim response (control; no payload)
)

// Node-address range (spec §"Node Address Acquisition"). The valid unicast range
// is 1..0xFE; 0 is reserved (unclaimed) and 0xFF is broadcast.
const (
	// NodeUnclaimed is the node value before a claim completes.
	NodeUnclaimed uint8 = 0x00
	// MinNode / MaxNode bound the claimable unicast node range.
	MinNode uint8 = 0x01
	MaxNode uint8 = 0xFE
	// DefaultDesiredNode is the preferred first candidate a claim probes (spec default).
	DefaultDesiredNode uint8 = 0xFE
)

// ErrShortLLAP is returned by DecodeControl for a frame too small to hold the
// 3-byte LLAP header.
var ErrShortLLAP = errors.New("llap: frame too short for LLAP header")

// ControlFrame is a decoded LLAP CONTROL frame (ENQ/ACK) — the node-claim header
// with no DDP payload. The data frames (short/long DDP) are decoded by the adapter
// framer's ddp seam, not here.
type ControlFrame struct {
	Dst  uint8 // destination node (0xFF broadcast)
	Src  uint8 // source node
	Type uint8 // TypeENQ or TypeACK
}

// IsControl reports whether an LLAP type byte is a control (ENQ/ACK) frame rather
// than a DDP-data frame.
func IsControl(typ uint8) bool { return typ == TypeENQ || typ == TypeACK }

// Header returns the three LLAP header bytes of a frame, or ok=false when the frame
// is shorter than the header. It lets the adapter classify a frame (control vs DDP
// data) from one place without re-reading the offsets.
func Header(frame []byte) (dst, src, typ uint8, ok bool) {
	if len(frame) < HeaderLen {
		return 0, 0, 0, false
	}
	return frame[0], frame[1], frame[2], true
}

// EncodeControl renders an LLAP control frame: the 3-byte header with no payload
// (ENQ/ACK are header-only). The returned slice is freshly allocated.
func EncodeControl(c ControlFrame) []byte {
	return []byte{c.Dst, c.Src, c.Type}
}

// DecodeControl parses an LLAP control frame header from frame. It returns
// ErrShortLLAP for a runt; a non-control type byte still decodes (the caller checks
// IsControl) so the read loop can classify in one step.
func DecodeControl(frame []byte) (ControlFrame, error) {
	dst, src, typ, ok := Header(frame)
	if !ok {
		return ControlFrame{}, ErrShortLLAP
	}
	return ControlFrame{Dst: dst, Src: src, Type: typ}, nil
}

// Enq builds a node-claim ENQ probe for a candidate node. Per spec the ENQ is
// self-addressed: destination AND source are the candidate node (the convention a
// receiver uses to recognise a probe for an address).
func Enq(candidate uint8) ControlFrame {
	return ControlFrame{Dst: candidate, Src: candidate, Type: TypeENQ}
}

// Ack builds a node-claim ACK defending a claimed node: destination and source are
// the claimed node, signalling the address is taken.
func Ack(node uint8) ControlFrame {
	return ControlFrame{Dst: node, Src: node, Type: TypeACK}
}
