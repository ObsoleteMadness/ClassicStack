//go:build tinygo

package csnet

import (
	"strconv"
	"strings"
)

// ParseIPv4 parses a dotted-quad string into an IPv4. Hand-rolled (net.ParseIP is
// unavailable under TinyGo) — accepts exactly four decimal octets 0-255, stricter
// than the desktop build's net.ParseIP wrapper (no IPv6, no IPv6-mapped forms).
func ParseIPv4(s string) (IPv4, error) {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return IPv4{}, ErrBadIPv4
	}
	var ip IPv4
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 255 {
			return IPv4{}, ErrBadIPv4
		}
		ip[i] = byte(n)
	}
	return ip, nil
}
