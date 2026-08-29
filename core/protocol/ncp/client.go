package ncp

// client.go holds the CLIENT-direction NCP wire DTOs: a Requester that marshals the
// request packets a NetWare workstation (NETx/VLM shell) sends and parses the reply
// bodies the server returns. It is the mirror of the server-direction framing in
// ncp.go (RequestHeader.Unmarshal / ReplyHeader.Marshal): here the client MARSHALS a
// RequestHeader ahead of a function body and PARSES a ReplyHeader ahead of a reply
// body. Wire-format only — no I/O, no connection state (the transport in client/ncp
// owns the socket and the learned server address).
//
// The request byte layouts mirror exactly what core/service/ncp parses (fileio.go /
// handlers.go / dispatch.go), which in turn follow mars_nwe's nwconn.c dispatch — so
// a request this Requester builds round-trips against the ClassicStack server and a
// real NetWare 3.x server alike. All NCP-header multi-byte fields are BIG-ENDIAN;
// function-body multi-byte fields are big-endian too (mars_nwe's U16/U32 in the file
// calls), except the name-space family (0x57), which is little-endian (namespace.go).
//
// Reference: Novell NCP; mars_nwe (Martin Stover) nwconn.c/nwbind.c; Linux ncpfs
// (Volker Lendecke). Constants and framing attributed to those works (CLAUDE.md #7).

import (
	"errors"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// Requester marshals client→server NCP packets, stamping the per-connection sequence
// and the assigned connection number into every request header. One Requester drives
// one service connection: the transport creates it, runs CreateConnection to learn the
// connection number, then builds each function request through it. It is NOT
// goroutine-safe; the client serialises requests per connection (one in flight).
type Requester struct {
	// Conn is the service connection number the server assigned in the
	// CreateConnection reply; 0 until then (create/destroy carry it regardless).
	Conn uint16
	// Task is the client task number stamped on each request. NetWare shells use the
	// DOS task; any stable value works for a single-threaded file client.
	Task uint8

	seq uint8 // per-connection sequence, bumped before each request (wraps at 256)
}

// NextSeq advances and returns the request sequence number. NetWare sequences each
// request on a connection; the server echoes it in the reply, and the client matches
// the reply to the request by (sequence, connection). It wraps naturally at 256.
func (r *Requester) NextSeq() uint8 {
	r.seq++
	return r.seq
}

// ResetSeq resets the request sequence to 0 so the NEXT request carries sequence 1. A
// real NetWare server assigns the service connection on CreateConnection and then expects
// the connection's request sequence to restart at 1 (ncpfs sets conn->sequence = 0 right
// after the allocate-slot reply). CreateConnection itself is sequence-exempt on the
// server, so the client must reset here once the connection is assigned — otherwise the
// first post-create request arrives at sequence 2 (Create consumed 1) and the server,
// waiting for sequence 1, silently drops it and every request after.
func (r *Requester) ResetSeq() { r.seq = 0 }

// marshalRequest prepends the 6-byte NCP request header (type, sequence, conn-low,
// task, conn-high, function) to body and returns the whole packet. typ is TypeRequest
// for an ordinary function call. The sequence is bumped here so every packet a
// Requester emits carries a fresh sequence.
func (r *Requester) marshalRequest(fn uint8, body []byte) []byte {
	seq := r.NextSeq()
	out := make([]byte, 0, requestHeaderLen+1+len(body))
	out = bp.AppendBE16(out, TypeRequest)
	out = append(out, seq, byte(r.Conn), r.Task, byte(r.Conn>>8), fn)
	return append(out, body...)
}

// marshalControl builds a connection-control packet (CreateConnection /
// DestroyConnection) — these carry a request type but NO function byte or body. The
// sequence is bumped so the reply matches.
func (r *Requester) marshalControl(typ uint16) []byte {
	seq := r.NextSeq()
	out := bp.AppendBE16(nil, typ)
	return append(out, seq, byte(r.Conn), r.Task, byte(r.Conn>>8))
}

// CreateConnection builds the TypeCreateConnection packet (0x1111) — the first packet
// of a session. The server allocates a service connection and returns its number in
// the reply header's connection bytes; ParseReply surfaces it so the caller records it
// on the Requester.
func (r *Requester) CreateConnection() []byte { return r.marshalControl(TypeCreateConnection) }

// DestroyConnection builds the TypeDestroyConnection packet (0x5555) — the client
// releasing its service connection at session end.
func (r *Requester) DestroyConnection() []byte { return r.marshalControl(TypeDestroyConnection) }

// Request builds an ordinary NCP request (TypeRequest) for function fn with body.
// Callers with a purpose-built builder below rarely need this directly; it is the seam
// for a function the typed builders do not yet cover.
func (r *Requester) Request(fn uint8, body []byte) []byte { return r.marshalRequest(fn, body) }

// --- reply parsing ---

var (
	// ErrShortReply is returned by ParseReply when the buffer is shorter than an NCP
	// reply header.
	ErrShortReply = errors.New("ncp: reply shorter than NCP header")
)

// ReplyPacket is a parsed server→client NCP reply: the header fields the client acts on
// plus the function-specific Body (everything after the 8-byte header). CompletionCode
// is the success/error byte; a non-zero value is the server's failure return. (Named
// ReplyPacket to avoid colliding with the server-direction Reply constructor in ncp.go.)
type ReplyPacket struct {
	Type             uint16
	SequenceNumber   uint8
	Connection       uint16 // reassembled conn-low/conn-high (the assigned number on Create)
	TaskNumber       uint8
	CompletionCode   uint8
	ConnectionStatus uint8
	Body             []byte
}

// ParseReply decodes one NCP reply packet (the IPX payload). The Body slice aliases b.
// It returns ErrShortReply on a truncated header. The caller checks CompletionCode
// before trusting Body.
func ParseReply(b []byte) (*ReplyPacket, error) {
	if len(b) < ReplyHeaderLen {
		return nil, ErrShortReply
	}
	rep := &ReplyPacket{
		Type:             uint16(b[0])<<8 | uint16(b[1]),
		SequenceNumber:   b[2],
		Connection:       uint16(b[3]) | uint16(b[5])<<8,
		TaskNumber:       b[4],
		CompletionCode:   b[6],
		ConnectionStatus: b[7],
	}
	rep.Body = b[ReplyHeaderLen:]
	return rep, nil
}

// OK reports whether the reply completed successfully (CompletionSuccess).
func (r *ReplyPacket) OK() bool { return r.CompletionCode == CompletionSuccess }
