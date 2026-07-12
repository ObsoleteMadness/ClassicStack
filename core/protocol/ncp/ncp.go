// Package ncp holds the NetWare Core Protocol (NCP) request/reply framing — the
// wire-format DTOs the NetWare 3.x bindery file service exchanges over IPX socket
// 0x0451. Wire-format only: no I/O, no connection state (that lives in
// core/service/ncp).
//
// Ring: CORE (stdlib only, reflection-free). The request/reply headers are the
// classic NCP framing documented by Novell and re-implemented in mars_nwe and the
// Linux ncpfs client; field names follow those references. All multi-byte fields
// in the NCP header are BIG-ENDIAN (the 2-byte request type and the sequence are
// the only multi-byte header fields).
//
// Reference: Novell NCP; the canonical open-source implementations are
// mars_nwe (Martin Stover) and the Linux kernel ncpfs/ipx (Volker Lendecke et
// al). Constants and framing here are attributed to those works (CLAUDE.md #7).
package ncp

import "errors"

// Request-type values (the first two header bytes, big-endian). NCP multiplexes a
// few "verbs" at the framing layer ahead of the per-request function code.
const (
	// TypeCreateConnection (0x1111) — the client asks the server to allocate a
	// service connection (the first packet of a session). The reply carries the
	// assigned connection number in the header.
	TypeCreateConnection uint16 = 0x1111
	// TypeRequest (0x2222) — an ordinary NCP request carrying a function code.
	TypeRequest uint16 = 0x2222
	// TypeReply (0x3333) — a server reply to a TypeRequest (server→client only;
	// dropped on ingress).
	TypeReply uint16 = 0x3333
	// TypeDestroyConnection (0x5555) — the client releases its service connection.
	TypeDestroyConnection uint16 = 0x5555
	// TypePositiveAck (0x9999) — server "request being processed" keep-alive
	// (long operations). Emitted by the server only.
	TypePositiveAck uint16 = 0x9999
	// TypeBurst (0x7777) — NetWare Burst Mode (NCPB) packet. Out of scope for the
	// bindery file service; requests of this type are rejected.
	TypeBurst uint16 = 0x7777
)

// Completion codes (the reply header's CompletionCode byte). 0 == success; the
// rest are the common NetWare error returns the file service emits. Named from the
// reference implementations.
const (
	CompletionSuccess       uint8 = 0x00 // operation succeeded
	CompletionConnNotLogged uint8 = 0x7C // connection not logged in
	CompletionNoSuchObject  uint8 = 0xFC // bindery: no such object (mars_nwe -0xfc)
	CompletionNoSuchVolume  uint8 = 0x98 // volume does not exist (mars_nwe -0x98)
	CompletionInvalidConn   uint8 = 0x9B // bad connection number / station / dir handle
	CompletionBadStation    uint8 = 0xFD // bad station (target connection) number (mars_nwe 0xfd)
	CompletionNoFiles       uint8 = 0x9C // no more matching files (scan end)
	CompletionInvalidPath   uint8 = 0x9C // invalid path (shares 0x9C in NetWare)
	CompletionNoSuchFile    uint8 = 0xFF // file/dir not found (generic failure)
	CompletionFuncNotSupp   uint8 = 0xFB // requested function not supported
	CompletionLockFail      uint8 = 0xFE // lock / busy
	CompletionAccessDenied  uint8 = 0x8C // no privileges / access denied
	CompletionBadNameSpace  uint8 = 0xBF // invalid name space (mars_nwe's AFP-calls reply)
)

// Connection-status bits (the reply header's ConnectionStatus byte). Bit 0x40
// ("DOWN") tells the client the server is shutting the connection down; 0 is the
// normal "connection good" state.
const (
	ConnStatusGood uint8 = 0x00
	ConnStatusDown uint8 = 0x40
)

// requestHeaderLen is the fixed NCP request header length: type(2) seq(1)
// connLow(1) task(1) connHigh(1) = 6 bytes. The function code (and any
// subfunction/length) is the first payload byte(s), not part of the header.
const requestHeaderLen = 6

// ReplyHeaderLen is the fixed NCP reply header length: type(2) seq(1) connLow(1)
// task(1) connHigh(1) completion(1) connStatus(1) = 8 bytes. Exported so a
// transport can size a reply buffer ahead of the body.
const ReplyHeaderLen = 8

var (
	// ErrShort is returned by Unmarshal when the buffer is shorter than a header.
	ErrShort = errors.New("ncp: buffer shorter than NCP header")
)

// RequestHeader is the fixed prefix of every client→server NCP packet. The two
// connection bytes are split (low/high) for historical reasons; ConnectionNumber
// reassembles them. Function and the remaining bytes are the Body.
type RequestHeader struct {
	Type           uint16 // request type (TypeRequest, TypeCreateConnection, …)
	SequenceNumber uint8  // per-connection sequence; echoed in the reply
	ConnLow        uint8  // connection number low byte
	TaskNumber     uint8  // client task issuing the request
	ConnHigh       uint8  // connection number high byte
	// Function is the NCP function code (first body byte) for a TypeRequest; 0 for
	// create/destroy-connection which carry no function. Body is everything after
	// the function byte (function-specific arguments; for the 0x16/0x17/0x22
	// multiplexed functions it begins with subfunction-length + subfunction).
	Function uint8
	Body     []byte
}

// ConnectionNumber reassembles the split low/high connection bytes.
func (h *RequestHeader) ConnectionNumber() uint16 {
	return uint16(h.ConnLow) | uint16(h.ConnHigh)<<8
}

// UnmarshalRequest parses one NCP request packet (the IPX payload). The Body slice
// aliases b (the caller owns b for the dispatch lifetime); a create/destroy-
// connection packet has no Function/Body. Returns ErrShort on a truncated header.
func UnmarshalRequest(b []byte) (*RequestHeader, error) {
	if len(b) < requestHeaderLen {
		return nil, ErrShort
	}
	h := &RequestHeader{
		Type:           uint16(b[0])<<8 | uint16(b[1]),
		SequenceNumber: b[2],
		ConnLow:        b[3],
		TaskNumber:     b[4],
		ConnHigh:       b[5],
	}
	// Only an ordinary request carries a function code + arguments; the
	// create/destroy/ack verbs are framing-only.
	if h.Type == TypeRequest && len(b) > requestHeaderLen {
		h.Function = b[requestHeaderLen]
		h.Body = b[requestHeaderLen+1:]
	}
	return h, nil
}

// ReplyHeader is the fixed prefix of every server→client NCP packet. It echoes the
// request's sequence and connection, and adds the completion + connection-status
// bytes ahead of the function-specific Body.
type ReplyHeader struct {
	Type             uint16 // TypeReply (or TypeCreateConnection echoed on accept)
	SequenceNumber   uint8  // echoed from the request
	ConnLow          uint8  // assigned/echoed connection number low byte
	TaskNumber       uint8  // echoed from the request
	ConnHigh         uint8  // connection number high byte
	CompletionCode   uint8  // CompletionSuccess (0) or an error code
	ConnectionStatus uint8  // ConnStatusGood (0) or ConnStatusDown
}

// Reply builds a reply header echoing a request's sequence/task and carrying the
// supplied connection number and completion code (status defaults to good).
func Reply(req *RequestHeader, conn uint16, completion uint8) ReplyHeader {
	return ReplyHeader{
		Type:           TypeReply,
		SequenceNumber: req.SequenceNumber,
		ConnLow:        uint8(conn),
		TaskNumber:     req.TaskNumber,
		ConnHigh:       uint8(conn >> 8),
		CompletionCode: completion,
	}
}

// Marshal appends the wire form of the reply header (8 bytes) to dst and returns
// it (append-style → the caller appends the function-specific body afterwards).
func (h ReplyHeader) Marshal(dst []byte) []byte {
	dst = append(dst, byte(h.Type>>8), byte(h.Type))
	dst = append(dst, h.SequenceNumber, h.ConnLow, h.TaskNumber, h.ConnHigh)
	dst = append(dst, h.CompletionCode, h.ConnectionStatus)
	return dst
}
