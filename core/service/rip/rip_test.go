package rip

import (
	"sync"
	"testing"
	"time"

	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
	ripproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/rip"
)

// fakeSender is an IPXSender that records every datagram sent, for
// assertions, and lets a test block a specific number of sends via a
// buffered channel (used to synchronize with the background broadcast loop
// without a real network or a flaky sleep).
type fakeSender struct {
	mu   sync.Mutex
	sent []*ipxproto.Datagram
	ch   chan *ipxproto.Datagram
}

func newFakeSender(chCap int) *fakeSender {
	return &fakeSender{ch: make(chan *ipxproto.Datagram, chCap)}
}

func (f *fakeSender) Send(d *ipxproto.Datagram) error {
	f.mu.Lock()
	f.sent = append(f.sent, d)
	f.mu.Unlock()
	select {
	case f.ch <- d:
	default:
	}
	return nil
}

func (f *fakeSender) last() *ipxproto.Datagram {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		return nil
	}
	return f.sent[len(f.sent)-1]
}

func (f *fakeSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

var net1 = [4]byte{0x00, 0x00, 0x00, 0x01}
var net2 = [4]byte{0x00, 0x00, 0x00, 0x02}

// TestSetNetworks_DropsZeroEntries checks the all-zero network (not a real
// owned network) is filtered out rather than advertised.
func TestSetNetworks_DropsZeroEntries(t *testing.T) {
	r := New(newFakeSender(4))
	r.SetNetworks(net1, [4]byte{}, net2)
	got := r.owned()
	if len(got) != 2 || got[0] != net1 || got[1] != net2 {
		t.Fatalf("owned() = %v, want [%v %v] (zero entry dropped)", got, net1, net2)
	}
}

// TestHandleDatagram_AnswersOwnedNetwork checks a Request naming an owned
// network gets a unicast Response back to the querier at the directly-served
// metric (hops 1 / ticks 2).
func TestHandleDatagram_AnswersOwnedNetwork(t *testing.T) {
	sender := newFakeSender(4)
	r := New(sender)
	r.SetNetworks(net1)

	req := &ripproto.Packet{
		Operation: ripproto.OpRequest,
		Entries:   []ripproto.Entry{{Network: net1, Hops: 0xFFFF, Ticks: 0xFFFF}},
	}
	d := &ipxproto.Datagram{
		Type:    ripproto.IPXType,
		SrcNet:  [4]byte{9, 9, 9, 9},
		SrcNode: [6]byte{1, 2, 3, 4, 5, 6},
		SrcSock: ripproto.Socket,
		Payload: req.Marshal(nil),
	}
	r.HandleDatagram(d)

	got := sender.last()
	if got == nil {
		t.Fatal("no response sent")
	}
	if got.DstNet != d.SrcNet || got.DstNode != d.SrcNode || got.DstSock != d.SrcSock {
		t.Errorf("response addressed to %v/%v/%v, want the querier %v/%v/%v",
			got.DstNet, got.DstNode, got.DstSock, d.SrcNet, d.SrcNode, d.SrcSock)
	}
	resp, err := ripproto.Unmarshal(got.Payload)
	if err != nil {
		t.Fatalf("Unmarshal response: %v", err)
	}
	if resp.Operation != ripproto.OpResponse {
		t.Errorf("Operation = %#04x, want OpResponse", resp.Operation)
	}
	if len(resp.Entries) != 1 || resp.Entries[0].Network != net1 {
		t.Fatalf("Entries = %+v, want one entry for %v", resp.Entries, net1)
	}
	if resp.Entries[0].Hops != directHops || resp.Entries[0].Ticks != directTicks {
		t.Errorf("metric = hops %d ticks %d, want %d/%d", resp.Entries[0].Hops, resp.Entries[0].Ticks, directHops, directTicks)
	}
}

// TestHandleDatagram_WildcardAnswersEverything checks a wildcard Request
// (network 0xFFFFFFFF) returns every owned network, not just one.
func TestHandleDatagram_WildcardAnswersEverything(t *testing.T) {
	sender := newFakeSender(4)
	r := New(sender)
	r.SetNetworks(net1, net2)

	req := &ripproto.Packet{
		Operation: ripproto.OpRequest,
		Entries:   []ripproto.Entry{{Network: ripproto.NetworkWildcard}},
	}
	r.HandleDatagram(&ipxproto.Datagram{Payload: req.Marshal(nil), SrcSock: ripproto.Socket})

	resp, err := ripproto.Unmarshal(sender.last().Payload)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(resp.Entries) != 2 {
		t.Fatalf("got %d entries, want 2 (wildcard matches every owned network)", len(resp.Entries))
	}
}

// TestHandleDatagram_UnknownNetworkNoReply checks a Request for a network
// this responder doesn't own gets no response at all (not an empty one).
func TestHandleDatagram_UnknownNetworkNoReply(t *testing.T) {
	sender := newFakeSender(4)
	r := New(sender)
	r.SetNetworks(net1)

	req := &ripproto.Packet{
		Operation: ripproto.OpRequest,
		Entries:   []ripproto.Entry{{Network: [4]byte{0xDE, 0xAD, 0xBE, 0xEF}}},
	}
	r.HandleDatagram(&ipxproto.Datagram{Payload: req.Marshal(nil), SrcSock: ripproto.Socket})

	if sender.count() != 0 {
		t.Errorf("sent %d datagrams for an unowned network, want 0", sender.count())
	}
}

// TestHandleDatagram_IgnoresResponses checks HandleDatagram never answers a
// Response packet (another router's broadcast) — this responder doesn't
// learn routes.
func TestHandleDatagram_IgnoresResponses(t *testing.T) {
	sender := newFakeSender(4)
	r := New(sender)
	r.SetNetworks(net1)

	resp := &ripproto.Packet{
		Operation: ripproto.OpResponse,
		Entries:   []ripproto.Entry{{Network: net1, Hops: 1, Ticks: 2}},
	}
	r.HandleDatagram(&ipxproto.Datagram{Payload: resp.Marshal(nil), SrcSock: ripproto.Socket})

	if sender.count() != 0 {
		t.Errorf("sent %d datagrams in response to a Response packet, want 0", sender.count())
	}
}

// TestHandleDatagram_NilAndMalformedSafe checks a nil datagram and one whose
// payload doesn't parse as RIP are dropped safely, not panicking.
func TestHandleDatagram_NilAndMalformedSafe(t *testing.T) {
	sender := newFakeSender(4)
	r := New(sender)
	r.SetNetworks(net1)

	r.HandleDatagram(nil)
	r.HandleDatagram(&ipxproto.Datagram{Payload: []byte{0x00}}) // too short for even the operation header

	if sender.count() != 0 {
		t.Errorf("sent %d datagrams for nil/malformed input, want 0", sender.count())
	}
}

// TestStartStop checks Start fires an immediate broadcast at the
// directly-served metric, and Stop fires a final broadcast at
// HopsUnreachable (16) so clients drop the route — both to the IPX
// broadcast address.
func TestStartStop(t *testing.T) {
	sender := newFakeSender(4)
	r := New(sender)
	r.SetNetworks(net1)

	r.Start()
	defer r.Stop()

	select {
	case d := <-sender.ch:
		resp, err := ripproto.Unmarshal(d.Payload)
		if err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if d.DstNode != ipxproto.BroadcastNode {
			t.Errorf("Start broadcast DstNode = %v, want the broadcast address", d.DstNode)
		}
		if len(resp.Entries) != 1 || resp.Entries[0].Hops != directHops {
			t.Fatalf("Start broadcast entries = %+v, want one entry at hops %d", resp.Entries, directHops)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the immediate broadcast on Start")
	}

	r.Stop()
	select {
	case d := <-sender.ch:
		resp, err := ripproto.Unmarshal(d.Payload)
		if err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if len(resp.Entries) != 1 || resp.Entries[0].Hops != ripproto.HopsUnreachable {
			t.Fatalf("Stop broadcast entries = %+v, want one entry at HopsUnreachable (%d)", resp.Entries, ripproto.HopsUnreachable)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the shutdown broadcast on Stop")
	}
}

// TestStartStop_Idempotent checks a second Start/Stop is a safe no-op (no
// panic on double-close, no duplicate broadcast).
func TestStartStop_Idempotent(t *testing.T) {
	sender := newFakeSender(4)
	r := New(sender)
	r.SetNetworks(net1)

	r.Start()
	<-sender.ch // the immediate broadcast
	r.Start()   // no-op: already started

	r.Stop()
	<-sender.ch // the shutdown broadcast
	r.Stop()    // no-op: already stopped, must not panic on double-close
}

// TestBroadcast_NoOwnedNetworksIsNoop checks Start's immediate broadcast
// sends nothing when no networks are owned yet, rather than an empty packet.
func TestBroadcast_NoOwnedNetworksIsNoop(t *testing.T) {
	sender := newFakeSender(4)
	r := New(sender)

	r.Start()
	defer r.Stop()
	time.Sleep(50 * time.Millisecond) // let the loop goroutine's immediate broadcast run
	if sender.count() != 0 {
		t.Errorf("sent %d datagrams with no owned networks, want 0", sender.count())
	}
}
