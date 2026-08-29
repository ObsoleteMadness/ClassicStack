// Package rip holds the Novell IPX Routing Information Protocol wire DTOs — the
// request/response format riding IPX socket 0x0453 (IPX packet type 1). A NetWare
// client resolves the network a SAP advertisement names via a RIP Request (the
// "GetLocalTarget" step) before it will open an NCP connection: it broadcasts a
// Request for the advertised network and takes the responder's node as the
// immediate (MAC-level) address for that network.
//
// Wire format (all fields BIG-ENDIAN): a 2-byte operation followed by zero or more
// 8-byte entries — network(4) hops(2) ticks(2). In a Request the hops/ticks of each
// entry are 0xFFFF filler; the network 0xFFFFFFFF asks for all known routes.
//
// Reference: Novell RIP (IPX socket 0x0453); mars_nwe nwroute.c (handle_rip,
// build_rip_buff, send_rip_buff) — the canonical open-source reference (CLAUDE.md #7).
package rip

import "errors"

// Socket is the well-known IPX socket RIP rides.
var Socket = [2]byte{0x04, 0x53}

// IPXType is the IPX packet type for RIP (type 1).
const IPXType uint8 = 0x01

// RIP operation codes (the first two bytes of a RIP packet, big-endian).
const (
	OpRequest  uint16 = 0x0001 // route query ("GetLocalTarget" when for one net)
	OpResponse uint16 = 0x0002 // answer to a request / periodic broadcast
)

// NetworkWildcard in a Request entry asks for all known routes (mars_nwe MAX_U32).
var NetworkWildcard = [4]byte{0xFF, 0xFF, 0xFF, 0xFF}

// HopsUnreachable marks a route as down (16 = infinity). A shutdown broadcast
// advertises every owned network at this metric so clients drop the route
// (mars_nwe send_rip_broadcast mode 2 → hops 16).
const HopsUnreachable uint16 = 16

// EntryLen is the fixed length of one RIP entry: network(4) hops(2) ticks(2).
const EntryLen = 8

// headerLen is the operation field ahead of the entries.
const headerLen = 2

// ErrShort is returned by Unmarshal for a buffer too short to hold an operation.
var ErrShort = errors.New("rip: packet shorter than operation header")

// Entry is one route: the network and its distance in router hops and ticks
// (1 tick ≈ 1/18.2 s). A directly served network is hops 1 / ticks 2 in a
// response (mars_nwe ins_rip_buff(internal_net, 1, 2); a real NetWare 4 server
// answers the same).
type Entry struct {
	Network [4]byte
	Hops    uint16
	Ticks   uint16
}

// Packet is a parsed RIP request or response.
type Packet struct {
	Operation uint16
	Entries   []Entry
}

// Marshal appends the wire form (operation + entries) to dst and returns it.
func (p *Packet) Marshal(dst []byte) []byte {
	dst = append(dst, byte(p.Operation>>8), byte(p.Operation))
	for _, e := range p.Entries {
		dst = append(dst, e.Network[:]...)
		dst = append(dst, byte(e.Hops>>8), byte(e.Hops))
		dst = append(dst, byte(e.Ticks>>8), byte(e.Ticks))
	}
	return dst
}

// Unmarshal parses a RIP packet. Trailing bytes short of a whole entry are
// ignored (clients pad to minimum Ethernet frame length).
func Unmarshal(b []byte) (*Packet, error) {
	if len(b) < headerLen {
		return nil, ErrShort
	}
	p := &Packet{Operation: uint16(b[0])<<8 | uint16(b[1])}
	b = b[headerLen:]
	for len(b) >= EntryLen {
		var e Entry
		copy(e.Network[:], b[:4])
		e.Hops = uint16(b[4])<<8 | uint16(b[5])
		e.Ticks = uint16(b[6])<<8 | uint16(b[7])
		p.Entries = append(p.Entries, e)
		b = b[EntryLen:]
	}
	return p, nil
}
