// Package bridge is the proxy-AARP Wi-Fi/tunnel bridge (ring: adapter). It forwards raw
// AppleTalk frames between TWO FrameLinks — a "tunnel" side (a local Ethernet segment, a
// GRE/UDP tunnel, a wired NIC) and an "egress" side (typically a Wi-Fi interface) — so
// AppleTalk works across a link layer that cannot transparently bridge MAC addresses.
//
// A plain L2 bridge would just copy frames both ways, but Wi-Fi (and many tunnels) will
// not carry a station's real source MAC: an AP only forwards frames sourced from MACs it
// has associated. So on the tunnel→egress direction the bridge applies the atalk-proxy
// transform (framing.ProxyRewriteFrame over core/protocol/aarp.ProxyReply): an AARP Reply
// crossing to the egress side has its AARP sender-hardware AND Ethernet source MAC
// rewritten to the EGRESS interface's own MAC, so remote Wi-Fi stations learn to reach the
// bridged node via the proxy. AARP Requests/Probes and DDP data frames pass through
// unchanged in both directions. Refs: jcs/atalk-proxy, Linux net/appletalk/aarp.c proxies[].
//
// The component is transport-agnostic: it takes two per-Start FrameLink openers (pcap at
// the cmd edge, or inmem in tests) and the egress MAC, exactly like the ports take an
// injected opener — core/adapter never import the pcap/cgo backend directly.
package bridge
