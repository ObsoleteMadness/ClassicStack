// Package llap defines the LocalTalk Link Access Protocol wire format
// (frame layout, control/data type codes, validation). It contains no
// I/O or state-machine logic — see service/llap for the access-control
// state machine and port/localtalk for the link-layer transports that
// carry LLAP frames over UDP, TashTalk, or virtual cables.
//
// Reference: spec/06-llap.md and Inside AppleTalk, 2nd ed., chapter 1.
package llap

import "fmt"

// Control- and data-type codes carried in the third byte of an LLAP
// frame. Data types (< 0x80) carry an AppleTalk DDP header; control
// types (>= 0x80) participate in the access-control handshake.
const (
	TypeAppleTalkShortHeader = 0x01
	TypeAppleTalkLongHeader  = 0x02
	TypeENQ                  = 0x81
	TypeACK                  = 0x82
	TypeRTS                  = 0x84
	TypeCTS                  = 0x85
)

// BroadcastNode is the LLAP destination address that selects every node
// on the LocalTalk segment.
const BroadcastNode = 0xFF

// MaxDataSize is the largest payload an LLAP data frame may carry.
const MaxDataSize = 600

// Frame is the wire form of an LLAP frame: destination, source, type,
// and an optional payload (data frames only). The 2-byte trailing FCS
// that appears on the cable is handled by the link layer and is not
// represented here.
type Frame struct {
	DestinationNode uint8
	SourceNode      uint8
	Type            uint8
	Payload         []byte
}

// FrameFromBytes parses a wire-form LLAP frame. The returned Frame's
// Payload is a copy and does not alias b.
func FrameFromBytes(b []byte) (Frame, error) {
	if len(b) < 3 {
		return Frame{}, fmt.Errorf("LLAP frame too short: %d", len(b))
	}
	f := Frame{
		DestinationNode: b[0],
		SourceNode:      b[1],
		Type:            b[2],
		Payload:         append([]byte(nil), b[3:]...),
	}
	if err := f.Validate(); err != nil {
		return Frame{}, err
	}
	return f, nil
}

// Validate reports whether f is a well-formed LLAP frame.
func (f Frame) Validate() error {
	if f.IsControl() {
		if len(f.Payload) != 0 {
			return fmt.Errorf("LLAP control frame 0x%02X has payload length %d", f.Type, len(f.Payload))
		}
		switch f.Type {
		case TypeENQ, TypeACK, TypeRTS, TypeCTS:
			return nil
		default:
			return fmt.Errorf("invalid LLAP control type 0x%02X", f.Type)
		}
	}
	if !f.IsData() {
		return fmt.Errorf("invalid LLAP frame type 0x%02X", f.Type)
	}
	if len(f.Payload) > MaxDataSize {
		return fmt.Errorf("LLAP payload too large: %d", len(f.Payload))
	}
	return nil
}

// IsControl reports whether f is a link-control frame (ENQ/ACK/RTS/CTS).
func (f Frame) IsControl() bool { return f.Type >= 0x80 }

// IsData reports whether f carries an AppleTalk DDP datagram.
func (f Frame) IsData() bool {
	return f.Type == TypeAppleTalkShortHeader || f.Type == TypeAppleTalkLongHeader
}

// Bytes returns the wire encoding of f.
func (f Frame) Bytes() []byte {
	out := make([]byte, 0, 3+len(f.Payload))
	out = append(out, f.DestinationNode, f.SourceNode, f.Type)
	out = append(out, f.Payload...)
	return out
}
