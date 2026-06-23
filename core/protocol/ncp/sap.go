package ncp

// sap.go holds the Service Advertising Protocol (SAP) wire DTOs — the IPX
// broadcast/query format NetWare servers use to advertise themselves so NETx/VLM
// clients discover a file server without a preferred-server binding. SAP rides IPX
// socket 0x0452; all multi-byte fields are BIG-ENDIAN.
//
// Reference: Novell SAP (IPX socket 0x0452); mars_nwe / ncpfs. A SAP packet is a
// 2-byte operation followed by one or more 64-byte service entries (the
// general/periodic forms) or a bare type for the query forms.

import "errors"

// SAPSocket is the well-known IPX socket SAP rides.
var SAPSocket = [2]byte{0x04, 0x52}

// NCPSocket is the well-known IPX socket the NCP file service listens on; SAP
// advertises this as the file server's service socket.
var NCPSocket = [2]byte{0x04, 0x51}

// SAP operation codes (the first two bytes of a SAP packet, big-endian).
const (
	SAPGeneralQuery    uint16 = 0x0001 // "who offers service type X?"
	SAPGeneralResponse uint16 = 0x0002 // periodic broadcast / answer to a general query
	SAPNearestQuery    uint16 = 0x0003 // "nearest server of type X?" (Get Nearest Server)
	SAPNearestResponse uint16 = 0x0004 // answer to a nearest-service query
)

// SAPServerTypeFileServer is the SAP service type for a NetWare File Server. A
// client issues a nearest/general query for this type to find a server to attach
// to.
const SAPServerTypeFileServer uint16 = 0x0004

// SAPServerTypeWildcard matches any service type in a query.
const SAPServerTypeWildcard uint16 = 0xFFFF

// SAPEntryLen is the fixed length of one SAP service entry: type(2) name(48)
// net(4) node(6) socket(2) hops(2) = 64 bytes. Exported so a transport/test can
// size a SAP buffer or step entries.
const SAPEntryLen = 64

// sapNameLen is the fixed (NUL-padded) length of a SAP service name field.
const sapNameLen = 48

// ErrShortSAP is returned by UnmarshalSAPQuery for a buffer too short to hold an
// operation + service type.
var ErrShortSAP = errors.New("ncp: SAP packet shorter than query header")

// SAPEntry is one advertised service: its type, name, and IPX address (the
// network/node/socket a client should contact). Hops is the distance metric
// (0 for a directly attached server, the value clients use to pick the nearest).
type SAPEntry struct {
	Type    uint16
	Name    string // ≤47 chars; NUL-padded to 48 on the wire, upper-cased by convention
	Network [4]byte
	Node    [6]byte
	Socket  [2]byte
	Hops    uint16
}

// MarshalResponse appends a SAP response packet (operation + the entries) to dst
// and returns it. op is SAPGeneralResponse (periodic broadcast / general answer)
// or SAPNearestResponse (nearest-service answer).
func MarshalResponse(op uint16, entries []SAPEntry, dst []byte) []byte {
	dst = append(dst, byte(op>>8), byte(op))
	for _, e := range entries {
		dst = e.marshal(dst)
	}
	return dst
}

// marshal appends one 64-byte SAP service entry to dst.
func (e SAPEntry) marshal(dst []byte) []byte {
	dst = append(dst, byte(e.Type>>8), byte(e.Type))
	var name [sapNameLen]byte
	copy(name[:], e.Name) // truncates at 48 and leaves the rest NUL
	dst = append(dst, name[:]...)
	dst = append(dst, e.Network[:]...)
	dst = append(dst, e.Node[:]...)
	dst = append(dst, e.Socket[:]...)
	dst = append(dst, byte(e.Hops>>8), byte(e.Hops))
	return dst
}

// SAPQuery is a parsed SAP query: the operation (general vs nearest) and the
// service type being sought.
type SAPQuery struct {
	Operation   uint16
	ServiceType uint16
}

// UnmarshalSAPQuery parses a SAP query packet (operation + service type). It
// returns ErrShortSAP if the buffer is too short. A response packet (which carries
// entries, not a bare type) also parses — the caller dispatches on Operation.
func UnmarshalSAPQuery(b []byte) (*SAPQuery, error) {
	if len(b) < 4 {
		return nil, ErrShortSAP
	}
	return &SAPQuery{
		Operation:   uint16(b[0])<<8 | uint16(b[1]),
		ServiceType: uint16(b[2])<<8 | uint16(b[3]),
	}, nil
}

// WantsType reports whether a query for ServiceType should be answered with an
// advertisement of want (matching exactly or via the wildcard).
func (q *SAPQuery) WantsType(want uint16) bool {
	return q.ServiceType == want || q.ServiceType == SAPServerTypeWildcard
}
