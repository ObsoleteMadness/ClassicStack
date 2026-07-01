package framing

import (
	"bytes"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/protocol/aarp"
)

func macOf(b ...byte) [6]byte {
	var m [6]byte
	copy(m[:], b)
	return m
}

// aarpFrame wraps an AARP packet in the EtherTalk 802.3/SNAP frame with the given
// destination/source Ethernet MACs (mirrors what aarpLink.writeAARP builds).
func aarpFrame(dstMAC, srcMAC [6]byte, p aarp.Packet) []byte {
	return appendEthSNAP(nil, dstMAC[:], srcMAC[:], snapAARP, p.Encode(nil))
}

// TestProxyRewriteFrameRewritesReply proves an AARP Reply crossing toward egress has BOTH
// its AARP sender-hardware and its outer Ethernet source MAC rewritten to the egress MAC,
// while the destination MAC and the AARP target are preserved.
func TestProxyRewriteFrameRewritesReply(t *testing.T) {
	egress := macOf(0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x01)
	station := macOf(1, 2, 3, 4, 5, 6)
	requester := macOf(9, 8, 7, 6, 5, 4)

	reply := aarp.Reply(station, aarp.ProtoAddr{Network: 1, Node: 0x10},
		requester, aarp.ProtoAddr{Network: 1, Node: 0x20})
	frame := aarpFrame(requester, station, reply)

	out, changed := ProxyRewriteFrame(frame, egress)
	if !changed {
		t.Fatal("ProxyRewriteFrame did not rewrite an AARP Reply")
	}
	// Ethernet source MAC rewritten to egress; destination preserved.
	if !bytes.Equal(out[6:12], egress[:]) {
		t.Fatalf("ethernet src = %x, want egress %x", out[6:12], egress)
	}
	if !bytes.Equal(out[0:6], requester[:]) {
		t.Fatalf("ethernet dst = %x, want preserved %x", out[0:6], requester)
	}
	// Decode the AARP payload back and check the sender-hardware was rewritten too.
	pid, off, ok := snapPIDOf(out)
	if !ok || !equal(pid, snapAARP) {
		t.Fatal("rewritten frame is not an AARP SNAP frame")
	}
	got, err := aarp.Decode(out[off:])
	if err != nil {
		t.Fatalf("decode rewritten AARP: %v", err)
	}
	if got.SrcHw != egress {
		t.Fatalf("AARP SrcHw = %x, want egress %x", got.SrcHw, egress)
	}
	if got.TargetHw != requester {
		t.Fatal("ProxyRewriteFrame altered the AARP target hardware address")
	}
}

// TestProxyRewriteFrameLeavesRequestProbeDDP proves non-Reply AARP (Request/Probe) and
// non-AARP frames (DDP, noise) pass through: changed=false, out=nil (caller forwards the
// original verbatim).
func TestProxyRewriteFrameLeavesRequestProbeDDP(t *testing.T) {
	egress := macOf(0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x01)
	station := macOf(1, 2, 3, 4, 5, 6)

	req := aarp.Request(station, aarp.ProtoAddr{Network: 1, Node: 0x10}, aarp.ProtoAddr{Network: 1, Node: 0x20})
	if out, changed := ProxyRewriteFrame(aarpFrame(appleTalkBroadcast(), station, req), egress); changed || out != nil {
		t.Fatal("ProxyRewriteFrame must not rewrite an AARP Request")
	}

	probe := aarp.Probe(station, aarp.ProtoAddr{Network: 1, Node: 0x10})
	if out, changed := ProxyRewriteFrame(aarpFrame(appleTalkBroadcast(), station, probe), egress); changed || out != nil {
		t.Fatal("ProxyRewriteFrame must not rewrite an AARP Probe")
	}

	// A plain (non-SNAP-AARP) frame passes through: build a DDP SNAP frame.
	ddpFrame := appendEthSNAP(nil, appleTalkBroadcastMAC, station[:], snapAppleTalk, []byte{0x00, 0x01, 0x02})
	if out, changed := ProxyRewriteFrame(ddpFrame, egress); changed || out != nil {
		t.Fatal("ProxyRewriteFrame must not touch a DDP frame")
	}

	// A too-short / non-SNAP frame passes through.
	if out, changed := ProxyRewriteFrame([]byte{1, 2, 3}, egress); changed || out != nil {
		t.Fatal("ProxyRewriteFrame must not touch a short frame")
	}
}

func appleTalkBroadcast() [6]byte { return macOf(0x09, 0x00, 0x07, 0xFF, 0xFF, 0xFF) }
