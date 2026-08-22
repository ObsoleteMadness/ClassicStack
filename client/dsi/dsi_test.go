package dsi

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	dsiproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/dsi"
)

// fakeServer is a minimal hand-rolled DSI peer for exercising the client in isolation:
// it answers GetStatus/OpenSession/Command directly and lets the test inject arbitrary
// extra frames (Tickle, Attention) at chosen moments to prove the client's read loop
// demuxes them correctly instead of mistaking them for a Command reply.
type fakeServer struct {
	conn net.Conn
}

func newFakeServer(t *testing.T) (client net.Conn, srv *fakeServer) {
	t.Helper()
	c, s := net.Pipe()
	t.Cleanup(func() { _ = c.Close(); _ = s.Close() })
	return c, &fakeServer{conn: s}
}

// These helpers run on a background goroutine in every test (the fake server side
// while the main goroutine drives the client synchronously), so they use
// t.Error/t.Errorf rather than t.Fatal/t.Fatalf — Fatal calls runtime.Goexit, which
// only stops the calling (non-test) goroutine and would silently strand the test
// instead of failing it (go vet's tests analyzer flags this).

func (s *fakeServer) readReq(t *testing.T) dsiproto.Header {
	t.Helper()
	hdrBuf := make([]byte, dsiproto.HeaderSize)
	if _, err := io.ReadFull(s.conn, hdrBuf); err != nil {
		t.Errorf("server read header: %v", err)
		return dsiproto.Header{}
	}
	var h dsiproto.Header
	if !h.Unmarshal(hdrBuf) {
		t.Error("server: bad header")
		return dsiproto.Header{}
	}
	if h.DataLen > 0 {
		buf := make([]byte, h.DataLen)
		if _, err := io.ReadFull(s.conn, buf); err != nil {
			t.Errorf("server read data: %v", err)
			return dsiproto.Header{}
		}
	}
	return h
}

func (s *fakeServer) reply(t *testing.T, reqID uint16, cmd uint8, errCode uint32, data []byte) {
	t.Helper()
	h := dsiproto.Header{Flags: dsiproto.Reply, Command: cmd, RequestID: reqID, ErrorOffset: errCode, DataLen: uint32(len(data))}
	if _, err := s.conn.Write(h.Marshal()); err != nil {
		t.Errorf("server write reply: %v", err)
		return
	}
	if len(data) > 0 {
		if _, err := s.conn.Write(data); err != nil {
			t.Errorf("server write data: %v", err)
		}
	}
}

// pushTickle sends an unsolicited Tickle (server->client keepalive), which the client
// must silently ignore rather than treating it as a reply to anything.
func (s *fakeServer) pushTickle(t *testing.T) {
	t.Helper()
	h := dsiproto.Header{Flags: dsiproto.Request, Command: dsiproto.Tickle}
	if _, err := s.conn.Write(h.Marshal()); err != nil {
		t.Errorf("server push tickle: %v", err)
	}
}

// pushAttention sends an unsolicited Attention carrying a 2-byte code, which the
// client must route to its installed handler without disturbing an in-flight Command.
func (s *fakeServer) pushAttention(t *testing.T, code uint16) {
	t.Helper()
	var data [2]byte
	binary.BigEndian.PutUint16(data[:], code)
	h := dsiproto.Header{Flags: dsiproto.Request, Command: dsiproto.Attention, DataLen: 2}
	if _, err := s.conn.Write(h.Marshal()); err != nil {
		t.Errorf("server push attention: %v", err)
		return
	}
	if _, err := s.conn.Write(data[:]); err != nil {
		t.Errorf("server push attention data: %v", err)
	}
}

// serveHandshake answers the GetStatus + OpenSession pair Dial always sends first.
func (s *fakeServer) serveHandshake(t *testing.T, status []byte) {
	t.Helper()
	h := s.readReq(t)
	if h.Command != dsiproto.GetStatus {
		t.Errorf("first request = %d, want GetStatus", h.Command)
		return
	}
	s.reply(t, h.RequestID, dsiproto.GetStatus, 0, status)

	h = s.readReq(t)
	if h.Command != dsiproto.OpenSession {
		t.Errorf("second request = %d, want OpenSession", h.Command)
		return
	}
	s.reply(t, h.RequestID, dsiproto.OpenSession, 0, nil)
}

func TestDialHandshake(t *testing.T) {
	clientConn, srv := newFakeServer(t)
	done := make(chan struct{})
	go func() { defer close(done); srv.serveHandshake(t, []byte("hello")) }()

	status, sess, err := Dial(clientConn)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer sess.Close()
	<-done
	if string(status) != "hello" {
		t.Fatalf("status = %q, want %q", status, "hello")
	}
}

func TestCommandRoundTrip(t *testing.T) {
	clientConn, srv := newFakeServer(t)
	go srv.serveHandshake(t, nil)
	_, sess, err := Dial(clientConn)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer sess.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		h := srv.readReq(t)
		if h.Command != dsiproto.Command {
			t.Errorf("command = %d, want Command", h.Command)
		}
		var wantResult int32 = -5000
		srv.reply(t, h.RequestID, dsiproto.Command, uint32(wantResult), []byte("reply-body"))
	}()

	reply, result, err := sess.Command([]byte("req-body"))
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	<-done
	if result != -5000 {
		t.Fatalf("result = %d, want -5000", result)
	}
	if string(reply) != "reply-body" {
		t.Fatalf("reply = %q", reply)
	}
}

// TestTickleAndAttentionDoNotStallCommand proves the async read loop correctly demuxes
// unsolicited server pushes (Tickle, Attention) that arrive INTERLEAVED with a
// Command's reply — the scenario a naive synchronous "read exactly one frame" client
// would deadlock or misparse.
func TestTickleAndAttentionDoNotStallCommand(t *testing.T) {
	clientConn, srv := newFakeServer(t)
	go srv.serveHandshake(t, nil)
	_, sess, err := Dial(clientConn)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer sess.Close()

	attnCh := make(chan uint16, 1)
	sess.SetAttentionHandler(func(code uint16) { attnCh <- code })

	done := make(chan struct{})
	go func() {
		defer close(done)
		h := srv.readReq(t)
		// Interleave a Tickle and an Attention BEFORE answering the Command — both
		// must be transparently absorbed by the client's read loop.
		srv.pushTickle(t)
		srv.pushAttention(t, 42)
		srv.reply(t, h.RequestID, dsiproto.Command, 0, []byte("ok"))
	}()

	reply, result, err := sess.Command([]byte("req"))
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	<-done
	if result != 0 || string(reply) != "ok" {
		t.Fatalf("reply=%q result=%d, want ok/0", reply, result)
	}
	select {
	case code := <-attnCh:
		if code != 42 {
			t.Fatalf("attention code = %d, want 42", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("attention handler was never called")
	}
}

func TestCommandAfterCloseReturnsErrSessionClosed(t *testing.T) {
	clientConn, srv := newFakeServer(t)
	go srv.serveHandshake(t, nil)
	_, sess, err := Dial(clientConn)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	_ = sess.Close()

	if _, _, err := sess.Command([]byte("x")); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("Command after Close: err = %v, want ErrSessionClosed", err)
	}
}

func TestWriteConcatenatesHeaderAndData(t *testing.T) {
	clientConn, srv := newFakeServer(t)
	go srv.serveHandshake(t, nil)
	_, sess, err := Dial(clientConn)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer sess.Close()

	done := make(chan struct{})
	var gotLen int
	go func() {
		defer close(done)
		h := srv.readReq(t)
		if h.Command != dsiproto.Write {
			t.Errorf("command = %d, want Write", h.Command)
		}
		gotLen = int(h.DataLen)
		srv.reply(t, h.RequestID, dsiproto.Write, 0, nil)
	}()

	header := []byte{1, 2, 3, 4}
	data := []byte("some file bytes")
	if _, _, err := sess.Write(header, data); err != nil {
		t.Fatalf("Write: %v", err)
	}
	<-done
	if gotLen != len(header)+len(data) {
		t.Fatalf("server saw DataLen=%d, want %d", gotLen, len(header)+len(data))
	}
}
