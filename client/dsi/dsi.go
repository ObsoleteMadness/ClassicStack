// Package dsi is the client-side DSI (Data Stream Interface) session: it opens an
// AFP-over-TCP session, runs Command/Write exchanges, and keeps the session alive — all
// over a plain net.Conn. It is the TCP counterpart of client/asp (ASP-over-DDP): both
// implement client/afp.Session (structurally — this package does not import
// client/afp, to keep the dependency one-directional), so client/afp's command
// plumbing does not care which transport carried the session.
//
// DSI session flow (core/protocol/dsi; spec/21-dsi.md):
//   - GetStatus, on the fresh connection, needs no session: returns the FPGetSrvrInfo
//     block used to negotiate the AFP version/UAM (client/afp.LoginNegotiated).
//   - OpenSession establishes the session; nothing else is negotiated client-side.
//   - Command / Write carry AFP command blocks; Write's block already has its bulk
//     data spliced on (header + data concatenated) — the same shape the AFP command
//     core expects regardless of which DSI command carried it.
//   - The server may send an unsolicited Attention or Tickle at any time. A background
//     read loop demuxes these from solicited replies by RequestID, so a push arriving
//     while a Command/Write is in flight never stalls it (the ASP transport gets this
//     for free from DDP's packet multiplexing; a TCP byte stream needs it explicit).
//   - Close tears down the session and the TCP connection.
//
// Ring: CLIENT.
package dsi

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"

	aspclient "github.com/ObsoleteMadness/ClassicStack/client/asp"
	dsiproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/dsi"
)

// ErrSessionClosed is returned by Command/Write after the session is closed or the
// connection dies. It is client/asp's own sentinel, reused here (rather than a second,
// distinct error) so client/afp's errors.Is(err, aspclient.ErrSessionClosed) reconnect
// check works identically for either transport.
var ErrSessionClosed = aspclient.ErrSessionClosed

// maxMessage caps a single DSI data block at 16 MiB — well above any real AFP
// command/write reply — so a malformed or hostile DataLen cannot drive an unbounded
// allocation.
const maxMessage = 16 << 20

// frame is one decoded inbound DSI message: header plus its data block.
type frame struct {
	hdr  dsiproto.Header
	data []byte
}

// Session is an open DSI session to one AFP server.
type Session struct {
	conn net.Conn

	writeMu sync.Mutex // serializes writes; one requester goroutine per AFP call site in practice
	nextID  uint32     // atomic; wraps into RequestID via uint16 cast

	pendingMu sync.Mutex
	pending   map[uint16]chan frame

	attnMu      sync.Mutex
	onAttention func(code uint16)

	stopOnce sync.Once
	closed   chan struct{}
}

// Dial opens conn (already net.Dial'd by the caller) as a DSI session: it runs
// GetStatus (returning the FPGetSrvrInfo block for version/UAM negotiation, mirroring
// aspclient.GetStatus) then OpenSession, and starts the background read loop. On any
// failure it closes conn and returns the error.
func Dial(conn net.Conn) (status []byte, sess *Session, err error) {
	s := &Session{conn: conn, pending: make(map[uint16]chan frame), closed: make(chan struct{})}
	go s.readLoop()

	status, _, err = s.exchange(dsiproto.GetStatus, nil)
	if err != nil {
		_ = s.Close()
		return nil, nil, fmt.Errorf("dsi: GetStatus: %w", err)
	}
	if _, _, err := s.exchange(dsiproto.OpenSession, nil); err != nil {
		_ = s.Close()
		return nil, nil, fmt.Errorf("dsi: OpenSession: %w", err)
	}
	return status, s, nil
}

// Command runs one AFP command block over a DSICommand exchange.
func (s *Session) Command(block []byte) (reply []byte, result int32, err error) {
	return s.exchange(dsiproto.Command, block)
}

// CommandMax is Command with a reply-size budget — meaningless on a stream transport
// (TCP reassembles the whole reply regardless), so maxResp is ignored; kept only to
// satisfy client/afp.Session's shape.
func (s *Session) CommandMax(block []byte, _ int) (reply []byte, result int32, err error) {
	return s.exchange(dsiproto.Command, block)
}

// Write runs a DSIWrite exchange: header (the fixed-length AFP write command header)
// and data (the bulk bytes) are concatenated into one block, matching what the AFP
// command core expects on either transport (core/service/afp/conn.go).
func (s *Session) Write(header, data []byte) (reply []byte, result int32, err error) {
	block := make([]byte, 0, len(header)+len(data))
	block = append(block, header...)
	block = append(block, data...)
	return s.exchange(dsiproto.Write, block)
}

// SetAttentionHandler installs the callback the read loop invokes when the server
// sends an unsolicited Attention (message-waiting, server-going-down).
func (s *Session) SetAttentionHandler(h func(code uint16)) {
	s.attnMu.Lock()
	s.onAttention = h
	s.attnMu.Unlock()
}

// Close tears down the session: no explicit CloseSession handshake is sent (the
// connection close is itself the signal, mirroring client/smb's TCP transport) —
// waiting on a possibly-dead peer to acknowledge a goodbye would only delay teardown.
func (s *Session) Close() error {
	s.stopOnce.Do(func() { close(s.closed) })
	return s.conn.Close()
}

// exchange sends one DSI request and blocks for its matching reply (by RequestID), or
// until the session closes. The AFP/DSI result code comes from the reply header's
// ErrorOffset field, not the payload — see core/protocol/dsi and spec/21-dsi.md.
func (s *Session) exchange(cmd uint8, block []byte) (data []byte, result int32, err error) {
	select {
	case <-s.closed:
		return nil, 0, ErrSessionClosed
	default:
	}

	id := uint16(atomic.AddUint32(&s.nextID, 1))
	ch := make(chan frame, 1)
	s.pendingMu.Lock()
	s.pending[id] = ch
	s.pendingMu.Unlock()
	defer func() {
		s.pendingMu.Lock()
		delete(s.pending, id)
		s.pendingMu.Unlock()
	}()

	h := dsiproto.Header{Flags: dsiproto.Request, Command: cmd, RequestID: id, DataLen: uint32(len(block))}
	s.writeMu.Lock()
	_, werr := s.conn.Write(h.Marshal())
	if werr == nil && len(block) > 0 {
		_, werr = s.conn.Write(block)
	}
	s.writeMu.Unlock()
	if werr != nil {
		s.fail()
		return nil, 0, ErrSessionClosed
	}

	select {
	case f := <-ch:
		return f.data, int32(f.hdr.ErrorOffset), nil
	case <-s.closed:
		return nil, 0, ErrSessionClosed
	}
}

// readLoop is the session's single reader: it decodes each inbound DSI frame and
// either dispatches it as an unsolicited Attention, silently drops a Tickle (no reply
// needed, whichever direction it travels — mirrors ASP's SPTickle), or delivers it to
// the exchange call waiting on that RequestID. It runs until the connection errors,
// at which point every in-flight (and future) exchange unblocks via s.closed.
func (s *Session) readLoop() {
	hdrBuf := make([]byte, dsiproto.HeaderSize)
	for {
		if _, err := io.ReadFull(s.conn, hdrBuf); err != nil {
			s.fail()
			return
		}
		var h dsiproto.Header
		if !h.Unmarshal(hdrBuf) {
			s.fail()
			return
		}
		if h.DataLen > maxMessage {
			s.fail()
			return
		}
		var data []byte
		if h.DataLen > 0 {
			data = make([]byte, h.DataLen)
			if _, err := io.ReadFull(s.conn, data); err != nil {
				s.fail()
				return
			}
		}

		switch {
		case h.Command == dsiproto.Attention && h.Flags == dsiproto.Request:
			s.dispatchAttention(data)
		case h.Command == dsiproto.Tickle:
			// no-op: nothing to deliver, no reply expected.
		default:
			s.deliver(h, data)
		}
	}
}

// deliver hands one decoded reply to the exchange call waiting on its RequestID. A
// RequestID with no waiter (already timed out and abandoned, or a stray duplicate) is
// silently dropped.
func (s *Session) deliver(h dsiproto.Header, data []byte) {
	s.pendingMu.Lock()
	ch, ok := s.pending[h.RequestID]
	s.pendingMu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- frame{hdr: h, data: data}:
	default:
	}
}

// dispatchAttention decodes the 2-byte big-endian attention code (Inside AppleTalk's
// AspAttnMsg shape, which DSI's Attention payload mirrors) and invokes the installed
// handler, if any.
func (s *Session) dispatchAttention(data []byte) {
	var code uint16
	if len(data) >= 2 {
		code = binary.BigEndian.Uint16(data[:2])
	}
	s.attnMu.Lock()
	h := s.onAttention
	s.attnMu.Unlock()
	if h != nil {
		h(code)
	}
}

// fail marks the session dead exactly once, unblocking every exchange call currently
// waiting (via s.closed) and every future one (which checks s.closed up front).
func (s *Session) fail() {
	s.stopOnce.Do(func() { close(s.closed) })
}
