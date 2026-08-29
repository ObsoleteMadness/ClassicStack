//go:build tinygo

package hostinfo

import "net"

// primary_interfaces_tinygo.go stubs the net.Interfaces()/Interface.Addrs()/
// net.InterfaceByName-based lookups (primary_interfaces.go) for TinyGo baremetal
// targets, whose net package implements neither. An embedded target typically has one
// fixed hardware interface wired up directly by its own main.go (e.g.
// hardware/esp32/wt32eth01's custom LinkOpener over the LAN8720A PHY), bypassing this
// multi-NIC auto-detection entirely — so "no primary interface resolvable" is the
// honest answer here, not a real gap for those targets. Mirrors the 0/0 "unknown"
// posture core/fs/diskusage_other.go and hostinfo/diagnostics_tinygo.go already take
// for the same class of OS-API-not-on-TinyGo gap.

func PrimaryInterface() (net.Interface, error) {
	return net.Interface{}, ErrNoPrimaryInterface
}

func InterfaceForDevice(name string, devices []Device) (net.Interface, error) {
	_, _ = name, devices
	return net.Interface{}, ErrNoHardwareAddr
}

func HardwareAddrForDevice(name string, devices []Device) ([6]byte, error) {
	_, _ = name, devices
	return [6]byte{}, ErrNoHardwareAddr
}
