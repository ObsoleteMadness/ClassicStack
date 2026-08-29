//go:build !tinygo

package csnet

import "net"

// ParseIPv4 parses a dotted-quad string into an IPv4, via net.ParseIP/To4. An
// IPv6 address, or anything else not representable as 4 bytes, is rejected —
// ClassicStack's AppleTalk-side gateways only ever deal in bare IPv4.
func ParseIPv4(s string) (IPv4, error) {
	ip := net.ParseIP(s)
	if ip == nil {
		return IPv4{}, ErrBadIPv4
	}
	v4 := ip.To4()
	if v4 == nil {
		return IPv4{}, ErrBadIPv4
	}
	var out IPv4
	copy(out[:], v4)
	return out, nil
}
