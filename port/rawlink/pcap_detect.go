package rawlink

import (
	"net"
	"sort"
	"strings"

	tsfaces "tailscale.com/net/interfaces"
)

// DetectDefaultPcapInterface finds the pcap device name for the machine's
// default-route interface by matching the local IP reported by
// LikelyHomeRouterIP against the addresses on each pcap device.
func DetectDefaultPcapInterface() (string, bool) {
	devs, err := ListPcapDevices()
	if err != nil {
		return "", false
	}

	_, myIP, ok := tsfaces.LikelyHomeRouterIP()
	if ok {
		wantIP := net.IP(myIP.Unmap().AsSlice()).To4()
		if wantIP != nil {
			for _, d := range devs {
				for _, addr := range d.Addresses {
					if parsed := parsePcapIP(addr); parsed != nil && parsed.Equal(wantIP) {
						return d.Name, true
					}
				}
			}
		}
	}

	return detectAnyUsablePcapInterface(devs)
}

// DetectDefaultGatewayForPcapInterface returns the default route gateway IP
// only when the selected pcap interface matches the default-route interface.
func DetectDefaultGatewayForPcapInterface(interfaceName string) (string, bool) {
	gw, myIP, ok := tsfaces.LikelyHomeRouterIP()
	if !ok {
		return "", false
	}

	wantIP := net.IP(myIP.Unmap().AsSlice()).To4()
	if wantIP == nil {
		return "", false
	}
	devs, err := ListPcapDevices()
	if err != nil {
		return "", false
	}

	for _, d := range devs {
		if d.Name != interfaceName {
			continue
		}
		for _, addr := range d.Addresses {
			if parsed := parsePcapIP(addr); parsed != nil && parsed.Equal(wantIP) {
				ip := net.IP(gw.Unmap().AsSlice())
				if ip == nil || ip.IsUnspecified() {
					return "", false
				}
				return ip.String(), true
			}
		}
		break
	}

	return "", false
}

// DetectDefaultGatewayIP returns the likely upstream gateway IP for the
// machine's default route using LikelyHomeRouterIP.
func DetectDefaultGatewayIP() (string, bool) {
	gw, _, ok := tsfaces.LikelyHomeRouterIP()
	if !ok {
		return "", false
	}
	ip := net.IP(gw.Unmap().AsSlice())
	if ip == nil || ip.IsUnspecified() {
		return "", false
	}
	return ip.String(), true
}

// DetectHostMACForPcapInterface returns the host interface MAC for the given
// pcap device by matching pcap IPv4 addresses against OS interfaces.
func DetectHostMACForPcapInterface(interfaceName string) (string, bool) {
	devs, err := ListPcapDevices()
	if err != nil {
		return "", false
	}

	ipv4Set := map[string]struct{}{}
	for _, d := range devs {
		if d.Name != interfaceName {
			continue
		}
		for _, addr := range d.Addresses {
			if ip := parsePcapIP(addr); ip != nil {
				ipv4Set[ip.String()] = struct{}{}
			}
		}
		break
	}
	if len(ipv4Set) == 0 {
		return "", false
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return "", false
	}

	// Keep deterministic selection if multiple interfaces share the same IPv4.
	sort.Slice(ifaces, func(i, j int) bool { return ifaces[i].Name < ifaces[j].Name })

	for _, iface := range ifaces {
		if len(iface.HardwareAddr) != 6 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			ip4 := ip.To4()
			if ip4 == nil {
				continue
			}
			if _, ok := ipv4Set[ip4.String()]; ok {
				return iface.HardwareAddr.String(), true
			}
		}
	}

	return "", false
}

func parsePcapIP(addr string) net.IP {
	ip := strings.TrimSpace(addr)
	if slash := strings.IndexByte(ip, '/'); slash >= 0 {
		ip = ip[:slash]
	}
	return net.ParseIP(ip).To4()
}

func detectAnyUsablePcapInterface(devs []PcapDeviceInfo) (string, bool) {
	var fallback string
	for _, d := range devs {
		for _, addr := range d.Addresses {
			ip := parsePcapIP(addr)
			if ip == nil || ip.IsUnspecified() || ip.IsLoopback() {
				continue
			}
			if ip[0] == 169 && ip[1] == 254 {
				if fallback == "" {
					fallback = d.Name
				}
				continue
			}
			return d.Name, true
		}
	}

	if fallback != "" {
		return fallback, true
	}

	return "", false
}
