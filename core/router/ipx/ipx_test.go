package ipx

import (
	"sync"
	"testing"

	portipx "github.com/ObsoleteMadness/ClassicStack/core/port/ipx"
	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
)

// fakePort implements the mini-router's Port: it captures the delivery callback and records
// sends. Its SetDeliveryCallback takes the port package's named type so it satisfies Port.
type fakePort struct {
	mu   sync.Mutex
	cb   portipx.DeliveryCallback
	sent []sentFrame
}

type sentFrame struct {
	dstMAC [6]byte
	d      *protocol.Datagram
}

func (p *fakePort) SetDeliveryCallback(cb portipx.DeliveryCallback) { p.cb = cb }
func (p *fakePort) Send(dstMAC [6]byte, d *protocol.Datagram) error {
	p.mu.Lock()
	p.sent = append(p.sent, sentFrame{dstMAC: dstMAC, d: d})
	p.mu.Unlock()
	return nil
}

// recordingSocket records datagrams delivered to a socket.
type recordingSocket struct{ got []*protocol.Datagram }

func (s *recordingSocket) HandleDatagram(d *protocol.Datagram) { s.got = append(s.got, d) }

// recordingNode records datagrams delivered to a node/broadcast handler.
type recordingNode struct{ got []*protocol.Datagram }

func (n *recordingNode) HandleNodeDatagram(d *protocol.Datagram) { n.got = append(n.got, d) }

var ourNode = [6]byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}

func newWiredRouter() (*Router, *fakePort) {
	r := NewRouter(nil)
	r.SetIdentity([4]byte{0, 0, 0, 0x10}, ourNode)
	p := &fakePort{}
	r.AddPort(p)
	return r, p
}

func TestSocketDispatch(t *testing.T) {
	r, p := newWiredRouter()
	sock := &recordingSocket{}
	if err := r.RegisterSocket([2]byte{0x04, 0x51}, sock); err != nil {
		t.Fatalf("RegisterSocket: %v", err)
	}
	// Inbound addressed to us on socket 0x0451.
	p.cb(&protocol.Datagram{
		DstNet: [4]byte{0, 0, 0, 0x10}, DstNode: ourNode, DstSock: [2]byte{0x04, 0x51},
	})
	if len(sock.got) != 1 {
		t.Fatalf("socket handler got %d, want 1", len(sock.got))
	}
}

func TestNodeHandlerTakesPrecedence(t *testing.T) {
	r, p := newWiredRouter()
	sock := &recordingSocket{}
	node := &recordingNode{}
	clientNode := [6]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF}
	_ = r.RegisterSocket([2]byte{0x04, 0x51}, sock)
	if err := r.RegisterNode(clientNode, node); err != nil {
		t.Fatalf("RegisterNode: %v", err)
	}
	// Addressed to the claimed client node on a registered socket: node handler wins.
	p.cb(&protocol.Datagram{
		DstNet: [4]byte{0, 0, 0, 0x10}, DstNode: clientNode, DstSock: [2]byte{0x04, 0x51},
	})
	if len(node.got) != 1 {
		t.Errorf("node handler got %d, want 1", len(node.got))
	}
	if len(sock.got) != 0 {
		t.Errorf("socket handler got %d, want 0 (node takes precedence)", len(sock.got))
	}
}

func TestBroadcastFansOut(t *testing.T) {
	r, p := newWiredRouter()
	sock := &recordingSocket{}
	bcast := &recordingNode{}
	_ = r.RegisterSocket([2]byte{0x04, 0x52}, sock)
	if err := r.RegisterBroadcast(bcast); err != nil {
		t.Fatalf("RegisterBroadcast: %v", err)
	}
	// Broadcast on socket 0x0452: both the socket handler and the broadcast handler fire.
	p.cb(&protocol.Datagram{
		DstNet: [4]byte{0, 0, 0, 0x10}, DstNode: BroadcastNode, DstSock: [2]byte{0x04, 0x52},
	})
	if len(sock.got) != 1 {
		t.Errorf("socket handler got %d on broadcast, want 1", len(sock.got))
	}
	if len(bcast.got) != 1 {
		t.Errorf("broadcast handler got %d, want 1", len(bcast.got))
	}
}

func TestForeignDestinationDropped(t *testing.T) {
	r, p := newWiredRouter()
	sock := &recordingSocket{}
	_ = r.RegisterSocket([2]byte{0x04, 0x51}, sock)
	// Addressed to a different node on our network: not ours, not broadcast → dropped.
	p.cb(&protocol.Datagram{
		DstNet: [4]byte{0, 0, 0, 0x10}, DstNode: [6]byte{1, 2, 3, 4, 5, 6}, DstSock: [2]byte{0x04, 0x51},
	})
	if len(sock.got) != 0 {
		t.Errorf("foreign-destination datagram was delivered (%d)", len(sock.got))
	}
}

func TestSendFillsSourceAndMAC(t *testing.T) {
	r, p := newWiredRouter()
	dstNode := [6]byte{0x09, 0x08, 0x07, 0x06, 0x05, 0x04}
	if err := r.Send(&protocol.Datagram{DstNode: dstNode, DstSock: [2]byte{0x04, 0x51}}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(p.sent) != 1 {
		t.Fatalf("port got %d sends, want 1", len(p.sent))
	}
	s := p.sent[0]
	if s.dstMAC != dstNode {
		t.Errorf("dst MAC = %v, want the dst node %v (IPX node == MAC on Ethernet)", s.dstMAC, dstNode)
	}
	if s.d.SrcNode != ourNode {
		t.Errorf("src node not filled: %v, want %v", s.d.SrcNode, ourNode)
	}
	if s.d.SrcNet != [4]byte{0, 0, 0, 0x10} {
		t.Errorf("src net not filled: %v", s.d.SrcNet)
	}
}
