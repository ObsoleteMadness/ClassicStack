// Package diag holds the IPX Diagnostic protocol codec — the Novell IPX/SPX
// Diagnostic Responder framing carried on socket 0x0456, the wire behind Novell's
// IPXPING reachability tool. Wire-format only: self-serialising request/response
// DTOs (the DTO rule), no I/O and no responder state.
//
// Ring: CORE (stdlib only, reflection-free).
//
// No formal spec ships with ClassicStack for this protocol; the layout below is from
// observation of NetWare diagnostic traffic and Novell's published Diagnostic
// Responder description, recorded here per the project's observation-documentation
// rule. See spec/errata.md.
//
// Wire format (IPX payload on socket 0x0456):
//
//	Request (a station asking "who is there / are you there"):
//	  +------+-------------------------------+
//	  | excl | exclusion-address list        |
//	  | cnt  | (excl * 6-byte node IDs)      |
//	  +------+-------------------------------+
//	The exclusion list names nodes that should NOT answer (the sender's own node and
//	any already-known responders), so a broadcast diagnostic does not re-collect
//	hosts. A directed reachability ping carries an empty list (excl = 0).
//
//	Response (a responder announcing its presence + component summary):
//	  +-----------+----------------------------------------------+
//	  | component | per-component records (each: 1-byte type +   |
//	  | count     | type-specific body). The reachability tool   |
//	  +-----------+ treats ANY well-formed response as "alive".  |
//	ClassicStack emits the minimal response: a single component record of type
//	CompIPX (an IPX/SPX node), which is what a reachability ping needs.
package diag

import (
	"errors"
	"slices"
)

// Socket is the IPX socket the Diagnostic Responder listens on (Novell well-known
// IPX/SPX Diagnostic socket).
var Socket = [2]byte{0x04, 0x56}

// Component type bytes carried in a Diagnostic Response component record. Only the
// IPX/SPX component is emitted by ClassicStack; the others are listed for decoding
// real NetWare responders.
const (
	CompIMSP    = 0x00 // IPX/SPX (immediate) — an IPX node
	CompBridge  = 0x02 // an internal IPX bridge/router driver
	CompIPX     = 0x06 // IPX protocol stack
	CompSPX     = 0x07 // SPX protocol stack
	CompNetBIOS = 0x09 // NetBIOS-over-IPX
)

var (
	// ErrShort is returned when a buffer is too short to hold the declared structure.
	ErrShort = errors.New("ipx/diag: buffer shorter than declared structure")
	// ErrTooMany is returned when a request names more exclusion nodes than fit in the
	// single-byte count.
	ErrTooMany = errors.New("ipx/diag: exclusion list exceeds 255 entries")
)

// Request is a Diagnostic request: the list of node IDs that should stay silent.
type Request struct {
	Exclusions [][6]byte
}

// Marshal renders the request (1-byte count + 6-byte node IDs). A nil/empty list
// yields a single zero byte — a directed reachability ping.
func (r Request) Marshal() ([]byte, error) {
	if len(r.Exclusions) > 0xFF {
		return nil, ErrTooMany
	}
	out := make([]byte, 0, 1+6*len(r.Exclusions))
	out = append(out, byte(len(r.Exclusions)))
	for _, n := range r.Exclusions {
		out = append(out, n[:]...)
	}
	return out, nil
}

// UnmarshalRequest parses a Diagnostic request. An empty buffer is treated as an
// implicit empty-exclusion ping (some senders emit a zero-length payload).
func UnmarshalRequest(b []byte) (*Request, error) {
	if len(b) == 0 {
		return &Request{}, nil
	}
	count := int(b[0])
	if len(b) < 1+6*count {
		return nil, ErrShort
	}
	r := &Request{}
	for i := range count {
		var n [6]byte
		copy(n[:], b[1+6*i:1+6*i+6])
		r.Exclusions = append(r.Exclusions, n)
	}
	return r, nil
}

// Excludes reports whether node appears in the request's exclusion list (so the
// responder can stay silent when its own node is named).
func (r *Request) Excludes(node [6]byte) bool {
	return slices.Contains(r.Exclusions, node)
}

// Component is one record in a Diagnostic Response: a type byte plus its raw body.
type Component struct {
	Type uint8
	Body []byte
}

// Response is a Diagnostic Response: the responder's component summary.
type Response struct {
	Components []Component
}

// Marshal renders the response (1-byte component count + each record's type byte
// and body).
func (r Response) Marshal() []byte {
	out := make([]byte, 0, 1+2*len(r.Components))
	out = append(out, byte(len(r.Components)))
	for _, c := range r.Components {
		out = append(out, c.Type)
		out = append(out, c.Body...)
	}
	return out
}

// UnmarshalResponse parses a Diagnostic Response. Component bodies are length-free
// on the wire (the type implies the body length), so this decodes the count and the
// leading type byte of each record but treats the remainder as opaque — enough for a
// reachability tool, which only needs to know a response arrived and how many
// components it claims. The trailing bytes are attached to the last component.
func UnmarshalResponse(b []byte) (*Response, error) {
	if len(b) < 1 {
		return nil, ErrShort
	}
	count := int(b[0])
	r := &Response{}
	rest := b[1:]
	for i := range count {
		if len(rest) < 1 {
			return nil, ErrShort
		}
		c := Component{Type: rest[0]}
		rest = rest[1:]
		// The reachability response ClassicStack emits has empty bodies; for a real
		// NetWare responder the body layout is type-specific and not needed here, so
		// the final component absorbs any remaining bytes as an opaque body.
		if i == count-1 {
			c.Body = append([]byte(nil), rest...)
			rest = nil
		}
		r.Components = append(r.Components, c)
	}
	return r, nil
}

// SimpleResponse builds the minimal reachability response: a single IPX-component
// record with an empty body. This is what ClassicStack's responder returns and what
// a reachability ping needs to confirm the host is alive.
func SimpleResponse() Response {
	return Response{Components: []Component{{Type: CompIPX}}}
}
