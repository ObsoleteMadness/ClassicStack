package ltoudp

import (
	"net"
	"strings"
)

// classifyMulticastInterfaces splits host interfaces into LAN candidates (Wi-Fi,
// Ethernet, bridges) and loopback. VPN/AirDrop/tunnels are dropped: joining or
// sending on them keeps TTL-1 LToUDP packets off the shared LAN segment.
func classifyMulticastInterfaces(ifaces []net.Interface) (lan, loopback []*net.Interface) {
	for i := range ifaces {
		intf := &ifaces[i]
		if intf.Flags&net.FlagUp == 0 || intf.Flags&net.FlagMulticast == 0 {
			continue
		}
		if !interfaceHasIPv4(intf) {
			continue
		}
		if intf.Flags&net.FlagLoopback != 0 {
			loopback = append(loopback, intf)
			continue
		}
		if !isHostLANInterface(intf) {
			continue
		}
		lan = append(lan, intf)
	}
	return lan, loopback
}

// isHostLANInterface reports whether intf is a broadcast LAN the LToUDP segment
// should ride. Point-to-point (utun/VPN) and Apple peer-to-peer/tunnel names
// never reach other machines on the operator's Ethernet/Wi-Fi.
func isHostLANInterface(intf *net.Interface) bool {
	if intf == nil {
		return false
	}
	if intf.Flags&net.FlagPointToPoint != 0 {
		return false
	}
	n := strings.ToLower(intf.Name)
	for _, p := range []string{"awdl", "llw", "gif", "stf", "anpi"} {
		if n == p || strings.HasPrefix(n, p) {
			return false
		}
	}
	return true
}

// pickSendInterface chooses the NIC outbound LToUDP datagrams are pinned to.
// prefer (typically the default-route interface) wins when it is in the LAN
// join set; otherwise the first LAN NIC.
func pickSendInterface(lan []*net.Interface, prefer *net.Interface) *net.Interface {
	if prefer != nil && prefer.Index != 0 {
		for _, intf := range lan {
			if intf != nil && intf.Index == prefer.Index {
				return intf
			}
		}
	}
	if len(lan) > 0 {
		return lan[0]
	}
	return nil
}
