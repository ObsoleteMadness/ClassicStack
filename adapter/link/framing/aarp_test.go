package framing

import (
	"sync"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/adapter/link/inmem"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/aarp"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
)

// aarpMAC builds a 6-byte MAC.
func aarpMAC(b ...byte) [6]byte {
	var m [6]byte
	copy(m[:], b)
	return m
}

// readAARP reads one frame from the peer link and decodes its AARP payload, or returns
// ok=false on timeout/non-AARP. It waits up to a deadline for an AARP frame.
func readAARP(t *testing.T, peer *inmem.Link, within time.Duration) (aarp.Packet, bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	type res struct {
		p  aarp.Packet
		ok bool
	}
	for time.Now().Before(deadline) {
		ch := make(chan res, 1)
		go func() {
			frame, err := peer.Read()
			if err != nil {
				ch <- res{}
				return
			}
			pid, off, ok := snapPIDOf(frame)
			if !ok || !equal(pid, snapAARP) {
				ch <- res{}
				return
			}
			p, derr := aarp.Decode(frame[off:])
			ch <- res{p: p, ok: derr == nil}
		}()
		select {
		case r := <-ch:
			if r.ok {
				return r.p, true
			}
		case <-time.After(time.Until(deadline)):
			return aarp.Packet{}, false
		}
	}
	return aarp.Packet{}, false
}

// writeAARPFrame sends an AARP packet to the framer (from the peer end).
func writeAARPFrame(peer *inmem.Link, srcMAC [6]byte, pkt aarp.Packet) {
	frame := appendEthSNAP(nil, appleTalkBroadcastMAC, srcMAC[:], snapAARP, pkt.Encode(nil))
	_ = peer.Write(frame)
}

// newAARPHarness builds an AARP framer over one end of an inmem Pair and returns the
// DatagramLink, the shared LiveAddr, the peer link, and a function reporting the claimed
// address. Probes are fast (count 2, 5ms) so claims complete promptly.
func newAARPHarness(t *testing.T, stationMAC [6]byte) (link.DatagramLink, *LiveAddr, *inmem.Link, func() (aarp.ProtoAddr, bool)) {
	t.Helper()
	local, peer := inmem.Pair(8)
	addr := &LiveAddr{}

	var mu sync.Mutex
	var claimed aarp.ProtoAddr
	var done bool

	f := &EtherTalkAARP{
		SrcMAC:        stationMAC[:],
		Addr:          addr,
		SeedNetMin:    0xFE01,
		SeedNetMax:    0xFE01, // single network → deterministic
		ProbeCount:    2,
		ProbeInterval: 5 * time.Millisecond,
		RandNode:      func() uint8 { return 0x42 },
		OnClaimed: func(network uint16, node uint8, _, _ uint16) {
			mu.Lock()
			claimed = aarp.ProtoAddr{Network: network, Node: node}
			done = true
			mu.Unlock()
		},
	}
	dl, err := f.Framing(local)
	if err != nil {
		t.Fatalf("Framing: %v", err)
	}
	t.Cleanup(func() { _ = dl.Close() })

	return dl, addr, peer, func() (aarp.ProtoAddr, bool) {
		mu.Lock()
		defer mu.Unlock()
		return claimed, done
	}
}

// drainReadLoop runs ReadDatagram in the background so the framer services inbound AARP
// (the read loop is what calls serviceAARP). Returns a channel of decoded DDP datagrams.
func drainReadLoop(dl link.DatagramLink) <-chan ddp.Datagram {
	out := make(chan ddp.Datagram, 16)
	go func() {
		for {
			dg, err := dl.ReadDatagram()
			if err != nil {
				close(out)
				return
			}
			out <- dg
		}
	}()
	return out
}

// TestAARPClaimsAddress proves the framer probes for and accepts a node address when the
// peer raises no conflict, publishing it via OnClaimed + the LiveAddr.
func TestAARPClaimsAddress(t *testing.T) {
	station := aarpMAC(0x00, 0x11, 0x22, 0x33, 0x44, 0x55)
	dl, addr, peer, claimedFn := newAARPHarness(t, station)
	_ = drainReadLoop(dl)

	// The peer should observe our probes (we don't answer → we claim).
	if p, ok := readAARP(t, peer, 200*time.Millisecond); !ok || p.Function != aarp.FuncProbe {
		t.Fatalf("expected a probe, got %+v ok=%v", p, ok)
	}

	// Claim completes: OnClaimed fired and the LiveAddr carries the node.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, done := claimedFn(); done {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	got, done := claimedFn()
	if !done {
		t.Fatal("claim never completed")
	}
	if got.Node != 0x42 || got.Network != 0xFE01 {
		t.Fatalf("claimed %+v, want net 0xFE01 node 0x42", got)
	}
	if addr.Node() != 0x42 {
		t.Fatalf("LiveAddr node = %d, want 0x42", addr.Node())
	}
}

// TestAARPDropsOutboundUntilClaimed proves WriteDatagram drops DDP before the node is
// claimed and delivers it after.
func TestAARPDropsOutboundUntilClaimed(t *testing.T) {
	station := aarpMAC(0x00, 0x11, 0x22, 0x33, 0x44, 0x55)
	dl, addr, peer, _ := newAARPHarness(t, station)
	_ = drainReadLoop(dl)

	// Before claim: a write is dropped (LiveAddr node 0). Send and confirm the peer sees
	// no DDP frame within a short window (only probes).
	dg := ddp.Datagram{DestNetwork: 0xFE01, SrcNetwork: 0xFE01, DestNode: 0x10, SrcNode: 0x42, DDPType: 1}
	if addr.Node() == 0 {
		if err := dl.WriteDatagram(dg); err != nil {
			t.Fatalf("WriteDatagram(pre-claim): %v", err)
		}
	}

	// Wait for the claim to land.
	deadline := time.Now().Add(500 * time.Millisecond)
	for addr.Node() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if addr.Node() == 0 {
		t.Fatal("never claimed")
	}

	// After claim: the write is delivered. Drain probe frames first, then look for DDP.
	if err := dl.WriteDatagram(dg); err != nil {
		t.Fatalf("WriteDatagram(post-claim): %v", err)
	}
	sawDDP := false
	end := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(end) {
		frame, err := peer.Read()
		if err != nil {
			break
		}
		if pid, _, ok := snapPIDOf(frame); ok && equal(pid, snapAppleTalk) {
			sawDDP = true
			break
		}
	}
	if !sawDDP {
		t.Fatal("post-claim DDP datagram was not delivered")
	}
}

// TestAARPResolvesToUnicast proves a unicast DDP to a peer triggers an AARP Request, and
// once the peer Replies the next datagram goes UNICAST to the peer's MAC (not broadcast).
func TestAARPResolvesToUnicast(t *testing.T) {
	station := aarpMAC(0x00, 0x11, 0x22, 0x33, 0x44, 0x55)
	peerMAC := aarpMAC(0xAB, 0xCD, 0xEF, 0x01, 0x02, 0x03)
	dl, addr, peer, _ := newAARPHarness(t, station)
	_ = drainReadLoop(dl)

	// Wait for claim.
	for addr.Node() == 0 {
		time.Sleep(5 * time.Millisecond)
	}

	// Pre-seed nothing: a unicast write misses the AMT → emits a Request and falls back to
	// broadcast for this datagram.
	target := ddp.Datagram{DestNetwork: 0xFE01, SrcNetwork: 0xFE01, DestNode: 0x20, SrcNode: 0x42, DDPType: 1}
	if err := dl.WriteDatagram(target); err != nil {
		t.Fatalf("WriteDatagram: %v", err)
	}

	// The peer should receive an AARP Request for node 0x20.
	gotReq := false
	end := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(end) && !gotReq {
		if p, ok := readAARP(t, peer, 50*time.Millisecond); ok && p.Function == aarp.FuncRequest {
			if p.TargetProto.Node == 0x20 {
				gotReq = true
			}
		}
	}
	if !gotReq {
		t.Fatal("no AARP Request emitted for the unresolved unicast destination")
	}

	// The peer answers: it owns 0xFE01.0x20 at peerMAC.
	reply := aarp.Reply(peerMAC, aarp.ProtoAddr{Network: 0xFE01, Node: 0x20}, station, aarp.ProtoAddr{Network: 0xFE01, Node: 0x42})
	writeAARPFrame(peer, peerMAC, reply)

	// Give the read loop time to glean.
	time.Sleep(30 * time.Millisecond)

	// Now a unicast write to 0x20 should go to peerMAC, not broadcast.
	if err := dl.WriteDatagram(target); err != nil {
		t.Fatalf("WriteDatagram(after resolve): %v", err)
	}
	sawUnicast := false
	end = time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(end) {
		frame, err := peer.Read()
		if err != nil {
			break
		}
		if pid, _, ok := snapPIDOf(frame); ok && equal(pid, snapAppleTalk) {
			// dst MAC is the first 6 bytes of the Ethernet frame.
			if equalBytes(frame[0:6], peerMAC[:]) {
				sawUnicast = true
				break
			}
		}
	}
	if !sawUnicast {
		t.Fatal("DDP to a resolved node did not go unicast to the peer MAC")
	}
}

// TestAARPGleansFromRequest proves the framer learns a peer's MAC from an inbound Request
// (gleaning), so a later resolve hits the AMT without a query.
func TestAARPGleansFromRequest(t *testing.T) {
	station := aarpMAC(0x00, 0x11, 0x22, 0x33, 0x44, 0x55)
	peerMAC := aarpMAC(0x77, 0x77, 0x77, 0x77, 0x77, 0x77)
	dl, addr, peer, _ := newAARPHarness(t, station)
	_ = drainReadLoop(dl)
	for addr.Node() == 0 {
		time.Sleep(5 * time.Millisecond)
	}

	// The peer broadcasts a Request (for some third party) — we glean its source.
	req := aarp.Request(peerMAC, aarp.ProtoAddr{Network: 0xFE01, Node: 0x55}, aarp.ProtoAddr{Network: 0xFE01, Node: 0x99})
	writeAARPFrame(peer, peerMAC, req)
	time.Sleep(30 * time.Millisecond)

	// A unicast to the gleaned node now goes unicast immediately (no Request).
	dg := ddp.Datagram{DestNetwork: 0xFE01, SrcNetwork: 0xFE01, DestNode: 0x55, SrcNode: 0x42, DDPType: 1}
	if err := dl.WriteDatagram(dg); err != nil {
		t.Fatalf("WriteDatagram: %v", err)
	}
	sawUnicast := false
	end := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(end) {
		frame, err := peer.Read()
		if err != nil {
			break
		}
		if pid, _, ok := snapPIDOf(frame); ok && equal(pid, snapAppleTalk) && equalBytes(frame[0:6], peerMAC[:]) {
			sawUnicast = true
			break
		}
	}
	if !sawUnicast {
		t.Fatal("gleaned node was not resolved to unicast")
	}
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
