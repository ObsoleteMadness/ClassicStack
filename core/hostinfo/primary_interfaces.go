//go:build !tinygo

package hostinfo

import "net"

// primary_interfaces.go holds the primary.go functions that walk net.Interfaces()/
// Interface.Addrs() / net.InterfaceByName — APIs TinyGo's baremetal net package does
// not implement (see primary_interfaces_tinygo.go's stubs). Split out so primary.go
// itself (PrimaryIP, PrimaryDevice — both TinyGo-compatible, using only net.Dial and
// net.ParseIP) compiles unconditionally.

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
