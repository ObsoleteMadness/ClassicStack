package ncp

import (
	"sync"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
	ipxport "github.com/ObsoleteMadness/ClassicStack/core/port/ipx"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
)

// scriptLink is an in-test FrameLink: it captures every frame written and returns the
// frames queued on inbox from Read (then ErrTimeout to idle the read loop). A test injects
// a server reply with inject().
type scriptLink struct {
	mu     sync.Mutex
	inbox  [][]byte
	sent   [][]byte
	closed bool
}

func (l *scriptLink) inject(frame []byte) {
	l.mu.Lock()
	l.inbox = append(l.inbox, frame)
	l.mu.Unlock()
}

func (l *scriptLink) Read() (link.Frame, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil, link.ErrClosed
	}
	if len(l.inbox) > 0 {
		f := l.inbox[0]
		l.inbox = l.inbox[1:]
		return f, nil
	}
	return nil, link.ErrTimeout
}

func (l *scriptLink) Write(f link.Frame) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return link.ErrClosed
	}
	l.sent = append(l.sent, append([]byte(nil), f...))
	return nil
}

func (l *scriptLink) Close() error {
	l.mu.Lock()
	l.closed = true
	l.mu.Unlock()
	return nil
}

func (l *scriptLink) lastSent(t *testing.T) []byte {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.sent) == 0 {
		t.Fatal("no frame sent")
	}
	return l.sent[len(l.sent)-1]
}

// createConnReply builds a raw-802.3 IPX frame carrying an NCP CreateConnection reply
// (type 0x3333) that echoes the request's sequence with connection 1, sourced from the
// server node/net and addressed to the client MAC — what a NetWare server bound on raw
// 802.3 sends back.
func createConnReply(serverMAC, clientMAC [6]byte, serverNet [4]byte, seq uint8) []byte {
	// NCP reply header: type(2 BE) seq conn-low task conn-high completion.
	body := []byte{0x33, 0x33, seq, 0x01 /*conn low*/, 0x01 /*task*/, 0x00 /*conn high*/, 0x00 /*completion*/, 0x00 /*conn status*/}
	d := &ipxproto.Datagram{
		Type:    ipxNCPType,
		DstNode: clientMAC,
		DstSock: ncpSocket,
		SrcNet:  serverNet,
		SrcNode: serverMAC, // IPX node (may be internal-net node 1 on a real server)
		SrcSock: ncpSocket,
		Payload: body,
	}
	ipxBytes, _ := d.Encode(nil)
	return ipxport.FrameRaw8023.Encapsulate(clientMAC, serverMAC, ipxBytes)
}

// TestClientLearnsServerFrameType asserts the NCP client transport (a) sends its first
// (broadcast) request in the default Ethernet II framing, then (b) after a raw-802.3 reply
// switches to raw 802.3 on the next request and (c) addresses it to the reply frame's
// Ethernet source MAC (the L2 next hop), not the broadcast MAC.
func TestClientLearnsServerFrameType(t *testing.T) {
	l := &scriptLink{}
	clientMAC := [6]byte{0x02, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE}
	tr := DialIPX(l, clientMAC) // unpinned → learns
	defer tr.Close()

	// Drive CreateConnection: the session's Requester stamps seq 0 on the first request.
	// We call the transport directly with a minimal CreateConnection request packet
	// (type 0x1111, seq 0, conn 0) to exercise Send without the full session.
	req := []byte{0x11, 0x11, 0x00 /*seq*/, 0x00 /*conn low*/, 0x00 /*task*/, 0x00 /*conn high*/}

	// Inject the server's raw-802.3 reply so the in-flight Send completes.
	serverMAC := [6]byte{0x00, 0x00, 0xD8, 0xDE, 0xA9, 0x91} // a "Novell" NIC MAC
	serverNet := [4]byte{0x6A, 0x09, 0x18, 0x3D}             // the server's internal net
	go func() {
		time.Sleep(20 * time.Millisecond)
		l.inject(createConnReply(serverMAC, clientMAC, serverNet, 0))
	}()

	if _, err := tr.Send(req); err != nil {
		t.Fatalf("first Send: %v", err)
	}

	// The FIRST frame the client wrote must be Ethernet II (the pre-learn default) and
	// broadcast at L2.
	first := l.sent[0]
	if first[12] != 0x81 || first[13] != 0x37 {
		t.Errorf("first frame etherType = % x, want 81 37 (Ethernet II default)", first[12:14])
	}
	if [6]byte(first[0:6]) != broadcastNode {
		t.Errorf("first frame dst MAC = % x, want broadcast", first[0:6])
	}

	// Now send a second request; it must be framed raw-802.3 (learned) and unicast to the
	// reply's Ethernet source MAC.
	req2 := []byte{0x22, 0x22, 0x01 /*seq*/, 0x01 /*conn low*/, 0x00 /*task*/, 0x00 /*conn high*/}
	go func() {
		time.Sleep(20 * time.Millisecond)
		// reply of type 0x3333 for seq 1 conn 1
		body := []byte{0x33, 0x33, 0x01, 0x01, 0x00, 0x00, 0x00, 0x00}
		d := &ipxproto.Datagram{Type: ipxNCPType, DstNode: clientMAC, DstSock: ncpSocket, SrcNet: serverNet, SrcNode: serverMAC, SrcSock: ncpSocket, Payload: body}
		b, _ := d.Encode(nil)
		l.inject(ipxport.FrameRaw8023.Encapsulate(clientMAC, serverMAC, b))
	}()
	if _, err := tr.Send(req2); err != nil {
		t.Fatalf("second Send: %v", err)
	}

	second := l.lastSent(t)
	etherType := int(second[12])<<8 | int(second[13])
	if etherType > 0x05DC {
		t.Errorf("second frame etherType %#x is Ethernet II, want length-typed raw 802.3", etherType)
	} else if second[14] != 0xFF || second[15] != 0xFF {
		t.Errorf("second frame body[0:2] = % x, want ff ff (raw 802.3 magic)", second[14:16])
	}
	if [6]byte(second[0:6]) != serverMAC {
		t.Errorf("second frame dst MAC = % x, want the learned next-hop %x", second[0:6], serverMAC)
	}
}

// TestClientPinnedFrameTypeIgnoresLearning asserts a PINNED frame type is used on every
// request and NOT overwritten by the server's reply framing.
func TestClientPinnedFrameTypeIgnoresLearning(t *testing.T) {
	l := &scriptLink{}
	clientMAC := [6]byte{0x02, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE}
	tr := DialIPXFrame(l, clientMAC, ipxport.FrameLLC8022, true) // pinned to 802.2
	defer tr.Close()

	serverMAC := [6]byte{0x00, 0x00, 0xD8, 0xDE, 0xA9, 0x91}
	serverNet := [4]byte{0x6A, 0x09, 0x18, 0x3D}
	go func() {
		time.Sleep(20 * time.Millisecond)
		l.inject(createConnReply(serverMAC, clientMAC, serverNet, 0)) // reply is raw 802.3
	}()
	if _, err := tr.Send([]byte{0x11, 0x11, 0x00, 0x00, 0x00, 0x00}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// The first (and only) frame must be 802.2 LLC despite the raw-802.3 reply.
	first := l.sent[0]
	etherType := int(first[12])<<8 | int(first[13])
	if etherType > 0x05DC {
		t.Fatalf("pinned frame etherType %#x is Ethernet II, want length-typed 802.2", etherType)
	}
	if first[14] != 0xE0 || first[15] != 0xE0 || first[16] != 0x03 {
		t.Errorf("pinned frame LLC header = % x, want e0 e0 03 (802.2)", first[14:17])
	}
}
