// Package nbp defines the AppleTalk Name Binding Protocol wire format
// (function codes, tuple layout, packet parser/builder, and the small
// matching primitives used by lookup). It contains no I/O or service
// state — see service/zip.NameInformationService for the registry and
// routing logic that uses these types.
//
// Reference: spec/04-nbp.md and Inside AppleTalk, 2nd ed., chapter 7.
package nbp

import (
	"bytes"
	"errors"
)

// Well-known DDP socket and DDP type for NBP traffic.
const (
	SASSocket = 2
	DDPType   = 2
)

// NBP control function codes carried in the high nibble of the first
// byte of an NBP packet. The low nibble carries the tuple count.
const (
	CtrlBrRq     = 1 // Broadcast request
	CtrlLkUp     = 2 // Lookup
	CtrlLkUpRply = 3 // Lookup reply
	CtrlFwd      = 4 // Forward request
)

// Wildcards used in BrRq / LkUp lookups.
const (
	NameWildcard = '='
	ZoneWildcard = '*'
)

// ErrMalformed is returned when an inbound packet cannot be decoded.
var ErrMalformed = errors.New("nbp: malformed packet")

// Tuple is a single NBP tuple: an address (network/node/socket), an
// enumerator, and an entity name (object:type@zone). Inbound packets
// carry exactly one tuple in ClassicStack's NBP handler; LkUp-Rply may
// pack several but the registered service emits one per match.
type Tuple struct {
	Network    uint16
	Node       uint8
	Socket     uint8
	Enumerator uint8
	Object     []byte
	Type       []byte
	Zone       []byte
}

// Packet is a parsed NBP packet header plus the embedded tuple.
type Packet struct {
	Function   uint8 // CtrlBrRq, CtrlLkUp, CtrlLkUpRply, CtrlFwd
	TupleCount uint8
	NBPID      uint8
	Tuple      Tuple
}

// ParsePacket decodes the single-tuple form of an NBP packet from a DDP
// payload. It returns ErrMalformed if the layout is invalid or the
// declared lengths run past the buffer.
//
// On-wire layout:
//
//	0       1            2..3       4    5    6    7
//	+-------+------------+----------+----+----+----+
//	|fn|cnt | NBPID      | network  |node|sock|enum|
//	+-------+------------+----------+----+----+----+
//	| obj   | objBytes   | typ      | typBytes ... | zone | zoneBytes |
//
// Trailing zone-length zero is treated as the zone wildcard "*".
func ParsePacket(data []byte) (Packet, error) {
	if len(data) < 8 {
		return Packet{}, ErrMalformed
	}
	funcTupleCount := data[0]
	pkt := Packet{
		Function:   funcTupleCount >> 4,
		TupleCount: funcTupleCount & 0x0F,
		NBPID:      data[1],
	}
	objLen := int(data[7])
	if objLen < 1 || len(data) < 8+objLen+1 {
		return Packet{}, ErrMalformed
	}
	typLen := int(data[8+objLen])
	if typLen < 1 || len(data) < 9+objLen+typLen+1 {
		return Packet{}, ErrMalformed
	}
	zoneLen := int(data[9+objLen+typLen])
	if len(data) < 10+objLen+typLen+zoneLen {
		return Packet{}, ErrMalformed
	}
	pkt.Tuple = Tuple{
		Network:    uint16(data[2])<<8 | uint16(data[3]),
		Node:       data[4],
		Socket:     data[5],
		Enumerator: data[6],
		Object:     data[8 : 8+objLen],
		Type:       data[9+objLen : 9+objLen+typLen],
		Zone:       data[10+objLen+typLen : 10+objLen+typLen+zoneLen],
	}
	if len(pkt.Tuple.Zone) == 0 {
		pkt.Tuple.Zone = []byte{ZoneWildcard}
	}
	return pkt, nil
}

// BuildLkUpRply encodes a single-tuple LkUp-Rply packet. The returned
// slice is freshly allocated.
func BuildLkUpRply(nbpID byte, network uint16, node, socket uint8, obj, typ, zone []byte) []byte {
	out := make([]byte, 0, 12+len(obj)+len(typ)+len(zone))
	out = append(out, (CtrlLkUpRply<<4)|1)
	out = append(out, nbpID)
	out = append(out, byte(network>>8), byte(network))
	out = append(out, node)
	out = append(out, socket)
	out = append(out, 0) // enumerator
	out = append(out, byte(len(obj)))
	out = append(out, obj...)
	out = append(out, byte(len(typ)))
	out = append(out, typ...)
	out = append(out, byte(len(zone)))
	out = append(out, zone...)
	return out
}

// NameMatch reports whether the given pattern matches the registered
// name. NBP uses '=' as the wildcard for object and type fields.
func NameMatch(pattern, name []byte) bool {
	if len(pattern) == 1 && pattern[0] == NameWildcard {
		return true
	}
	return bytes.EqualFold(pattern, name)
}

// ZoneMatch reports whether the given pattern matches the registered
// zone. NBP uses '*' as the zone wildcard.
func ZoneMatch(pattern, zone []byte) bool {
	if len(pattern) == 1 && pattern[0] == ZoneWildcard {
		return true
	}
	return bytes.EqualFold(pattern, zone)
}
