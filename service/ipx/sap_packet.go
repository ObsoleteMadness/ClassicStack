package ipx

import (
	"encoding/binary"
	"errors"
)

// SAP operation codes carried in the first 16 bits of every SAP body.
const (
	SAPGeneralQuery    uint16 = 1
	SAPGeneralResponse uint16 = 2
	SAPNearestQuery    uint16 = 3
	SAPNearestResponse uint16 = 4
)

// Well-known SAP service types. NetBIOS over IPX uses 0x0640; a
// NetWare file server proper uses 0x0004.
const (
	SAPServiceTypeWildcard  uint16 = 0xFFFF
	SAPServiceTypeFileSrv   uint16 = 0x0004
	SAPServiceTypeNetBIOS   uint16 = 0x0640
)

// SAPHopsUnreachable is the sentinel hop count meaning "no route";
// real entries are 1..15 inclusive (0 is reserved for "self" in some
// implementations but commonly 1 is used for our own services).
const SAPHopsUnreachable uint16 = 16

// SAPNameLength is the fixed-width zero-padded name field carried in
// every SAP response entry.
const SAPNameLength = 48

// SAPEntrySize is the on-wire size of one SAP response entry.
const SAPEntrySize = 2 + SAPNameLength + 4 + 6 + 2 + 2 // = 64

// SAPMaxEntriesPerPacket limits broadcast packets to the IPX-MTU
// budget: 30-byte IPX header + 2-byte op + N*64 ≤ 576. With seven
// entries the body is 2 + 7*64 = 450 bytes, well under the limit.
const SAPMaxEntriesPerPacket = 7

// SAPEntry is one service advertisement carried inside a SAP
// response. The Name field is the human-visible service identifier
// (e.g. "CLASSICSTACK").
type SAPEntry struct {
	ServiceType uint16
	Name        string
	Network     [4]byte
	Node        [6]byte
	Socket      [2]byte
	Hops        uint16
}

// SAPPacket is the decoded form of a SAP body. Queries carry a single
// service type in QueryServiceType (Entries left empty); responses
// carry one or more Entries (QueryServiceType ignored).
type SAPPacket struct {
	Operation        uint16
	QueryServiceType uint16
	Entries          []SAPEntry
}

// EncodeSAP serialises a SAP body for the wire.
func EncodeSAP(p *SAPPacket) ([]byte, error) {
	if p == nil {
		return nil, errors.New("ipx: nil SAP packet")
	}
	switch p.Operation {
	case SAPGeneralQuery, SAPNearestQuery:
		out := make([]byte, 4)
		binary.BigEndian.PutUint16(out[0:2], p.Operation)
		binary.BigEndian.PutUint16(out[2:4], p.QueryServiceType)
		return out, nil
	case SAPGeneralResponse, SAPNearestResponse:
		if len(p.Entries) > SAPMaxEntriesPerPacket {
			return nil, errors.New("ipx: too many SAP entries for one packet")
		}
		out := make([]byte, 2+SAPEntrySize*len(p.Entries))
		binary.BigEndian.PutUint16(out[0:2], p.Operation)
		off := 2
		for _, e := range p.Entries {
			binary.BigEndian.PutUint16(out[off:off+2], e.ServiceType)
			off += 2
			// Name is zero-padded; truncate names longer than 47
			// bytes to leave room for the trailing null.
			name := e.Name
			if len(name) > SAPNameLength-1 {
				name = name[:SAPNameLength-1]
			}
			copy(out[off:off+SAPNameLength], []byte(name))
			off += SAPNameLength
			copy(out[off:off+4], e.Network[:])
			off += 4
			copy(out[off:off+6], e.Node[:])
			off += 6
			copy(out[off:off+2], e.Socket[:])
			off += 2
			binary.BigEndian.PutUint16(out[off:off+2], e.Hops)
			off += 2
		}
		return out, nil
	default:
		return nil, errors.New("ipx: unknown SAP operation")
	}
}

// DecodeSAP parses a SAP body. Query and response shapes are
// distinguished by the operation field.
func DecodeSAP(b []byte) (*SAPPacket, error) {
	if len(b) < 2 {
		return nil, ErrShortSAP
	}
	op := binary.BigEndian.Uint16(b[0:2])
	switch op {
	case SAPGeneralQuery, SAPNearestQuery:
		if len(b) < 4 {
			return nil, ErrShortSAP
		}
		return &SAPPacket{
			Operation:        op,
			QueryServiceType: binary.BigEndian.Uint16(b[2:4]),
		}, nil
	case SAPGeneralResponse, SAPNearestResponse:
		p := &SAPPacket{Operation: op}
		off := 2
		for off+SAPEntrySize <= len(b) {
			var e SAPEntry
			e.ServiceType = binary.BigEndian.Uint16(b[off : off+2])
			off += 2
			// Trim trailing nulls from the name field.
			name := b[off : off+SAPNameLength]
			n := 0
			for n < len(name) && name[n] != 0 {
				n++
			}
			e.Name = string(name[:n])
			off += SAPNameLength
			copy(e.Network[:], b[off:off+4])
			off += 4
			copy(e.Node[:], b[off:off+6])
			off += 6
			copy(e.Socket[:], b[off:off+2])
			off += 2
			e.Hops = binary.BigEndian.Uint16(b[off : off+2])
			off += 2
			p.Entries = append(p.Entries, e)
		}
		return p, nil
	default:
		return nil, errors.New("ipx: unknown SAP operation")
	}
}

// ErrShortSAP indicates a SAP body too short to even contain the
// operation field (or, for queries, the service-type that follows).
var ErrShortSAP = errors.New("ipx: short SAP packet")
