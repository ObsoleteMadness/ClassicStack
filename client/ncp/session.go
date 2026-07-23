// Package ncp is the NetWare Core Protocol (NCP) file client: it drives one service
// connection to a NetWare 3.x bindery file server (ClassicStack's own, or mars_nwe, or
// a real server) over IPX socket 0x0451 through the client-direction codec
// (core/protocol/ncp) and presents a mounted volume as an fs.FileSystem — the same
// interface an AFP/SMB/local share exposes, so client/xfer and cmd/csfs drive a remote
// NetWare volume identically. NCP has no native resource fork, so client.Connect wraps
// this base with the AppleDouble fork backend, which reads/writes the server's own
// "._NAME" sidecars as ordinary 8.3 files over the DOS name space.
//
// Session flow (mirroring a NETx/VLM shell's attach): CreateConnection → Negotiate
// Buffer Size → Login (cleartext, guest when unnamed) → Get Volume Number → Allocate
// Permanent Directory Handle at the volume root. File operations then carry the dir
// handle + a relative 8.3 path; the transport addresses the learned server node.
//
// Ring: CLIENT.
package ncp

import (
	"fmt"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/client/trace"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ncp"
)

// ncpTrace narrates the NCP attach flow at log.Trace through the shared client/trace
// sink, so `csfs -v` shows CreateConnection / NegotiateBuffer / Login / GetVolumeNumber /
// AllocDirHandle alongside every other transport's trace.
var ncpTrace = trace.Logger("ncp")

// ncptracef narrates one NCP wire-trace line at log.Trace (no-op unless -v is on).
func ncptracef(format string, args ...any) {
	if !ncpTrace.Enabled(log.Trace) {
		return
	}
	ncpTrace.Log0(log.Trace, fmt.Sprintf(format, args...))
}

// clientTask is the NetWare task number stamped on every request. A single-threaded
// file client uses one stable task; the server echoes it and does not key state on it.
const clientTask uint8 = 1

// clientBufferSize is the read/write buffer size the client proposes in Negotiate
// Buffer Size. The server caps it to its own maximum (1024 for ClassicStack/mars_nwe
// over Ethernet); the accepted value bounds each Read/Write so a reply fits one IPX
// datagram.
const clientBufferSize uint16 = 1024

// defaultDrive is the DOS drive letter byte an Allocate Directory Handle request
// carries. NetWare maps a handle to a drive; a file client that never issues DOS
// drive-relative paths can send any value — 0 ("no drive") is the neutral choice
// mars_nwe accepts.
const defaultDrive uint8 = 0

// Transport is one NCP service connection as the client sees it: send a whole NCP
// request packet (starting at the 6-byte NCP header) and get the whole reply packet
// back, blocking until it arrives. The framing (IPX datagram encapsulation, the
// learned server node) lives in the implementation; the session only ever sees
// complete NCP packets. It mirrors the SMB client's Transport seam.
type Transport interface {
	// Send writes one NCP request and returns the matching reply packet. The client
	// serialises requests per connection (one in flight), so a strict request→reply
	// transport is sufficient; the transport correlates by (sequence, connection).
	Send(req []byte) (resp []byte, err error)
	// MaxPayload is the largest reply-body byte count the transport can carry back in
	// one exchange (one IPX datagram, no reassembly), used to bound Read sizes so a
	// read reply never overflows a datagram.
	MaxPayload() int
	Close() error
}

// Session is an attached NCP connection with one volume mounted (one dir handle at its
// root). It owns the Transport and the per-connection Requester (which stamps the
// sequence and connection number). All requests are serialised so the Requester's
// sequence and the request→reply transport stay consistent.
type Session struct {
	tr        Transport
	volume    string
	volNumber uint8
	rootDir   uint8  // permanent dir handle bound to the volume root
	rwBuffer  uint16 // negotiated read/write buffer size

	mu   sync.Mutex
	req  proto.Requester
	conn uint16
}

// DialParams carries what Open needs beyond the transport: the volume to mount and the
// credentials (empty user = guest login).
type DialParams struct {
	Volume   string
	User     string
	Password string
}

// Open runs the NCP attach flow over tr — CreateConnection, Negotiate Buffer Size,
// Login, Get Volume Number, Allocate Permanent Directory Handle at the volume root —
// and returns a Session with the volume mounted. Credentials are sent cleartext (empty
// = guest).
func Open(tr Transport, p DialParams) (*Session, error) {
	s := &Session{tr: tr, volume: p.Volume, req: proto.Requester{Task: clientTask}}

	// 1. CreateConnection — the server allocates a service connection and returns its
	// number, which every later request header carries.
	ncptracef("CreateConnection")
	if err := s.createConnection(); err != nil {
		return nil, err
	}
	ncptracef("connection %d assigned", s.conn)
	// 2. Negotiate Buffer Size — agree the max read/write packet so a reply fits one
	// datagram. A failure here is non-fatal (older servers may not answer); fall back
	// to the proposed size.
	s.negotiateBuffer()
	ncptracef("NegotiateBuffer → %d bytes", s.rwBuffer)
	// 3. Login — cleartext (guest when the user is empty).
	ncptracef("Login user=%q", p.User)
	if err := s.login(p.User, p.Password); err != nil {
		s.destroyConnection()
		return nil, err
	}
	// 4. Get Volume Number — resolve the volume name to its number.
	if err := s.getVolumeNumber(); err != nil {
		s.destroyConnection()
		return nil, err
	}
	ncptracef("volume %q resolved", p.Volume)
	// 5. Allocate a permanent directory handle at the volume root ("VOL:"), the anchor
	// every file operation resolves its relative path against.
	if err := s.allocRootHandle(); err != nil {
		s.destroyConnection()
		return nil, err
	}
	ncptracef("root directory handle allocated — volume mounted")
	return s, nil
}

// createConnection sends TypeCreateConnection and records the assigned connection
// number on the Requester so every later request carries it.
func (s *Session) createConnection() error {
	resp, err := s.tr.Send(s.req.CreateConnection())
	if err != nil {
		return fmt.Errorf("ncp: create connection: %w", err)
	}
	rep, err := proto.ParseReply(resp)
	if err != nil {
		return fmt.Errorf("ncp: create connection: %w", err)
	}
	if !rep.OK() {
		return fmt.Errorf("ncp: create connection refused (completion 0x%02X)", rep.CompletionCode)
	}
	s.conn = rep.Connection
	s.req.Conn = rep.Connection
	return nil
}

// negotiateBuffer sends Negotiate Buffer Size and records the accepted read/write
// buffer. A transport/parse error leaves rwBuffer at the proposed size (best effort).
func (s *Session) negotiateBuffer() {
	s.rwBuffer = clientBufferSize
	resp, err := s.tr.Send(s.req.BuildNegotiateBuffer(clientBufferSize))
	if err != nil {
		return
	}
	rep, err := proto.ParseReply(resp)
	if err != nil || !rep.OK() {
		return
	}
	if accepted, err := proto.ParseNegotiateBuffer(rep.Body); err == nil && accepted >= 512 {
		s.rwBuffer = accepted
	}
}

// login sends the cleartext Login To File Server. An empty user logs in as guest,
// which the server always grants.
func (s *Session) login(user, password string) error {
	resp, err := s.tr.Send(s.req.BuildLogin(user, password))
	if err != nil {
		return fmt.Errorf("ncp: login: %w", err)
	}
	rep, err := proto.ParseReply(resp)
	if err != nil {
		return fmt.Errorf("ncp: login: %w", err)
	}
	if !rep.OK() {
		return fmt.Errorf("ncp: login denied for %q (completion 0x%02X)", user, rep.CompletionCode)
	}
	return nil
}

// getVolumeNumber resolves the mounted volume's name to its number.
func (s *Session) getVolumeNumber() error {
	resp, err := s.tr.Send(s.req.BuildGetVolumeNumber(s.volume))
	if err != nil {
		return fmt.Errorf("ncp: get volume number %q: %w", s.volume, err)
	}
	rep, err := proto.ParseReply(resp)
	if err != nil {
		return fmt.Errorf("ncp: get volume number %q: %w", s.volume, err)
	}
	if !rep.OK() {
		return fmt.Errorf("ncp: volume %q not found (completion 0x%02X)", s.volume, rep.CompletionCode)
	}
	n, err := proto.ParseVolumeNumber(rep.Body)
	if err != nil {
		return fmt.Errorf("ncp: get volume number %q: %w", s.volume, err)
	}
	s.volNumber = n
	return nil
}

// allocRootHandle allocates a permanent directory handle at the volume root ("VOL:"),
// the anchor for every relative file path.
func (s *Session) allocRootHandle() error {
	root := s.volume + ":"
	resp, err := s.tr.Send(s.req.BuildAllocDirHandle(0, defaultDrive, root))
	if err != nil {
		return fmt.Errorf("ncp: allocate root handle: %w", err)
	}
	rep, err := proto.ParseReply(resp)
	if err != nil {
		return fmt.Errorf("ncp: allocate root handle: %w", err)
	}
	if !rep.OK() {
		return fmt.Errorf("ncp: allocate root handle for %q failed (completion 0x%02X)", root, rep.CompletionCode)
	}
	dh, err := proto.ParseDirHandle(rep.Body)
	if err != nil {
		return fmt.Errorf("ncp: allocate root handle: %w", err)
	}
	s.rootDir = dh.Handle
	return nil
}

// command serialises one request/response exchange: build the request through fn
// (which sees the current Requester state), send it, parse the reply header, and
// return the reply (the caller reads its Body). It maps a non-success completion to an
// ncpError the fs layer translates. Holding the mutex across the exchange keeps the
// Requester's sequence and the request→reply transport consistent.
func (s *Session) command(name string, build func(r *proto.Requester) []byte) (*proto.ReplyPacket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	req := build(&s.req)
	resp, err := s.tr.Send(req)
	if err != nil {
		return nil, fmt.Errorf("ncp: %s: %w", name, err)
	}
	rep, err := proto.ParseReply(resp)
	if err != nil {
		return nil, fmt.Errorf("ncp: %s: %w", name, err)
	}
	if !rep.OK() {
		return rep, &ncpError{op: name, completion: rep.CompletionCode}
	}
	return rep, nil
}

// MaxPayload is the largest read/write payload the client issues, bounded by the
// negotiated buffer and the transport's datagram ceiling.
func (s *Session) MaxPayload() int {
	max := int(s.rwBuffer)
	if tp := s.tr.MaxPayload(); tp > 0 && tp < max {
		max = tp
	}
	if max <= 0 {
		max = int(clientBufferSize)
	}
	return max
}

// destroyConnection releases the service connection (best effort, used on a failed
// Open and at Close).
func (s *Session) destroyConnection() {
	_, _ = s.tr.Send(s.req.DestroyConnection())
}

// Close tears the connection down: deallocate the root handle, DestroyConnection, then
// close the transport. Teardown-message errors are ignored (best effort).
func (s *Session) Close() error {
	s.mu.Lock()
	if s.rootDir != 0 {
		_, _ = s.tr.Send(s.req.BuildDeallocDirHandle(s.rootDir))
	}
	_, _ = s.tr.Send(s.req.DestroyConnection())
	s.mu.Unlock()
	return s.tr.Close()
}
