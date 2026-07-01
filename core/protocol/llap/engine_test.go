package llap

import "testing"

// drainProbes runs the probe burst, feeding each ENQ nowhere (no peer), and returns the
// claimed node. It mirrors the adapter's claim loop without timers.
func claimQuietly(t *testing.T, e *Engine) uint8 {
	t.Helper()
	e.BeginProbe()
	for {
		_, ok := e.NextProbe()
		if e.Conflicted() {
			t.Fatal("unexpected conflict on a quiet segment")
		}
		if !ok {
			break
		}
	}
	node, ok := e.AcceptTentative()
	if !ok {
		t.Fatal("AcceptTentative refused after a clean burst")
	}
	return node
}

// TestClaimQuiet proves a candidate with no collision is claimed after the probe burst,
// emitting ProbeCount ENQs for the desired node.
func TestClaimQuiet(t *testing.T) {
	e := NewEngine(Config{DesiredNode: 0xFE, ProbeCount: 3})

	e.BeginProbe()
	got := 0
	for {
		enq, ok := e.NextProbe()
		if !ok {
			break
		}
		if enq != Enq(0xFE) {
			t.Fatalf("probe %d = %v, want ENQ(0xFE)", got, enq)
		}
		got++
	}
	if got != 3 {
		t.Fatalf("sent %d probes, want 3", got)
	}
	node, ok := e.AcceptTentative()
	if !ok || node != 0xFE {
		t.Fatalf("AcceptTentative = (%#x,%v), want (0xFE,true)", node, ok)
	}
	if c, ok := e.Claimed(); !ok || c != 0xFE {
		t.Fatalf("Claimed = (%#x,%v), want (0xFE,true)", c, ok)
	}
}

// TestClaimConflictReroll proves an inbound ENQ (or ACK) for our candidate flags a
// conflict, rerolls to a different candidate, and a fresh burst then claims the new one.
func TestClaimConflictReroll(t *testing.T) {
	e := NewEngine(Config{DesiredNode: 0xFE, ProbeCount: 4})
	e.BeginProbe()

	// First probe goes out, then a peer ENQs our candidate → conflict + reroll.
	if _, ok := e.NextProbe(); !ok {
		t.Fatal("first NextProbe produced nothing")
	}
	_, _, conflict := e.Inbound(Enq(0xFE))
	if !conflict {
		t.Fatal("ENQ for our candidate did not conflict")
	}
	if !e.Conflicted() {
		t.Fatal("Conflicted() false after a collision")
	}
	if e.Candidate() == 0xFE {
		t.Fatal("did not reroll to a new candidate")
	}
	if _, ok := e.NextProbe(); ok {
		t.Fatal("NextProbe produced a probe while conflicted")
	}
	if _, ok := e.AcceptTentative(); ok {
		t.Fatal("AcceptTentative accepted a conflicted claim")
	}

	// Re-arm and claim the new candidate on a quiet segment.
	newCand := e.Candidate()
	node := claimQuietly(t, e)
	if node != newCand {
		t.Fatalf("claimed %#x, want the rerolled candidate %#x", node, newCand)
	}
}

// TestAckConflict proves an inbound ACK for our candidate (a node answering our ENQ) also
// triggers a reroll.
func TestAckConflict(t *testing.T) {
	e := NewEngine(Config{DesiredNode: 0x10, ProbeCount: 2})
	e.BeginProbe()
	e.NextProbe()
	if _, _, conflict := e.Inbound(Ack(0x10)); !conflict {
		t.Fatal("ACK for our candidate did not conflict")
	}
}

// TestDefendClaimedRespond proves a claimed engine with RespondToEnq answers an ENQ for
// its node with a defending ACK, and ignores ENQs for other nodes.
func TestDefendClaimedRespond(t *testing.T) {
	e := NewEngine(Config{DesiredNode: 0x20, ProbeCount: 1, RespondToEnq: true})
	if claimQuietly(t, e) != 0x20 {
		t.Fatal("did not claim 0x20")
	}

	reply, has, conflict := e.Inbound(Enq(0x20))
	if !has || conflict {
		t.Fatalf("ENQ for our node → (has=%v conflict=%v), want has=true conflict=false", has, conflict)
	}
	if reply != Ack(0x20) {
		t.Fatalf("defend reply = %v, want ACK(0x20)", reply)
	}
	// An ENQ for a different node draws no reply.
	if _, has, _ := e.Inbound(Enq(0x21)); has {
		t.Fatal("replied to an ENQ for someone else's node")
	}
}

// TestDefendClaimedSilent proves a claimed engine WITHOUT RespondToEnq (TashTalk) stays
// silent — the physical medium defends in hardware.
func TestDefendClaimedSilent(t *testing.T) {
	e := NewEngine(Config{DesiredNode: 0x20, ProbeCount: 1, RespondToEnq: false})
	if claimQuietly(t, e) != 0x20 {
		t.Fatal("did not claim 0x20")
	}
	if _, has, _ := e.Inbound(Enq(0x20)); has {
		t.Fatal("TashTalk-mode engine replied to an ENQ (should defend in hardware)")
	}
}

// TestClaimedIgnoresConflict proves that once claimed, an inbound ENQ for our node never
// flags a (now meaningless) claim conflict — node-claim is one-shot per the spec.
func TestClaimedIgnoresConflict(t *testing.T) {
	e := NewEngine(Config{DesiredNode: 0x30, ProbeCount: 1, RespondToEnq: true})
	claimQuietly(t, e)
	if _, _, conflict := e.Inbound(Enq(0x30)); conflict {
		t.Fatal("claimed engine reported a claim conflict")
	}
}

// TestRerollExhaustsThenRefills proves rerolling never gets stuck: popping the whole pool
// refills it, so a degenerate all-colliding segment still always offers a candidate.
func TestRerollExhaustsThenRefills(t *testing.T) {
	e := NewEngine(Config{DesiredNode: 0xFE, ProbeCount: 1})
	seen := map[uint8]bool{}
	// Force many rerolls; the candidate must always stay in the valid unicast range.
	for range 600 {
		e.BeginProbe()
		e.NextProbe()
		e.Inbound(Enq(e.Candidate()))
		c := e.Candidate()
		if c < MinNode || c > MaxNode {
			t.Fatalf("reroll produced out-of-range candidate %#x", c)
		}
		seen[c] = true
	}
	if len(seen) < 10 {
		t.Fatalf("reroll explored only %d candidates, expected the pool to cycle", len(seen))
	}
}
