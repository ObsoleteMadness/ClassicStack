// Package pcap is the libpcap/Npcap FrameLink adapter (§2, M1). It is the real
// L2 capture/inject backend behind core/link.FrameLink, confining the gopacket
// and libpcap (cgo) dependencies to this adapter — the archtest gate (A2)
// forbids them anywhere under core/.
//
// The adapter also satisfies the optional core/link capabilities: MediumReporter
// (physical medium for Wi-Fi bridge selection) and FilterableLink (kernel BPF).
// BPF filter *strings* live here, at the adapter, never in the ports (§2).
//
// Ring: adapter. May import gopacket/pcap/cgo; must present only the core/link
// interfaces upward.
package pcap
