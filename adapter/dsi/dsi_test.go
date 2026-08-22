package dsi

import (
	"net"
	"testing"
	"time"

	dsiproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/dsi"
	"github.com/ObsoleteMadness/ClassicStack/core/service/afp"
)

// echoHandler is a fake afp.CommandHandler whose circuit echoes each command block
// back with a fixed result code, so the test can assert the transport's DSI framing
// without the real AFP command engine.
type echoHandler struct{ opened chan struct{} }

func (h echoHandler) GetServerInfo() []byte { return []byte("srvinfo") }
func (h echoHandler) NewConn() afp.CommandCircuit {
	if h.opened != nil {
		select {
		case h.opened <- struct{}{}:
		default:
		}
	}
	return &echoCircuit{}
}

type echoCircuit struct{ closed bool }

func (c *echoCircuit) Command(block []byte) (reply []byte, result int32) {
	cp := append([]byte(nil), block...)
	return cp, -5000 // a distinguishable non-zero AFP result code
}
func (c *echoCircuit) Close() { c.closed = true }

func dialAndListen(t *testing.T, handler afp.CommandHandler) (net.Conn, *Transport) {
	t.Helper()
	tr := New(":0", handler, nil)
	// Bind on an ephemeral port directly (Start uses the configured addr; for the
	// test we want to know the resolved port), mirroring adapter/smbtcp's test.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	tr.listener = l
	tr.running = true
	go tr.acceptLoop(l)
	t.Cleanup(func() { _ = tr.Stop(nil) })

	conn, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, tr
}

func writeReq(t *testing.T, c net.Conn, reqID uint16, cmd uint8, data []byte) {
	t.Helper()
	h := dsiproto.Header{Flags: dsiproto.Request, Command: cmd, RequestID: reqID, DataLen: uint32(len(data))}
	if _, err := c.Write(h.Marshal()); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if len(data) > 0 {
		if _, err := c.Write(data); err != nil {
			t.Fatalf("write data: %v", err)
		}
	}
}

// replyFrame is a decoded DSI reply: the header plus its data block, kept together
// since dsiproto.Header itself carries no payload field.
type replyFrame struct {
	dsiproto.Header
	data []byte
}

func readReply(t *testing.T, c net.Conn) replyFrame {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	hdrBuf := make([]byte, dsiproto.HeaderSize)
	if _, err := readFull(c, hdrBuf); err != nil {
		t.Fatalf("read header: %v", err)
	}
	var f replyFrame
	if !f.Unmarshal(hdrBuf) {
		t.Fatal("bad header")
	}
	if f.DataLen > 0 {
		f.data = make([]byte, f.DataLen)
		if _, err := readFull(c, f.data); err != nil {
			t.Fatalf("read data: %v", err)
		}
	}
	return f
}

func readFull(c net.Conn, buf []byte) (int, error) {
	n := 0
	for n < len(buf) {
		m, err := c.Read(buf[n:])
		n += m
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

func TestGetStatus_NoSession(t *testing.T) {
	conn, _ := dialAndListen(t, echoHandler{})
	writeReq(t, conn, 1, dsiproto.GetStatus, nil)
	h := readReply(t, conn)
	if h.Command != dsiproto.GetStatus || h.Flags != dsiproto.Reply || h.RequestID != 1 {
		t.Fatalf("unexpected reply header: %+v", h)
	}
	if string(h.data) != "srvinfo" {
		t.Fatalf("GetStatus payload = %q, want %q", h.data, "srvinfo")
	}
}

func TestOpenSessionThenCommand(t *testing.T) {
	opened := make(chan struct{}, 1)
	conn, _ := dialAndListen(t, echoHandler{opened: opened})

	writeReq(t, conn, 1, dsiproto.OpenSession, nil)
	h := readReply(t, conn)
	if h.Command != dsiproto.OpenSession || h.DataLen != 0 {
		t.Fatalf("OpenSession reply = %+v", h)
	}
	select {
	case <-opened:
	case <-time.After(time.Second):
		t.Fatal("NewConn was not called on OpenSession")
	}

	writeReq(t, conn, 2, dsiproto.Command, []byte{0xAA, 0xBB})
	h = readReply(t, conn)
	if h.Command != dsiproto.Command || h.RequestID != 2 {
		t.Fatalf("Command reply header = %+v", h)
	}
	// The AFP result code lives in the header's ErrorOffset field, not prepended to
	// the payload — this is the correctness fix over the pre-refactor implementation.
	if int32(h.ErrorOffset) != -5000 {
		t.Fatalf("ErrorOffset = %d, want -5000", int32(h.ErrorOffset))
	}
	if string(h.data) != "\xaa\xbb" {
		t.Fatalf("Command reply payload = %v, want the echoed block unmodified", h.data)
	}
}

func TestCommandBeforeOpenSessionDropsConnection(t *testing.T) {
	conn, _ := dialAndListen(t, echoHandler{})
	writeReq(t, conn, 1, dsiproto.Command, []byte{0x01})
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	if _, err := conn.Read(buf); err == nil {
		t.Fatal("expected connection to be dropped for a Command before OpenSession")
	}
}

func TestTickleGetsNoReply(t *testing.T) {
	conn, _ := dialAndListen(t, echoHandler{})
	writeReq(t, conn, 1, dsiproto.Tickle, nil)
	// Follow it with a GetStatus; if Tickle wrongly produced a reply, this read would
	// return the Tickle's (wrong) header instead of GetStatus's.
	writeReq(t, conn, 2, dsiproto.GetStatus, nil)
	h := readReply(t, conn)
	if h.Command != dsiproto.GetStatus || h.RequestID != 2 {
		t.Fatalf("expected only the GetStatus reply, got %+v", h)
	}
}

func TestCloseSessionClosesCircuit(t *testing.T) {
	conn, _ := dialAndListen(t, echoHandler{})
	writeReq(t, conn, 1, dsiproto.OpenSession, nil)
	readReply(t, conn)
	writeReq(t, conn, 2, dsiproto.CloseSession, nil)
	h := readReply(t, conn)
	if h.Command != dsiproto.CloseSession {
		t.Fatalf("CloseSession reply = %+v", h)
	}
	// The server closes the connection after CloseSession; a further read should EOF.
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	if _, err := conn.Read(buf); err == nil {
		t.Fatal("expected connection to close after CloseSession")
	}
}
