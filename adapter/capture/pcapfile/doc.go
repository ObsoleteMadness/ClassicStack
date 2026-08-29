// Package pcapfile is a pure-Go, stdlib-only, TinyGo-safe writer for the classic
// libpcap capture file format (§6f, M1). It implements core/link.CaptureSink, so
// the Capture decorator can tee frames from ANY FrameLink — TAP, ESP32-raw,
// TashTalk tty, in-mem loopback — to a Wireshark-openable .pcap with no libpcap
// or gopacket linked.
//
// It writes the original (microsecond) pcap format, little-endian, which every
// version of Wireshark/tshark reads. For the libpcap-linked dumper used with the
// pcap link, see adapter/capture/libpcap instead.
//
// Ring: adapter. Stdlib-only on purpose (TinyGo-safe); no gopacket.
package pcapfile
