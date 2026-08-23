//go:build !tinygo

// osInterfaceIPv4 falls back to net.InterfaceByName when the pcap device listing
// (clientlink.ListInterfaces) has no match -- TinyGo's baremetal targets don't
// implement it, so they skip straight to a nil/no-match result instead (see
// ipv4_fallback_tinygo.go).

package browse

import "net"

func osInterfaceIPv4(device string) net.IP {
	ifi, err := net.InterfaceByName(device)
	if err != nil {
		return nil
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return nil
	}
	dotted := make([]string, 0, len(addrs))
	for _, a := range addrs {
		dotted = append(dotted, a.String())
	}
	return firstIPv4(dotted)
}
