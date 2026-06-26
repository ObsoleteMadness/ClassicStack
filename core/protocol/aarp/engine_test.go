package aarp

import "testing"

func newTestEngine() *Engine {
	return NewEngine(Config{HardwareAddr: mac(0x00, 0x11, 0x22, 0x33, 0x44, 0x55), ProbeCount: 3})
}

// TestClaimAcceptsWhenUnopposed proves a claim that meets no conflict emits its probes
// and then accepts the tentative address.
func TestClaimAcceptsWhenUnopposed(t *testing.T) {
	e := newTestEngine()
	tent := ProtoAddr{Network: 0xFE01, Node: 0x42}
	e.BeginProbe(tent)

	probes := 0
	for {
		pkt, ok := e.NextProbe()
		if !ok {
			break
		}
		probes++
		// Each probe decodes as a probe for the tentative address.
		p, err := Decode(pkt)
		if err != nil || p.Function != FuncProbe || p.SrcProto != tent {
			t.Fatalf("probe %d malformed: %+v err=%v", probes, p, err)
		}
	}
	if probes != 3 {
		t.Fatalf("emitted %d probes, want 3", probes)
	}
	if e.Conflicted() {
		t.Fatal("unopposed claim reported a conflict")
	}
	got, ok := e.AcceptTentative()
	if !ok || got != tent {
		t.Fatalf("AcceptTentative = %v ok=%v, want %v", got, ok, tent)
	}
	claimed, done := e.Claimed()
	if !done || claimed != tent {
		t.Fatalf("Claimed = %v done=%v, want %v", claimed, done, tent)
	}
}

// TestClaimConflictFromReply proves an inbound Reply (or Request) using our tentative
// address aborts the claim — NextProbe stops and AcceptTentative refuses.
func TestClaimConflictFromReply(t *testing.T) {
	e := newTestEngine()
	tent := ProtoAddr{Network: 0xFE01, Node: 0x42}
	e.BeginProbe(tent)
	e.NextProbe() // send one

	// A peer is already using our tentative address: it sends a Reply sourced from it.
	intruder := Reply(mac(9, 9, 9, 9, 9, 9), tent, mac(1, 1, 1, 1, 1, 1), ProtoAddr{Network: 0xFE01, Node: 0x01})
	_, conflict := e.Inbound(intruder.Encode(nil), 0)
	if !conflict {
		t.Fatal("Inbound did not report a claim conflict")
	}
	if !e.Conflicted() {
		t.Fatal("Conflicted() false after a conflict")
	}
	if _, ok := e.NextProbe(); ok {
		t.Fatal("NextProbe produced a probe after a conflict")
	}
	if _, ok := e.AcceptTentative(); ok {
		t.Fatal("AcceptTentative succeeded after a conflict")
	}
}

// TestClaimConflictFromSimultaneousProbe proves a peer probing the SAME tentative address
// counts as a conflict (the simultaneous-probe case from the spec).
func TestClaimConflictFromSimultaneousProbe(t *testing.T) {
	e := newTestEngine()
	tent := ProtoAddr{Network: 0xFE01, Node: 0x42}
	e.BeginProbe(tent)

	peerProbe := Probe(mac(9, 9, 9, 9, 9, 9), tent)
	if _, conflict := e.Inbound(peerProbe.Encode(nil), 0); !conflict {
		t.Fatal("a peer probing our tentative address must be a conflict")
	}
}

// TestResolveHitVsMiss proves Resolve returns an AMT hit, and a miss drives StartResolve
// to emit a Request which is satisfied by a Reply (filling the AMT).
func TestResolveHitVsMiss(t *testing.T) {
	e := newTestEngine()
	e.BeginProbe(ProtoAddr{Network: 0xFE01, Node: 0x42})
	e.NextProbe()
	e.AcceptTentative() // claimed

	want := ProtoAddr{Network: 0xFE01, Node: 0x10}

	// Miss → StartResolve emits a Request for `want`.
	if _, ok := e.Resolve(want); ok {
		t.Fatal("Resolve hit on an empty AMT")
	}
	reqBytes := e.StartResolve(want, 0)
	req, err := Decode(reqBytes)
	if err != nil || req.Function != FuncRequest || req.TargetProto != want {
		t.Fatalf("StartResolve request malformed: %+v err=%v", req, err)
	}

	// The owner answers with a Reply → AMT learns it; the pending resolve clears.
	peerMAC := mac(0xAB, 0xCD, 0xEF, 0x01, 0x02, 0x03)
	reply := Reply(peerMAC, want, e.cfg.HardwareAddr, ProtoAddr{Network: 0xFE01, Node: 0x42})
	e.Inbound(reply.Encode(nil), 0)

	hw, ok := e.Resolve(want)
	if !ok || hw != peerMAC {
		t.Fatalf("Resolve after Reply = %v ok=%v, want %v", hw, ok, peerMAC)
	}
}

// TestInboundAnswersRequestForClaimed proves a Request targeting our claimed address gets
// a Reply with our hardware address, and that we glean the requester.
func TestInboundAnswersRequestForClaimed(t *testing.T) {
	e := newTestEngine()
	mine := ProtoAddr{Network: 0xFE01, Node: 0x42}
	e.BeginProbe(mine)
	e.NextProbe()
	e.AcceptTentative()

	askerMAC := mac(7, 7, 7, 7, 7, 7)
	asker := ProtoAddr{Network: 0xFE01, Node: 0x09}
	req := Request(askerMAC, asker, mine)
	replies, _ := e.Inbound(req.Encode(nil), 0)
	if len(replies) != 1 {
		t.Fatalf("got %d replies, want 1", len(replies))
	}
	rp, _ := Decode(replies[0])
	if rp.Function != FuncReply || rp.SrcProto != mine || rp.SrcHw != e.cfg.HardwareAddr {
		t.Fatalf("reply wrong: %+v", rp)
	}
	if rp.TargetHw != askerMAC || rp.TargetProto != asker {
		t.Fatalf("reply not addressed to the asker: %+v", rp)
	}
	// We gleaned the requester.
	if hw, ok := e.Resolve(asker); !ok || hw != askerMAC {
		t.Fatalf("requester not gleaned: hw=%v ok=%v", hw, ok)
	}
}

// TestInboundProbeDeletesAndDefends proves an inbound Probe deletes a cached mapping for
// its source (probe-triggered aging) AND that a probe targeting our claimed address is
// defended with a Reply — while NOT gleaning the tentative source.
func TestInboundProbeDeletesAndDefends(t *testing.T) {
	e := newTestEngine()
	mine := ProtoAddr{Network: 0xFE01, Node: 0x42}
	e.BeginProbe(mine)
	e.NextProbe()
	e.AcceptTentative()

	// Seed a mapping for some address, then a Probe for it deletes it (no glean).
	other := ProtoAddr{Network: 0xFE01, Node: 0x30}
	e.AMT().Glean(other, mac(1, 2, 3, 4, 5, 6), 0)
	probe := Probe(mac(8, 8, 8, 8, 8, 8), other)
	e.Inbound(probe.Encode(nil), 0)
	if _, ok := e.Resolve(other); ok {
		t.Fatal("Probe did not delete the cached mapping")
	}

	// A probe targeting OUR claimed address is defended with a Reply.
	intruder := Probe(mac(9, 9, 9, 9, 9, 9), mine)
	replies, _ := e.Inbound(intruder.Encode(nil), 0)
	if len(replies) != 1 {
		t.Fatalf("claimed-address probe got %d replies, want 1 (defense)", len(replies))
	}
}
