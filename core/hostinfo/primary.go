package hostinfo

import (
	"errors"
	"net"
	"strings"
)

// primary.go finds the host's "primary" network interface — the one the OS routing
// table would use to reach the outside world — without parsing per-OS routing tables.
// It is pcap-free and needs no privileges, so both the file clients (auto-filling an
// omitted -iface) and a server "Easy mode" (auto-picking a NIC when config names none)
// share the same detection. detectHostIPAndMAC's "first up NIC" heuristic in the
// diagnostics_*.go files is a fallback; PrimaryInterface here is routing-table accurate.

// ErrNoPrimaryInterface is returned when no primary (default-route) interface can be
// resolved — a host with no default route, or only loopback.
var ErrNoPrimaryInterface = errors.New("hostinfo: no primary interface (no default route)")

// probeTarget is the off-host address a stateless UDP "connect" is aimed at so the OS
// reveals which local address its routing table would source from. No datagram is sent
// (UDP connect only fixes the socket's peer + selects a route), so the target need not
// be reachable — it just has to be a routable public address the default route covers.
const probeTarget = "8.8.8.8:80"

// PrimaryIP returns the local IPv4 address the host's routing table would use as the
// source when reaching an off-host destination — i.e. the address of the default-route
// interface. It works by opening (not sending on) a UDP socket to a public address and
// reading back the local address the kernel bound; this consults the OS routing table
// directly and is identical across Windows/Linux/macOS, needing no pcap and no
// privileges. It returns ErrNoPrimaryInterface if no route can be resolved.
func PrimaryIP() (net.IP, error) {
	conn, err := net.Dial("udp4", probeTarget)
	if err != nil {
		return nil, ErrNoPrimaryInterface
	}
	defer conn.Close()
	ua, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || ua.IP == nil || ua.IP.IsUnspecified() {
		return nil, ErrNoPrimaryInterface
	}
	return append(net.IP(nil), ua.IP...), nil
}

// Device is a minimal, pcap-free view of one capturable NIC — the name a raw-link
// backend opens by, plus the IP addresses bound to it. It lets PrimaryDevice match the
// routing-table primary interface to a backend device WITHOUT this package importing
// pcap: the caller (cmd edge / client ring) supplies the device list it already has.
type Device struct {
	Name      string   // backend device name (e.g. "\Device\NPF_{GUID}" or "eth0")
	Addresses []string // bound IP addresses, as bare strings ("192.168.0.108")
}

// PrimaryDevice picks, from devices, the one bound to the host's primary (default-route)
// interface — the device a NIC backend should open when the operator named none. It
// resolves PrimaryIP, then returns the first device that lists an equal address. This is
// the shared IP→device bridge behind both the server run-core's auto-NIC and the file
// clients' auto -iface: on Windows a pcap device name is "\Device\NPF_{GUID}", not
// derivable from the OS interface name, so only an IP match connects the two. It returns
// ErrNoPrimaryInterface when there is no default route or no device matches it.
func PrimaryDevice(devices []Device) (Device, error) {
	ip, err := PrimaryIP()
	if err != nil {
		return Device{}, err
	}
	for _, d := range devices {
		for _, a := range d.Addresses {
			if pd := net.ParseIP(a); pd != nil && pd.Equal(ip) {
				return d, nil
			}
		}
	}
	return Device{}, ErrNoPrimaryInterface
}

// PrimaryInterface returns the host interface that carries the default route, resolved
// by matching PrimaryIP against each interface's bound addresses. This is the NIC a
// caller should default to when the user has not named one. It returns
// ErrNoPrimaryInterface if the primary IP cannot be resolved or matched to an interface.
func PrimaryInterface() (net.Interface, error) {
	ip, err := PrimaryIP()
	if err != nil {
		return net.Interface{}, err
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return net.Interface{}, ErrNoPrimaryInterface
	}
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ifIP net.IP
			switch a := addr.(type) {
			case *net.IPNet:
				ifIP = a.IP
			case *net.IPAddr:
				ifIP = a.IP
			}
			if ifIP != nil && ifIP.Equal(ip) {
				return iface, nil
			}
		}
	}
	return net.Interface{}, ErrNoPrimaryInterface
}

// ErrNoHardwareAddr is returned when HardwareAddrForDevice / InterfaceForDevice cannot
// resolve a NIC: the name is unknown to both the supplied device list and the OS.
var ErrNoHardwareAddr = errors.New("hostinfo: no hardware address for device")

// InterfaceForDevice resolves the OS interface that corresponds to a pcap/Npcap device
// name. On Windows the pcap name is `\Device\NPF_{GUID}`, which does not match
// net.InterfaceByName, so the first path matches the device's listed IPs against
// net.Interfaces. On Linux/macOS the pcap name IS the OS name (en0, wlan0), so a miss
// on the IP walk falls back to InterfaceByName. devices is the caller-supplied pcap
// inventory (this package stays pcap-free). An empty name, or a name that matches
// neither path, returns ErrNoHardwareAddr.
func InterfaceForDevice(name string, devices []Device) (net.Interface, error) {
	if name == "" {
		return net.Interface{}, ErrNoHardwareAddr
	}
	var ips []net.IP
	for _, d := range devices {
		if d.Name != name {
			continue
		}
		for _, a := range d.Addresses {
			if ip := parseDeviceIP(a); ip != nil {
				ips = append(ips, ip)
			}
		}
		break
	}
	if len(ips) > 0 {
		ifaces, err := net.Interfaces()
		if err == nil {
			for _, iface := range ifaces {
				if interfaceHasIP(iface, ips) {
					return iface, nil
				}
			}
		}
	}
	ifi, err := net.InterfaceByName(name)
	if err != nil {
		return net.Interface{}, ErrNoHardwareAddr
	}
	return *ifi, nil
}

// HardwareAddrForDevice returns the 6-byte Ethernet MAC of the OS interface that
// corresponds to a pcap/Npcap device name. See InterfaceForDevice for the match
// rules. A resolved interface whose HardwareAddr is not 6 bytes (e.g. a tunnel)
// returns ErrNoHardwareAddr.
func HardwareAddrForDevice(name string, devices []Device) ([6]byte, error) {
	ifi, err := InterfaceForDevice(name, devices)
	if err != nil {
		return [6]byte{}, err
	}
	if len(ifi.HardwareAddr) != 6 {
		return [6]byte{}, ErrNoHardwareAddr
	}
	var mac [6]byte
	copy(mac[:], ifi.HardwareAddr)
	return mac, nil
}

// parseDeviceIP parses a pcap address string (optionally CIDR) to an IP, or nil.
func parseDeviceIP(addr string) net.IP {
	a := strings.TrimSpace(addr)
	if slash := strings.IndexByte(a, '/'); slash >= 0 {
		a = a[:slash]
	}
	return net.ParseIP(a)
}

// interfaceHasIP reports whether iface has any of the given addresses bound.
func interfaceHasIP(iface net.Interface, ips []net.IP) bool {
	addrs, err := iface.Addrs()
	if err != nil {
		return false
	}
	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip == nil {
			continue
		}
		for _, want := range ips {
			if ip.Equal(want) {
				return true
			}
		}
	}
	return false
}
