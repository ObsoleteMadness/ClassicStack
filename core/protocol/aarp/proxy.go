package aarp

// proxy.go is the OPTIONAL proxy-AARP transform: the stateless packet rewrite a
// two-interface AppleTalk bridge applies so AppleTalk works across a link layer that
// cannot transparently bridge MAC addresses — most importantly Wi-Fi (refs:
// jcs/atalk-proxy, and the Linux kernel's proxies[] table in net/appletalk/aarp.c).
//
// The rule (from atalk-proxy): an AARP REPLY (op=2) forwarded from the local/tunnel
// side toward the egress interface has its SENDER hardware address rewritten to the
// egress interface's own MAC. Remote stations then learn that the proxy's MAC is where
// to send AppleTalk traffic for the bridged node, so they route through the proxy — the
// only way to reach the node when MACs cannot be bridged transparently (Wi-Fi). AARP
// Requests and Probes are left UNCHANGED so address discovery still works end-to-end.
//
// This is a PURE, stateless transform (no AMT, no node-claim — unrelated to the station
// AARP Engine). The two-interface forwarding plumbing that drives it (reading from one
// interface, rewriting, injecting on the other) is an adapter/compose feature (the
// Wi-Fi/tunnel bridge), not part of this package — but the transform itself lives here so
// the bridge has one correct, tested implementation.

// RewriteSenderHardware sets the packet's sender hardware address to mac and reports
// whether it changed anything. It is the core proxy step; the bridge decides WHEN to
// call it (per the ProxyReply policy below).
func (p *Packet) RewriteSenderHardware(mac [6]byte) bool {
	if p.SrcHw == mac {
		return false
	}
	p.SrcHw = mac
	return true
}

// ProxyReply applies the atalk-proxy rule to a packet crossing from the tunnel/local
// side toward the egress interface: if it is an AARP Reply, rewrite its sender hardware
// address to the egress MAC and report changed=true; Requests and Probes pass through
// unchanged (changed=false). The caller re-encodes p when changed is true.
func ProxyReply(p *Packet, egressMAC [6]byte) (changed bool) {
	if p.Function != FuncReply {
		return false
	}
	return p.RewriteSenderHardware(egressMAC)
}
