package smbtcp

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/service/smb"
)

// echoConsumer is a fake smb.SessionConsumer whose circuit echoes each message back,
// so the test can assert the transport's framing without the real SMB command engine.
type echoConsumer struct{ served chan []byte }

func (e echoConsumer) NewConn() smb.SessionCircuit { return &echoCircuit{served: e.served} }

type echoCircuit struct{ served chan []byte }

func (c *echoCircuit) ServeMessage(req []byte) []byte {
	cp := append([]byte(nil), req...)
	select {
	case c.served <- cp:
	default:
	}
	return cp // echo
}
func (c *echoCircuit) SetPushWriter(func([]byte)) {}
func (c *echoCircuit) Close()                     {}

// writeMsg frames msg with the 4-byte session header and writes it.
func writeMsg(t *testing.T, c net.Conn, msg []byte) {
	t.Helper()
	var hdr [4]byte
	hdr[1] = byte(len(msg) >> 16)
	hdr[2] = byte(len(msg) >> 8)
	hdr[3] = byte(len(msg))
	if _, err := c.Write(hdr[:]); err != nil {
		t.Fatalf("write hdr: %v", err)
	}
	if _, err := c.Write(msg); err != nil {
		t.Fatalf("write msg: %v", err)
	}
}

// readMsg reads one framed message.
func readMsg(t *testing.T, c net.Conn) []byte {
	t.Helper()
	var hdr [4]byte
	if _, err := io.ReadFull(c, hdr[:]); err != nil {
		t.Fatalf("read hdr: %v", err)
	}
	n := int(hdr[1])<<16 | int(hdr[2])<<8 | int(hdr[3])
	buf := make([]byte, n)
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatalf("read body: %v", err)
	}
	return buf
}

// TestTransportFramesAndServes proves the transport accepts a TCP connection, reads a
// length-prefixed SMB message, drives the SMB session seam, and frames the reply back.
func TestTransportFramesAndServes(t *testing.T) {
	served := make(chan []byte, 1)
	tr := New("127.0.0.1:0", echoConsumer{served: served}, nil)
	if err := tr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tr.Stop(context.Background())

	// Start bound :0 — read the resolved address off the listener.
	tr.mu.Lock()
	addr := tr.listener.Addr().String()
	tr.mu.Unlock()

	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	msg := []byte("\xffSMBhello-smb")
	writeMsg(t, conn, msg)

	select {
	case got := <-served:
		if string(got) != string(msg) {
			t.Fatalf("served %q, want %q", got, msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("transport did not serve the message")
	}

	reply := readMsg(t, conn)
	if string(reply) != string(msg) {
		t.Fatalf("reply %q, want echo %q", reply, msg)
	}
}

// TestNBTSessionRequestAnswered proves a :139-style session request (type 0x81) gets a
// positive session response (type 0x82) and the following SMB message is then served.
func TestNBTSessionRequestAnswered(t *testing.T) {
	served := make(chan []byte, 1)
	tr := New("127.0.0.1:0", echoConsumer{served: served}, nil)
	if err := tr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tr.Stop(context.Background())
	tr.mu.Lock()
	addr := tr.listener.Addr().String()
	tr.mu.Unlock()

	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Session request: type 0x81, zero length.
	if _, err := conn.Write([]byte{nbtSessionRequest, 0, 0, 0}); err != nil {
		t.Fatalf("write session req: %v", err)
	}
	var resp [4]byte
	if _, err := io.ReadFull(conn, resp[:]); err != nil {
		t.Fatalf("read session resp: %v", err)
	}
	if resp[0] != nbtPositiveResp {
		t.Fatalf("session response type = %#x, want %#x", resp[0], nbtPositiveResp)
	}

	msg := []byte("\xffSMBafter-handshake")
	writeMsg(t, conn, msg)
	select {
	case got := <-served:
		if string(got) != string(msg) {
			t.Fatalf("served %q, want %q", got, msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("post-handshake message not served")
	}
}
