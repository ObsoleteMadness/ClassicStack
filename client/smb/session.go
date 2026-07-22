package smb

import (
	"fmt"
	"sync"

	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// clientMaxBuffer is the largest response this client asks the server to send in one
// message (its SESSION_SETUP MaxBufferSize). 16 KiB matches the server's own
// negotiateMaxBufferSize, so a READ_ANDX never asks for more than one message carries.
const clientMaxBuffer = 0x4000

// clientPID is the process id stamped on every request header. Any stable non-zero
// value works; the server only requires PID+MID to be consistent within a transaction.
const clientPID = 0xFEFF

// defaultMaxIO is the largest single READ_ANDX / WRITE_ANDX the client issues over a
// reassembling transport (TCP/NBT), bounded well under clientMaxBuffer so each transfer
// fits one negotiated buffer. A datagram transport shrinks this further (applyTransportLimits).
const defaultMaxIO = 0x3000 // 12 KiB

// smbReplyOverhead is the fixed byte budget a response consumes before its payload: the
// 32-byte SMB header plus a generous allowance for a command's WordCount/ByteCount words
// (READ_ANDX response WCT=12, TRANS2 response WCT=10 + parameter/data offsets). Subtracted
// from a datagram transport's MaxResponse so the client never requests a payload that,
// once wrapped, overflows the single datagram the reply must fit in.
const smbReplyOverhead = 128

// Session is an authenticated SMB circuit with one share mounted (one TID). It owns the
// Transport, the negotiated UID, and the per-request Builder (which stamps UID/TID/MID
// and the Unicode charset). All requests on the circuit are serialised so the Builder's
// MID and the request→response transport stay consistent.
type Session struct {
	tr      Transport
	unicode bool
	maxIO   int // largest READ_ANDX/WRITE_ANDX payload (bounded by the transport)

	mu      sync.Mutex
	builder proto.Builder
}

// DialParams carries what Open needs beyond the transport: the credentials and the
// UNC target (server label + share). The server label is only used to build the
// TREE_CONNECT UNC path; the transport already reaches the right host.
type DialParams struct {
	ServerName string // NetBIOS/host label for the \\server\share UNC (cosmetic to the wire)
	Share      string
	User       string
	Password   string
	Domain     string
}

// Open runs the SMB session-establishment flow over tr — NEGOTIATE, SESSION_SETUP_ANDX,
// TREE_CONNECT_ANDX — and returns a Session with the share mounted. Credentials are sent
// cleartext (empty = guest).
//
// Path names are sent in the OEM/ANSI charset, never UTF-16, regardless of the
// negotiated dialect: this client targets the classic redirectors that speak
// direct-IPX/NBF/NBIPX SMB (DOS, WfW, OS/2), which are ANSI clients, and — decisively —
// a server share's FilenameCodec need only implement the ANSI wire encoding (the
// macroman-utf8 codec, for one, rejects UTF-16), so an ANSI request works against every
// share while a Unicode one does not. The Unicode session bit is therefore left clear;
// the server's per-request wireFor() keys off that bit and uses ANSI to match.
func Open(tr Transport, p DialParams) (*Session, error) {
	s := &Session{tr: tr, builder: proto.Builder{PID: clientPID}}

	// Bound every reply to what the transport can carry back in one exchange. A
	// connectionless transport (SMB over IPX) has no reassembly, so the whole response
	// must fit one datagram; the session caps TRANS2 MaxDataCount and READ/WRITE sizes
	// by the transport's MaxResponse (minus SMB + per-command overhead) so a directory
	// listing pages through FIND_NEXT2 and a large file reads in chunks rather than
	// asking for a reply too big to transmit. A stream transport reports a large cap, so
	// this is a no-op there beyond the existing maxIO ceiling.
	s.applyTransportLimits(tr.MaxResponse())

	// 1. NEGOTIATE — no UID/TID yet; select the dialect. The reply is parsed for
	// success/dialect but the client stays on ANSI paths (Unicode is left off) so it
	// interoperates with every share codec (see the doc comment above).
	if _, err := s.negotiate(); err != nil {
		return nil, err
	}
	s.unicode = false
	s.builder.Unicode = false

	// 2. SESSION_SETUP_ANDX — obtain a UID (guest or named).
	if err := s.sessionSetup(p.User, p.Password, p.Domain); err != nil {
		return nil, err
	}

	// 3. TREE_CONNECT_ANDX — mount the share, obtain a TID.
	if err := s.treeConnect(p.ServerName, p.Share); err != nil {
		return nil, err
	}
	return s, nil
}

// negotiate sends SMB_COM_NEGOTIATE and parses the selected dialect.
func (s *Session) negotiate() (proto.NegotiateResult, error) {
	// Use the session builder so the MID sequence is monotonic across the whole session
	// (NEGOTIATE=1, SESSION_SETUP=2, …): a connectionless transport correlates responses
	// by command+MID, so NEGOTIATE and SESSION_SETUP must not share a MID. NEGOTIATE
	// still carries no UID/TID and offers the ANSI dialect list (Unicode not yet set).
	s.builder.NextMID()
	resp, err := s.tr.Send(s.builder.BuildNegotiate())
	if err != nil {
		return proto.NegotiateResult{}, fmt.Errorf("smb: negotiate: %w", err)
	}
	neg, err := proto.ParseNegotiate(resp)
	if err != nil {
		return proto.NegotiateResult{}, fmt.Errorf("smb: negotiate: %w", err)
	}
	return neg, nil
}

// sessionSetup sends SMB_COM_SESSION_SETUP_ANDX and records the granted UID on the
// builder so every later request carries it.
func (s *Session) sessionSetup(user, password, domain string) error {
	s.builder.NextMID()
	resp, err := s.tr.Send(s.builder.BuildSessionSetup(user, password, domain, clientMaxBuffer))
	if err != nil {
		return fmt.Errorf("smb: session setup: %w", err)
	}
	res, err := proto.ParseSessionSetup(resp)
	if err != nil {
		return fmt.Errorf("smb: session setup: %w", err)
	}
	s.builder.UID = res.UID
	return nil
}

// treeConnect sends SMB_COM_TREE_CONNECT_ANDX and records the granted TID on the
// builder.
func (s *Session) treeConnect(server, share string) error {
	if server == "" {
		server = "SERVER"
	}
	s.builder.NextMID()
	resp, err := s.tr.Send(s.builder.BuildTreeConnect(server, share))
	if err != nil {
		return fmt.Errorf("smb: tree connect %q: %w", share, err)
	}
	tid, err := proto.ParseTreeConnect(resp)
	if err != nil {
		return fmt.Errorf("smb: tree connect %q: %w", share, err)
	}
	s.builder.TID = tid
	return nil
}

// send serialises one request/response exchange on the circuit: bump the MID, build the
// request through fn (which sees the current builder state), send, and return the raw
// response for the caller to parse. Holding the mutex across the whole exchange keeps
// the request→response transport and the builder's MID consistent.
func (s *Session) send(build func(b *proto.Builder) []byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.builder.NextMID()
	req := build(&s.builder)
	return s.tr.Send(req)
}

// applyTransportLimits sets the session's read/write and TRANS2 caps from the transport's
// per-exchange response ceiling. A reassembling transport (TCP/NBT) reports a huge value,
// so the caps fall back to the defaults; a connectionless datagram transport (SMB over
// IPX) reports one datagram's worth, so both caps shrink to fit — MaxTransactBytes bounds
// a FIND/QUERY reply and maxIO bounds a READ_ANDX reply, each net of SMB reply overhead.
func (s *Session) applyTransportLimits(maxResp int) {
	budget := maxResp - smbReplyOverhead
	s.maxIO = defaultMaxIO
	if budget > 0 && budget < s.maxIO {
		s.maxIO = budget
	}
	// TRANS2 MaxDataCount is a uint16; 0 means "no client cap" (stream transport). Only
	// clamp when the datagram budget is smaller than the uint16 max, so TCP stays uncapped.
	if budget > 0 && budget < 0xFFFF {
		s.builder.MaxTransactBytes = uint16(budget)
	}
}

// MaxIO is the largest READ_ANDX / WRITE_ANDX payload the client issues on this circuit,
// bounded by the transport (one datagram for SMB-over-IPX, the buffer ceiling for TCP).
func (s *Session) MaxIO() int {
	if s.maxIO <= 0 {
		return defaultMaxIO
	}
	return s.maxIO
}

// Unicode reports whether the session negotiated the Unicode charset (so a caller
// parsing FIND records knows which charset the names came in).
func (s *Session) Unicode() bool { return s.unicode }

// Close tears down the session: TREE_DISCONNECT, LOGOFF, then close the transport. Errors
// on the teardown messages are ignored (best-effort); the transport close is returned.
func (s *Session) Close() error {
	s.mu.Lock()
	if s.builder.TID != 0 {
		s.builder.NextMID()
		_, _ = s.tr.Send(s.builder.BuildTreeDisconnect())
	}
	if s.builder.UID != 0 {
		s.builder.NextMID()
		_, _ = s.tr.Send(s.builder.BuildLogoff())
	}
	s.mu.Unlock()
	return s.tr.Close()
}
