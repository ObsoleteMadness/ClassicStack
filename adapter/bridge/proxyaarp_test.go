package bridge

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/adapter/link/inmem"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/aarp"
)

// Ethernet 802.3 + 802.2 LLC + SNAP framing (local copy so the test does not reach into
// the framing package's private helpers). Only enough to build/classify AARP frames.
var (
	llcSNAP  = []byte{0xAA, 0xAA, 0x03}
	snapAARP = []byte{0x00, 0x00, 0x00, 0x80, 0xF3}
	atBcast  = [6]byte{0x09, 0x00, 0x07, 0xFF, 0xFF, 0xFF}
)

func mac(b ...byte) [6]byte {
	var m [6]byte
	copy(m[:], b)
	return m
}

// aarpEthFrame builds an EtherTalk AARP frame (802.3/LLC/SNAP) for packet p.
func aarpEthFrame(dstMAC, srcMAC [6]byte, p aarp.Packet) []byte {
	payload := p.Encode(nil)
	length := len(llcSNAP) + len(snapAARP) + len(payload)
	f := make([]byte, 0, 14+length)
	f = append(f, dstMAC[:]...)
	f = append(f, srcMAC[:]...)
	f = append(f, byte(length>>8), byte(length))
	f = append(f, llcSNAP...)
	f = append(f, snapAARP...)
	f = append(f, payload...)
	return f
}

// harness wires a bridge between two inmem pairs; the test drives tunPeer/egrPeer.
type harness struct {
	b       *ProxyAARP
	tunPeer *inmem.Link // the test writes/reads the tunnel side here
	egrPeer *inmem.Link // the test writes/reads the egress side here
}

func newHarness(t *testing.T, egressMAC [6]byte) *harness {
	t.Helper()
	tunA, tunB := inmem.Pair(8) // tunA = bridge side, tunB = test side
	egrA, egrB := inmem.Pair(8) // egrA = bridge side, egrB = test side
	b := New(Name,
		func() (link.FrameLink, error) { return tunA, nil },
		func() (link.FrameLink, error) { return egrA, nil },
		egressMAC, nil)
	if err := b.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = b.Stop(context.Background()) })
	return &harness{b: b, tunPeer: tunB, egrPeer: egrB}
}

// readWithin reads one frame from l, failing if none arrives within d.
func readWithin(t *testing.T, l *inmem.Link, d time.Duration) []byte {
	t.Helper()
	type res struct {
		f   []byte
		err error
	}
	ch := make(chan res, 1)
	go func() { f, err := l.Read(); ch <- res{f, err} }()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("read: %v", r.err)
		}
		return r.f
	case <-time.After(d):
		t.Fatal("timed out waiting for a forwarded frame")
		return nil
	}
}

// TestBridgeRewritesReplyTunnelToEgress proves an AARP Reply forwarded tunnel→egress is
// re-sourced from the egress MAC (both Ethernet src and AARP sender-hardware).
func TestBridgeRewritesReplyTunnelToEgress(t *testing.T) {
	egress := mac(0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x01)
	station := mac(1, 2, 3, 4, 5, 6)
	requester := mac(9, 8, 7, 6, 5, 4)
	h := newHarness(t, egress)

	reply := aarp.Reply(station, aarp.ProtoAddr{Network: 1, Node: 0x10},
		requester, aarp.ProtoAddr{Network: 1, Node: 0x20})
	if err := h.tunPeer.Write(aarpEthFrame(requester, station, reply)); err != nil {
		t.Fatalf("write reply: %v", err)
	}

	got := readWithin(t, h.egrPeer, time.Second)
	if !bytes.Equal(got[6:12], egress[:]) {
		t.Fatalf("forwarded ethernet src = %x, want egress %x", got[6:12], egress)
	}
	// The AARP sender-hardware inside was rewritten too.
	pkt, err := aarp.Decode(got[22:]) // 14 eth + 3 llc + 5 snap = 22
	if err != nil {
		t.Fatalf("decode forwarded AARP: %v", err)
	}
	if pkt.SrcHw != egress {
		t.Fatalf("forwarded AARP SrcHw = %x, want egress %x", pkt.SrcHw, egress)
	}
}

// TestBridgePassesRequestUnchanged proves an AARP Request forwarded tunnel→egress is NOT
// rewritten (address discovery must survive).
func TestBridgePassesRequestUnchanged(t *testing.T) {
	egress := mac(0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x01)
	station := mac(1, 2, 3, 4, 5, 6)
	h := newHarness(t, egress)

	req := aarp.Request(station, aarp.ProtoAddr{Network: 1, Node: 0x10}, aarp.ProtoAddr{Network: 1, Node: 0x20})
	in := aarpEthFrame(atBcast, station, req)
	if err := h.tunPeer.Write(in); err != nil {
		t.Fatalf("write request: %v", err)
	}

	got := readWithin(t, h.egrPeer, time.Second)
	if !bytes.Equal(got, in) {
		t.Fatalf("Request was altered:\n got  %x\n want %x", got, in)
	}
}

// TestBridgeEgressToTunnelVerbatim proves the egress→tunnel direction never rewrites —
// even an AARP Reply passes through byte-for-byte.
func TestBridgeEgressToTunnelVerbatim(t *testing.T) {
	egress := mac(0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x01)
	remote := mac(0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF)
	station := mac(1, 2, 3, 4, 5, 6)
	h := newHarness(t, egress)

	reply := aarp.Reply(remote, aarp.ProtoAddr{Network: 1, Node: 0x30},
		station, aarp.ProtoAddr{Network: 1, Node: 0x10})
	in := aarpEthFrame(station, remote, reply)
	if err := h.egrPeer.Write(in); err != nil {
		t.Fatalf("write reply: %v", err)
	}

	got := readWithin(t, h.tunPeer, time.Second)
	if !bytes.Equal(got, in) {
		t.Fatalf("egress→tunnel Reply was altered:\n got  %x\n want %x", got, in)
	}
}

// TestBridgeInertWithoutOpeners proves the bridge satisfies the lifecycle (Start/Stop) as
// a no-op when an opener is nil (no NIC backend in this build).
func TestBridgeInertWithoutOpeners(t *testing.T) {
	b := New(Name, nil, nil, mac(1, 2, 3, 4, 5, 6), nil)
	if err := b.Start(context.Background()); err != nil {
		t.Fatalf("inert Start: %v", err)
	}
	if err := b.Stop(context.Background()); err != nil {
		t.Fatalf("inert Stop: %v", err)
	}
}
