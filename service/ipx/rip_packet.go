package ipx

import (
	"encoding/binary"
	"errors"
)

// RIP packet operations. The 16-bit operation field appears at the
// start of every RIP body.
const (
	RIPRequest  uint16 = 1
	RIPResponse uint16 = 2
)

// RIPHopUnreachable is the sentinel hop count meaning "no route";
// real RIP entries are 0..15 inclusive.
const RIPHopUnreachable uint16 = 16

// RIPNetworkAny is the wildcard network number a request body uses
// when asking "tell me about every network you know."
var RIPNetworkAny = [4]byte{0xFF, 0xFF, 0xFF, 0xFF}

// RIPEntry is a single network advertisement carried inside a RIP
// packet. Hops and Ticks are encoded big-endian on the wire.
type RIPEntry struct {
	Network [4]byte
	Hops    uint16
	Ticks   uint16
}

// RIPPacket is the decoded form of a RIP body (i.e. the IPX payload
// at socket 0x0453, not including the 30-byte IPX header).
type RIPPacket struct {
	Operation uint16
	Entries   []RIPEntry
}

// EncodeRIP serialises a RIP body. The wire layout is:
//
//	uint16  operation
//	[]entry; each entry is:
//	  [4]byte network
//	  uint16  hops
//	  uint16  ticks
func EncodeRIP(p *RIPPacket) ([]byte, error) {
	if p == nil {
		return nil, errors.New("ipx: nil RIP packet")
	}
	out := make([]byte, 2+8*len(p.Entries))
	binary.BigEndian.PutUint16(out[0:2], p.Operation)
	off := 2
	for _, e := range p.Entries {
		copy(out[off:off+4], e.Network[:])
		binary.BigEndian.PutUint16(out[off+4:off+6], e.Hops)
		binary.BigEndian.PutUint16(out[off+6:off+8], e.Ticks)
		off += 8
	}
	return out, nil
}

// DecodeRIP parses a RIP body. Returns ErrShortRIP when input is
// shorter than two bytes. Trailing bytes that don't form a complete
// 8-byte entry are ignored — RIP packets in the wild often pad to a
// minimum size, and a strict parser would reject those.
func DecodeRIP(b []byte) (*RIPPacket, error) {
	if len(b) < 2 {
		return nil, ErrShortRIP
	}
	p := &RIPPacket{
		Operation: binary.BigEndian.Uint16(b[0:2]),
	}
	off := 2
	for off+8 <= len(b) {
		var e RIPEntry
		copy(e.Network[:], b[off:off+4])
		e.Hops = binary.BigEndian.Uint16(b[off+4 : off+6])
		e.Ticks = binary.BigEndian.Uint16(b[off+6 : off+8])
		p.Entries = append(p.Entries, e)
		off += 8
	}
	return p, nil
}

// ErrShortRIP indicates a RIP body too short to even contain the
// operation field.
var ErrShortRIP = errors.New("ipx: short RIP packet")
