package main

import (
	"net"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/port/rawlink"
)

// broadcastAddr computes the broadcast address of an IP network.
func broadcastAddr(n *net.IPNet) net.IP {
	ip := n.IP.To4()
	bcast := make(net.IP, 4)
	for i := range bcast {
		bcast[i] = ip[i] | ^n.Mask[i]
	}
	return bcast
}

// detectPcapInterfaceIPv4 returns the preferred IPv4 address bound to the
// named pcap interface, if any.
func detectPcapInterfaceIPv4(interfaceName string) (string, bool) {
	if strings.TrimSpace(interfaceName) == "" {
		return "", false
	}
	devs, err := rawlink.ListPcapDevices()
	if err != nil {
		return "", false
	}
	for _, d := range devs {
		if d.Name != interfaceName {
			continue
		}
		return selectPreferredIPv4(d.Addresses)
	}
	return "", false
}

// firstUsableIPv4 returns the first host address in n (network address + 1),
// or nil when n has no usable host address.
func firstUsableIPv4(n *net.IPNet) net.IP {
	if n == nil {
		return nil
	}
	base := n.IP.To4()
	if base == nil || len(n.Mask) != net.IPv4len {
		return nil
	}
	candidate := append(net.IP(nil), base...)
	for i := len(candidate) - 1; i >= 0; i-- {
		candidate[i]++
		if candidate[i] != 0 {
			break
		}
	}
	if !n.Contains(candidate) || candidate.Equal(broadcastAddr(n)) {
		return nil
	}
	return candidate.To4()
}

// selectPreferredIPv4 picks the most useful IPv4 address from a list,
// preferring a routable address over an APIPA link-local one and skipping
// unspecified/loopback addresses. Used when resolving an interface's
// address for MacIP and diagnostics.
func selectPreferredIPv4(addrs []string) (string, bool) {
	var linkLocal string
	for _, addr := range addrs {
		ip := net.ParseIP(strings.TrimSpace(addr)).To4()
		if ip == nil || ip.IsUnspecified() || ip.IsLoopback() {
			continue
		}
		if ip[0] == 169 && ip[1] == 254 {
			if linkLocal == "" {
				linkLocal = ip.String()
			}
			continue
		}
		return ip.String(), true
	}

	if linkLocal != "" {
		return linkLocal, true
	}

	return "", false
}
