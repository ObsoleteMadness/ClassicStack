package framing

// proxyaarp.go is the FRAME-level half of the proxy-AARP transform used by the Wi-Fi/
// tunnel bridge (adapter/bridge). The PURE, decoded-packet rule lives in
// core/protocol/aarp (ProxyReply / RewriteSenderHardware); this file is the adapter glue
// that finds an AARP packet inside a full Ethernet/SNAP frame, applies that rule, and
// rewrites the Ethernet source MAC to match — the two edits an atalk-proxy makes when it
// forwards a Reply from the tunnel/local side onto the egress interface.
//
// It lives here (not in core) because pulling the AARP packet out of an 802.3/802.2/SNAP
// frame is Ethernet framing, which this package owns; core stays free of the wire header.
// Reuses the in-package snapPIDOf/snapAARP + appendEthSNAP helpers.

import (
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/aarp"
)

// ProxyRewriteFrame applies the atalk-proxy transform to one Ethernet frame crossing
// toward the egress interface, given the egress interface's own MAC. It reports whether
// it produced a rewritten frame:
//
//   - a frame that is NOT an EtherTalk AARP frame (plain DDP, non-SNAP, malformed) →
//     changed=false, out=nil (the caller forwards the ORIGINAL frame verbatim);
//   - an AARP frame that is NOT a Reply (Request/Probe) → changed=false, out=nil (pass
//     through unchanged, so address discovery still works end-to-end);
//   - an AARP Reply → changed=true, out=<a NEW frame> whose AARP sender-hardware AND
//     Ethernet source MAC are both set to egressMAC, so remote stations learn to reach
//     the bridged node via the proxy's MAC (the only way when MACs can't be bridged
//     transparently, e.g. Wi-Fi).
//
// out is a freshly allocated frame; the original is never mutated. changed=false always
// yields out=nil so the caller can branch cheaply.
func ProxyRewriteFrame(frame []byte, egressMAC [6]byte) (out []byte, changed bool) {
	pid, off, ok := snapPIDOf(frame)
	if !ok || !equal(pid, snapAARP) {
		return nil, false // not an AARP frame — forward as-is
	}
	pkt, err := aarp.Decode(frame[off:])
	if err != nil {
		return nil, false // not a decodable EtherTalk AARP packet — forward as-is
	}
	if !aarp.ProxyReply(&pkt, egressMAC) {
		return nil, false // Request/Probe (or already egress-sourced) — forward as-is
	}
	// Rewrite the outer Ethernet source MAC to match the rewritten AARP sender-hardware,
	// then re-frame the packet. The destination MAC is preserved (AARP replies are
	// unicast to the requester; a broadcasted reply keeps its broadcast dst).
	dstMAC := frame[0:6]
	return appendEthSNAP(nil, dstMAC, egressMAC[:], snapAARP, pkt.Encode(nil)), true
}
