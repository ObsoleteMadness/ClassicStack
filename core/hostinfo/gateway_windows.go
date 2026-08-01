package hostinfo

import (
	"net"
	"syscall"
	"unsafe"
)

// gateway_windows.go resolves the default gateway via iphlpapi!GetBestRoute, which
// returns the routing-table's best MIB_IPFORWARDROW for a destination. Asking for the
// route to a public address (probeGatewayTarget) yields the default route, whose
// dwForwardNextHop is the upstream gateway. We use syscall.NewLazyDLL directly (the
// project convention, keeping x/sys out of core) — see core/fs/fork_ads_ntfs_windows.go.

var (
	iphlpapiDLL      = syscall.NewLazyDLL("iphlpapi.dll")
	procGetBestRoute = iphlpapiDLL.NewProc("GetBestRoute")
)

// mibIPForwardRow mirrors MIB_IPFORWARDROW (all addresses are network-byte-order
// DWORDs). Only the fields we read/pass are named; the rest are padding to the correct
// struct size so GetBestRoute writes within bounds.
type mibIPForwardRow struct {
	dwForwardDest      uint32
	dwForwardMask      uint32
	dwForwardPolicy    uint32
	dwForwardNextHop   uint32
	dwForwardIfIndex   uint32
	dwForwardType      uint32
	dwForwardProto     uint32
	dwForwardAge       uint32
	dwForwardNextHopAS uint32
	dwForwardMetric1   uint32
	dwForwardMetric2   uint32
	dwForwardMetric3   uint32
	dwForwardMetric4   uint32
	dwForwardMetric5   uint32
}

// probeGatewayTarget is the public IPv4 whose route we ask the OS for; it need not be
// reachable — resolving its route just surfaces the default route's next hop.
var probeGatewayTarget = net.IPv4(8, 8, 8, 8).To4()

func defaultGateway() (net.IP, error) {
	dest := hostByteOrderDWORD(probeGatewayTarget)
	var row mibIPForwardRow
	// GetBestRoute(dwDestAddr, dwSourceAddr=0 (let the stack choose), &row).
	ret, _, _ := procGetBestRoute.Call(
		uintptr(dest),
		0,
		uintptr(unsafe.Pointer(&row)),
	)
	if ret != 0 { // non-zero is a Win32 error code (NO_ERROR == 0)
		return nil, ErrNoDefaultGateway
	}
	gw := dwordToIP(row.dwForwardNextHop)
	if gw == nil || gw.IsUnspecified() {
		// A next hop of 0.0.0.0 means the destination is on-link (no gateway) — not a
		// usable upstream router for our purpose.
		return nil, ErrNoDefaultGateway
	}
	return gw, nil
}

// hostByteOrderDWORD packs an IPv4 into the DWORD form GetBestRoute expects: the address
// bytes in network order interpreted as a little-endian DWORD on x86/x64 (i.e. a[0] is
// the low byte), matching how the Win32 API stores IPAddr.
func hostByteOrderDWORD(ip net.IP) uint32 {
	ip4 := ip.To4()
	if ip4 == nil {
		return 0
	}
	return uint32(ip4[0]) | uint32(ip4[1])<<8 | uint32(ip4[2])<<16 | uint32(ip4[3])<<24
}

// dwordToIP is the inverse of hostByteOrderDWORD.
func dwordToIP(v uint32) net.IP {
	return net.IPv4(byte(v), byte(v>>8), byte(v>>16), byte(v>>24)).To4()
}
