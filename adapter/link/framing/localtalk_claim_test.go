package framing

import (
	"sync"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/adapter/link/inmem"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/llap"
)

// newClaimHarness builds a claim-enabled LocalTalk framer over one end of an inmem
// Pair and returns the DatagramLink, the shared LiveAddr, the peer link, and a
// function reporting the claimed node. Probes are fast (count 2, 5ms) so claims
// complete promptly; the candidate is fixed via DesiredNode for determinism.
func newClaimHarness(t *testing.T, desired uint8, respondToEnq bool) (link.DatagramLink, *LiveAddr, *inmem.Link, func() (uint8, bool)) {
	t.Helper()
	local, peer := inmem.Pair(8)
	live := &LiveAddr{}

	var mu sync.Mutex
	var claimedNode uint8
	var done bool

	f := &LocalTalk{
		Addr:          live,
		Live:          live,
		EnableClaim:   true,
		RespondToEnq:  respondToEnq,
		SeedNetwork:   0x00CC,
		DesiredNode:   desired,
		ProbeCount:    2,
		ProbeInterval: 5 * time.Millisecond,
		OnClaimed: func(_ uint16, node uint8, _, _ uint16) {
			mu.Lock()
			claimedNode = node
			done = true
			mu.Unlock()
		},
	}
	dl, err := f.Framing(local)
	if err != nil {
		t.Fatalf("Framing: %v", err)
	}
	t.Cleanup(func() { _ = dl.Close() })

	return dl, live, peer, func() (uint8, bool) {
		mu.Lock()
		defer mu.Unlock()
		return claimedNode, done
	}
}

// TestClaimCloseUnblocksReadDatagram verifies Close stops a blocked ReadDatagram
// promptly instead of waiting for the next wire frame (TashTalk shutdown path).
func TestClaimCloseUnblocksReadDatagram(t *testing.T) {
	dl, _, _, _ := newClaimHarness(t, 0xFE, false)
	done := make(chan struct{})
	go func() {
		_, _ = dl.ReadDatagram()
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	if err := dl.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("ReadDatagram did not return after Close")
	}
}

// readControl reads one LLAP control frame from the peer within a deadline.
func readControl(t *testing.T, peer *inmem.Link, within time.Duration) (llap.ControlFrame, bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		type res struct {
			c  llap.ControlFrame
			ok bool
		}
		ch := make(chan res, 1)
		go func() {
			frame, err := peer.Read()
			if err != nil {
				ch <- res{}
				return
			}
			if _, _, typ, ok := llap.Header(frame); !ok || !llap.IsControl(typ) {
				ch <- res{}
				return
			}
			c, derr := llap.DecodeControl(frame)
			ch <- res{c: c, ok: derr == nil}
		}()
		select {
		case r := <-ch:
			if r.ok {
				return r.c, true
			}
		case <-time.After(time.Until(deadline)):
			return llap.ControlFrame{}, false
		}
	}
	return llap.ControlFrame{}, false
}

// TestLLAPClaimsNode proves the framer probes a candidate node with ENQs and, with no
// collision, claims it — publishing the node via OnClaimed + the LiveAddr.
func TestLLAPClaimsNode(t *testing.T) {
	dl, live, peer, claimedFn := newClaimHarness(t, 0xFE, true)
	_ = drainReadLoop(dl)

	// The peer observes our ENQ probe for the candidate node.
	if c, ok := readControl(t, peer, 200*time.Millisecond); !ok || c.Type != llap.TypeENQ || c.Dst != 0xFE {
		t.Fatalf("expected ENQ for 0xFE, got %+v ok=%v", c, ok)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, done := claimedFn(); done {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	node, done := claimedFn()
	if !done {
		t.Fatal("claim never completed")
	}
	if node != 0xFE {
		t.Fatalf("claimed node 0x%02X, want 0xFE", node)
	}
	if live.Node() != 0xFE || live.Network() != 0x00CC {
		t.Fatalf("LiveAddr = net 0x%X node 0x%X, want 0x00CC/0xFE", live.Network(), live.Node())
	}
}

// TestLLAPDropsOutboundUntilClaimed proves the framer stamps node 0 before the claim
// (so the runport's drop-until-claimed contract holds) and the real node after.
func TestLLAPDropsOutboundUntilClaimed(t *testing.T) {
	_, live, _, _ := newClaimHarness(t, 0x40, true)
	if live.Node() != 0 {
		t.Fatalf("pre-claim LiveAddr node = 0x%X, want 0 (unclaimed)", live.Node())
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	for live.Node() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if live.Node() != 0x40 {
		t.Fatalf("post-claim LiveAddr node = 0x%X, want 0x40", live.Node())
	}
}

// TestLLAPDefendsClaimedNode proves a claimed framer with RespondToEnq answers an ENQ
// for its node with a defending ACK (the LToUDP shared-segment behaviour).
func TestLLAPDefendsClaimedNode(t *testing.T) {
	dl, live, peer, _ := newClaimHarness(t, 0x30, true)
	_ = drainReadLoop(dl)

	// Read exactly our two outbound ENQ probes (ProbeCount=2); once the burst is done
	// the claim goroutine is finished and emits nothing more, so the next frame on the
	// peer will be the defending ACK we provoke. Reading inline (no spawned reader)
	// avoids a leaked goroutine stealing the ACK.
	for range 2 {
		frame, err := peer.Read()
		if err != nil {
			t.Fatalf("draining probe: %v", err)
		}
		if c, _ := llap.DecodeControl(frame); c.Type != llap.TypeENQ || c.Dst != 0x30 {
			t.Fatalf("drained frame = %+v, want ENQ(0x30)", c)
		}
	}
	// The burst is sent; give the claim goroutine its final tick to accept + publish.
	deadline := time.Now().Add(300 * time.Millisecond)
	for live.Node() == 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if live.Node() != 0x30 {
		t.Fatalf("never claimed 0x30 (node=0x%X)", live.Node())
	}

	// Inject a fresh ENQ for our claimed node; expect a defending ACK back.
	if err := peer.Write(llap.EncodeControl(llap.Enq(0x30))); err != nil {
		t.Fatalf("write ENQ: %v", err)
	}
	frame, err := peer.Read()
	if err != nil {
		t.Fatalf("read defending ACK: %v", err)
	}
	if c, _ := llap.DecodeControl(frame); c.Type != llap.TypeACK || c.Dst != 0x30 {
		t.Fatalf("expected defending ACK for 0x30, got %+v", c)
	}
}

// TestLLAPRerollsOnCollision proves an inbound ENQ for our candidate (a peer claiming
// it first) forces a reroll to a different node, which is then claimed.
func TestLLAPRerollsOnCollision(t *testing.T) {
	dl, live, peer, claimedFn := newClaimHarness(t, 0x50, true)
	_ = drainReadLoop(dl)

	// As soon as we see the first probe for 0x50, slam an ENQ for 0x50 back (the peer
	// owns it) so the claim collides and rerolls.
	if c, ok := readControl(t, peer, 200*time.Millisecond); !ok || c.Dst != 0x50 {
		t.Fatalf("expected first probe for 0x50, got %+v ok=%v", c, ok)
	}
	if err := peer.Write(llap.EncodeControl(llap.Enq(0x50))); err != nil {
		t.Fatalf("write colliding ENQ: %v", err)
	}

	// The claim must eventually complete on some OTHER node.
	deadline := time.Now().Add(800 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, done := claimedFn(); done {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	node, done := claimedFn()
	if !done {
		t.Fatal("claim never completed after collision")
	}
	if node == 0x50 {
		t.Fatal("claimed the colliding node 0x50 — should have rerolled")
	}
	if node < llap.MinNode || node > llap.MaxNode {
		t.Fatalf("rerolled to out-of-range node 0x%02X", node)
	}
	if live.Node() != node {
		t.Fatalf("LiveAddr node 0x%X != claimed 0x%X", live.Node(), node)
	}
}
