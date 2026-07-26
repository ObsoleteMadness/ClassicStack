package smb

import (
	"fmt"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/client/trace"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// smbTrace narrates the transport-agnostic SMB session steps (NEGOTIATE / SESSION_SETUP /
// TREE_CONNECT) at log.Trace through the shared client/trace sink, so `csfs -v` shows the
// SMB handshake regardless of which carrier (IPX/NBIPX/NBF/TCP) it rides.
var smbTrace = trace.Logger("smb")

// smbtracef narrates one SMB wire-trace line at log.Trace (no-op unless -v is on).
func smbtracef(format string, args ...any) {
	if !smbTrace.Enabled(log.Trace) {
		return
	}
	smbTrace.Log0(log.Trace, fmt.Sprintf(format, args...))
}

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
	tr           Transport
	unicode      bool
	maxIO        int    // largest READ_ANDX/WRITE_ANDX payload (bounded by the transport)
	negMaxBuffer uint32 // server MaxBufferSize from NEGOTIATE; the SESSION_SETUP MaxBuffer
	dialect      string // the dialect selected at NEGOTIATE (for display)

	mu      sync.Mutex
	builder proto.Builder

	// noPathInfo is set once a server rejects TRANS2 QUERY_PATH_INFORMATION as unsupported
	// (a Win9x file share answers "invalid function"), so Stat stops issuing it and relies
	// on the legacy QUERY_INFORMATION alone. Guarded by mu.
	noPathInfo bool
}

// PathInfoUnsupported reports whether this session's server has rejected TRANS2
// QUERY_PATH_INFORMATION as unsupported.
func (s *Session) PathInfoUnsupported() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.noPathInfo
}

// MarkPathInfoUnsupported records that the server does not support TRANS2
// QUERY_PATH_INFORMATION, so the client stops issuing it for the rest of the session.
func (s *Session) MarkPathInfoUnsupported() {
	s.mu.Lock()
	s.noPathInfo = true
	s.mu.Unlock()
}

// Dialect returns the SMB dialect the server selected at NEGOTIATE (e.g. "NT LM 0.12").
func (s *Session) Dialect() string { return s.dialect }

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
	s, err := establishSession(tr, p)
	if err != nil {
		return nil, err
	}

	// 3. TREE_CONNECT_ANDX — mount the share, obtain a TID.
	if err := s.treeConnect(p.ServerName, p.Share); err != nil {
		return nil, err
	}
	return s, nil
}

// establishSession runs NEGOTIATE + SESSION_SETUP_ANDX (no tree connect) and returns the
// authenticated session, shared by Open (which then mounts a disk share) and OpenIPC
// (which connects the IPC$ pipe for RAP enumeration).
func establishSession(tr Transport, p DialParams) (*Session, error) {
	s := &Session{tr: tr, builder: proto.Builder{PID: clientPID}}

	// Bound every reply to what the transport can carry back in one exchange (see Open).
	s.applyTransportLimits(tr.MaxResponse())

	// 1. NEGOTIATE — no UID/TID yet; select the dialect. The client stays on ANSI paths.
	neg, err := s.negotiate()
	if err != nil {
		return nil, err
	}
	s.unicode = false
	s.builder.Unicode = false
	s.dialect = neg.Dialect
	// Speak the server's status dialect: 32-bit NTSTATUS only when the server advertised
	// CAP_STATUS32, else DOS error codes (a Win9x server negotiates NT LM 0.12 WITHOUT
	// CAP_STATUS32 and silently drops an NT-status header).
	s.builder.NTStatus = neg.SupportsNTStatus()
	// Echo the server's SessionKey; a Win9x server drops a setup carrying 0.
	s.builder.SessionKey = neg.SessionKey
	// Advertise no more than the server's own MaxBufferSize.
	s.negMaxBuffer = neg.MaxBuffer
	// Clamp READ_ANDX/WRITE_ANDX to the server's negotiated MaxBufferSize. The transport
	// budget (applyTransportLimits, above) only bounds what the WIRE can carry — for a
	// reassembling carrier (NBF/NBT/TCP) that is huge, so maxIO stayed at defaultMaxIO
	// (12 KiB). But a Win9x server advertises a small MaxBufferSize (observed: Win98 =
	// 2920) and rejects a READ_ANDX asking for more than that with ERRDOS/87 "invalid
	// parameter" (status 0x00570001). Bound maxIO to the server's buffer net of reply
	// overhead so the read fits one server message.
	if s.negMaxBuffer > 0 {
		if bufCap := int(s.negMaxBuffer) - smbReplyOverhead; bufCap > 0 && bufCap < s.maxIO {
			s.maxIO = bufCap
		}
	}

	// 2. SESSION_SETUP_ANDX — obtain a UID (guest or named).
	if err := s.sessionSetup(p.User, p.Password, p.Domain); err != nil {
		return nil, err
	}
	return s, nil
}

// OpenIPC runs NEGOTIATE + SESSION_SETUP_ANDX and connects the server's IPC$ pipe tree,
// returning a Session ready for a RAP transaction (EnumShares) — the browse path for a
// URI that names a server but no share.
func OpenIPC(tr Transport, p DialParams) (*Session, error) {
	s, err := establishSession(tr, p)
	if err != nil {
		return nil, err
	}
	s.builder.NextMID()
	smbtracef("TREE_CONNECT_ANDX \\\\%s\\IPC$ (pipe)", p.ServerName)
	resp, err := s.tr.Send(s.builder.BuildTreeConnectIPC(p.ServerName))
	if err != nil {
		return nil, fmt.Errorf("smb: tree connect IPC$: %w", err)
	}
	tid, err := proto.ParseTreeConnect(resp)
	if err != nil {
		return nil, fmt.Errorf("smb: tree connect IPC$: %w", err)
	}
	s.builder.TID = tid
	smbtracef("TREE_CONNECT_ANDX ok — TID %d (IPC$)", tid)
	return s, nil
}

// EnumShares runs a RAP NetShareEnum over the connected IPC$ pipe and returns the server's
// share list. The session must have been opened with OpenIPC.
func (s *Session) EnumShares() ([]proto.ShareInfo, error) {
	smbtracef("NetShareEnum")
	resp, err := s.send(func(b *proto.Builder) []byte { return b.BuildNetShareEnum() })
	if err != nil {
		return nil, fmt.Errorf("smb: NetShareEnum: %w", err)
	}
	shares, err := proto.ParseNetShareEnum(resp)
	if err != nil {
		return nil, fmt.Errorf("smb: NetShareEnum: %w", err)
	}
	smbtracef("NetShareEnum ok — %d shares", len(shares))
	return shares, nil
}

// negotiate sends SMB_COM_NEGOTIATE and parses the selected dialect.
func (s *Session) negotiate() (proto.NegotiateResult, error) {
	// Use the session builder so the MID sequence is monotonic across the whole session
	// (NEGOTIATE=1, SESSION_SETUP=2, …): a connectionless transport correlates responses
	// by command+MID, so NEGOTIATE and SESSION_SETUP must not share a MID. NEGOTIATE
	// still carries no UID/TID and offers the ANSI dialect list (Unicode not yet set).
	s.builder.NextMID()
	smbtracef("NEGOTIATE")
	resp, err := s.tr.Send(s.builder.BuildNegotiate())
	if err != nil {
		return proto.NegotiateResult{}, fmt.Errorf("smb: negotiate: %w", err)
	}
	neg, err := proto.ParseNegotiate(resp)
	if err != nil {
		return proto.NegotiateResult{}, fmt.Errorf("smb: negotiate: %w", err)
	}
	smbtracef("NEGOTIATE ok — dialect %q", neg.Dialect)
	return neg, nil
}

// guestAccount is the account name sent when the caller supplies no username. A Win9x
// File & Print server (share-level) expects a NON-EMPTY account in SESSION_SETUP even for
// a null-password logon — the MS redirector sends its logged-on user; an empty account is
// rejected. "GUEST" is the conventional anonymous account name.
const guestAccount = "GUEST"

// sessionSetup sends SMB_COM_SESSION_SETUP_ANDX and records the granted UID on the
// builder so every later request carries it.
func (s *Session) sessionSetup(user, password, domain string) error {
	if user == "" {
		user = guestAccount
	}
	// Advertise the server's MaxBufferSize (bounded to what we can actually hold), never
	// more than it offered.
	maxBuf := uint16(clientMaxBuffer)
	if s.negMaxBuffer > 0 && s.negMaxBuffer < clientMaxBuffer {
		maxBuf = uint16(s.negMaxBuffer)
	}
	s.builder.NextMID()
	smbtracef("SESSION_SETUP_ANDX user=%q", user)
	resp, err := s.tr.Send(s.builder.BuildSessionSetup(user, password, domain, maxBuf))
	if err != nil {
		return fmt.Errorf("smb: session setup: %w", err)
	}
	res, err := proto.ParseSessionSetup(resp)
	if err != nil {
		return fmt.Errorf("smb: session setup: %w", err)
	}
	s.builder.UID = res.UID
	smbtracef("SESSION_SETUP_ANDX ok — UID %d", res.UID)
	return nil
}

// treeConnect sends SMB_COM_TREE_CONNECT_ANDX and records the granted TID on the
// builder.
func (s *Session) treeConnect(server, share string) error {
	if server == "" {
		server = "SERVER"
	}
	s.builder.NextMID()
	smbtracef("TREE_CONNECT_ANDX \\\\%s\\%s", server, share)
	resp, err := s.tr.Send(s.builder.BuildTreeConnect(server, share))
	if err != nil {
		return fmt.Errorf("smb: tree connect %q: %w", share, err)
	}
	tid, err := proto.ParseTreeConnect(resp)
	if err != nil {
		return fmt.Errorf("smb: tree connect %q: %w", share, err)
	}
	s.builder.TID = tid
	smbtracef("TREE_CONNECT_ANDX ok — TID %d (share mounted)", tid)
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
