package aarp

import "testing"

// TestProxyReplyRewritesReply proves the atalk-proxy rule: a Reply crossing toward the
// egress gets its sender hardware address replaced with the egress MAC, and round-trips
// on the wire with the new MAC.
func TestProxyReplyRewritesReply(t *testing.T) {
	egress := mac(0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x01)
	reply := Reply(mac(1, 2, 3, 4, 5, 6), ProtoAddr{Network: 1, Node: 0x10},
		mac(9, 8, 7, 6, 5, 4), ProtoAddr{Network: 1, Node: 0x20})

	if !ProxyReply(&reply, egress) {
		t.Fatal("ProxyReply did not rewrite a Reply")
	}
	if reply.SrcHw != egress {
		t.Fatalf("SrcHw = %v, want egress %v", reply.SrcHw, egress)
	}
	// The other fields (target, proto addresses) are untouched.
	if reply.TargetHw != mac(9, 8, 7, 6, 5, 4) {
		t.Fatal("ProxyReply altered the target hardware address")
	}
	// Re-encode/decode confirms the rewrite is on the wire.
	got, err := Decode(reply.Encode(nil))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.SrcHw != egress {
		t.Fatalf("re-decoded SrcHw = %v, want %v", got.SrcHw, egress)
	}
}

// TestProxyReplyLeavesRequestAndProbe proves Requests and Probes pass through unchanged
// (only Replies are rewritten).
func TestProxyReplyLeavesRequestAndProbe(t *testing.T) {
	egress := mac(0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x01)
	src := mac(1, 2, 3, 4, 5, 6)

	req := Request(src, ProtoAddr{Network: 1, Node: 0x10}, ProtoAddr{Network: 1, Node: 0x20})
	if ProxyReply(&req, egress) || req.SrcHw != src {
		t.Fatal("ProxyReply must not rewrite a Request")
	}

	probe := Probe(src, ProtoAddr{Network: 1, Node: 0x10})
	if ProxyReply(&probe, egress) || probe.SrcHw != src {
		t.Fatal("ProxyReply must not rewrite a Probe")
	}
}

// TestRewriteSenderHardwareNoOp proves a rewrite to the same MAC reports no change.
func TestRewriteSenderHardwareNoOp(t *testing.T) {
	same := mac(1, 2, 3, 4, 5, 6)
	p := Reply(same, ProtoAddr{Network: 1, Node: 1}, mac(9), ProtoAddr{Network: 1, Node: 2})
	if p.RewriteSenderHardware(same) {
		t.Fatal("rewrite to the same MAC reported a change")
	}
}
