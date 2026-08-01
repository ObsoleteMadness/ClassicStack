package cli

import (
	"fmt"
	"net"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/adapter/link/pcap"
	"github.com/ObsoleteMadness/ClassicStack/adapter/macipgw"
	"github.com/ObsoleteMadness/ClassicStack/compose/runtime"
	"github.com/ObsoleteMadness/ClassicStack/core/hostinfo"
	"github.com/ObsoleteMadness/ClassicStack/core/service/macip"
)

// macipEgressOpener is the runtime's MacIP IP-side egress opener: it builds the
// proxy-ARP / NAT / DHCP-relay egress (adapter/macipgw) over a libpcap link on the
// section's interface, auto-detecting the host MAC / host IP / default gateway from
// that interface where the operator left them blank. It lives at the cmd edge so
// compose/runtime pulls in no pcap/cgo dependency; under the pcap tag it opens a real
// link, otherwise pcap.Open returns ErrUnavailable and the egress build fails, leaving
// MacIP AppleTalk-only. ipv4 → dotted quad mapping mirrors macip's own.
func macipEgressOpener(params macip.EgressParams, ownsIP func(macip.IPv4) bool) (runtime.MacIPEgress, error) {
	if params.Interface == "" {
		return nil, nil // no IP egress configured → AppleTalk-only
	}

	hostMAC := params.HostMAC
	hostIP := params.HostIP
	defGW := params.DefaultGateway

	// Best-effort auto-detection from the chosen pcap interface for any blank field.
	if hostMAC == "" || hostIP == "" {
		mac, ip := detectIfaceMACIP(params.Interface)
		if hostMAC == "" {
			hostMAC = mac
		}
		if hostIP == "" {
			hostIP = ip
		}
	}
	if defGW == "" {
		// Auto-detect the host's default-route gateway from the OS routing table (the
		// real upstream router, e.g. 192.168.0.1). This is the gateway advertised to
		// MacTCP in bridge mode and the next hop for off-subnet bridge sends; the legacy
		// run-core resolved it the same way (DetectDefaultGatewayForPcapInterface).
		if gw, err := hostinfo.DefaultGateway(); err == nil {
			defGW = gw.String()
		}
	}
	if defGW == "" {
		// Last resort when the routing table could not be read: the host IP still gives
		// off-subnet bridge sends SOME next hop (NAT mode ignores it), though it is not a
		// real gateway. The MacIP gateway logs when it advertises this fallback.
		defGW = hostIP
	}
	if hostMAC == "" {
		return nil, fmt.Errorf("macip: host MAC could not be auto-detected for interface %q; set host_mac", params.Interface)
	}

	cfg := macipgw.Config{
		Interface:      params.Interface,
		HostMAC:        hostMAC,
		HostIP:         hostIP,
		DefaultGateway: defGW,
		GatewayIP:      ipv4Dotted(params.GatewayIP),
		Network:        ipv4Dotted(params.Network),
		SubnetMask:     ipv4Dotted(params.SubnetMask),
		NATEnabled:     params.NATEnabled,
		DHCPRelay:      params.DHCPRelay,
	}
	eg, err := macipgw.New(cfg, ownsIP, nil)
	if err != nil {
		return nil, err
	}
	return eg, nil
}

// detectIfaceMACIP returns the host MAC and first IPv4 of the OS interface whose IPv4
// addresses match the named pcap device. Returns empty strings when the device or a
// matching OS interface cannot be found (the caller then requires explicit config).
func detectIfaceMACIP(pcapName string) (mac, ipv4 string) {
	devs, err := pcap.ListDevices()
	if err != nil {
		return "", ""
	}
	want := map[string]struct{}{}
	for _, d := range devs {
		if d.Name != pcapName {
			continue
		}
		for _, a := range d.Addresses {
			if ip := parseIP4(a); ip != nil {
				want[ip.String()] = struct{}{}
			}
		}
		break
	}
	if len(want) == 0 {
		return "", ""
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", ""
	}
	for _, iface := range ifaces {
		if len(iface.HardwareAddr) != 6 {
			continue
		}
		addrs, _ := iface.Addrs()
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
			if _, ok := want[ip4.String()]; ok {
				return iface.HardwareAddr.String(), ip4.String()
			}
		}
	}
	return "", ""
}

// parseIP4 parses a pcap address string (optionally CIDR) to an IPv4, or nil.
func parseIP4(addr string) net.IP {
	a := strings.TrimSpace(addr)
	if slash := strings.IndexByte(a, '/'); slash >= 0 {
		a = a[:slash]
	}
	return net.ParseIP(a).To4()
}

// ipv4Dotted renders a macip.IPv4 as a dotted-quad string ("" for the zero address).
func ipv4Dotted(a macip.IPv4) string {
	if (a == macip.IPv4{}) {
		return ""
	}
	return net.IP(a[:]).String()
}
