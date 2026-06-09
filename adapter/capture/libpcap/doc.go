// Package libpcap is the gopacket/pcapgo-backed CaptureSink (§6f, M1), used with
// the libpcap link adapter. It implements core/link.CaptureSink by delegating to
// gopacket's pcapgo writer, confining gopacket to an adapter.
//
// Functionally it produces the same Wireshark-openable .pcap as the pure-Go
// adapter/capture/pcapfile; prefer pcapfile for non-pcap links and TinyGo
// targets, and this one when gopacket is already linked (the pcap link path).
//
// Ring: adapter. May import gopacket.
package libpcap
