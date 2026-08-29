// Package pcap — see doc.go. This file has no build tag: ExcludeSelf is pure string
// construction (no cgo/gopacket), so it is available identically to callers whether
// the tagged libpcap backend or the no-op stub is compiled in.
package pcap

import "fmt"

// ExcludeSelf combines a protocol capture filter with a clause excluding frames
// sourced from mac. A promiscuous handle that both reads and writes the same NIC can
// see its own transmitted frames reflected back by the kernel/driver; ANDing in "not
// ether src <mac>" keeps those out of the capture at the kernel, the same convention
// NIC emulators (e.g. 86Box) use to exclude their own virtual adapter's MAC from their
// capture filter. A zero mac (station identity not yet known/configured) is a no-op —
// filter is returned unchanged, and callers fall back to the software dedup layer
// (core/link.Dedup) for loopback suppression.
func ExcludeSelf(filter string, mac [6]byte) string {
	if mac == ([6]byte{}) {
		return filter
	}
	excl := fmt.Sprintf("not (ether src %02x:%02x:%02x:%02x:%02x:%02x)",
		mac[0], mac[1], mac[2], mac[3], mac[4], mac[5])
	if filter == "" {
		return excl
	}
	return fmt.Sprintf("(%s) and %s", filter, excl)
}
