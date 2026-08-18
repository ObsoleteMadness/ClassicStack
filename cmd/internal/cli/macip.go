package cli

import (
	"fmt"
	"net"

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
	// NAT-only (OS sockets, no pcap) does not need a host MAC. Bridge and DHCP-relay
	// inject Ethernet frames and still require one.
	natOnly := params.NATEnabled && !params.DHCPRelay
	if hostMAC == "" && !natOnly {
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

// detectIfaceMACIP returns the host MAC and first IPv4 of the OS interface that
// corresponds to the named pcap device. Shared with the NIC-port HostMAC path
// (hostinfo.InterfaceForDevice). Empty strings when the device cannot be resolved.
func detectIfaceMACIP(pcapName string) (mac, ipv4 string) {
	hd, err := pcapHostDevices()
	if err != nil {
		return "", ""
	}
	ifi, err := hostinfo.InterfaceForDevice(pcapName, hd)
	if err != nil {
		return "", ""
	}
	if len(ifi.HardwareAddr) == 6 {
		mac = ifi.HardwareAddr.String()
	}
	return mac, firstIPv4(ifi)
}

// firstIPv4 returns the first IPv4 address bound to ifi, or "".
func firstIPv4(ifi net.Interface) string {
	addrs, err := ifi.Addrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip4 := ip.To4(); ip4 != nil {
			return ip4.String()
		}
	}
	return ""
}

// ipv4Dotted renders a macip.IPv4 as a dotted-quad string ("" for the zero address).
func ipv4Dotted(a macip.IPv4) string {
	if (a == macip.IPv4{}) {
		return ""
	}
	return net.IP(a[:]).String()
}
