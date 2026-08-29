//go:build !tinygo

package csnet

import (
	"net"
	"strings"
)

// ParseMAC parses a colon-, dash-, dot-, or bare-hex six-octet hardware address
// into a fixed [6]byte, via net.ParseMAC. Rejects the 8-byte EUI-64 and 20-byte
// InfiniBand forms net.ParseMAC also accepts — ClassicStack only ever wants a
// classic 6-byte Ethernet/LocalTalk station address.
func ParseMAC(s string) ([6]byte, error) {
	hw, err := net.ParseMAC(strings.TrimSpace(s))
	if err != nil || len(hw) != 6 {
		return [6]byte{}, ErrBadMAC
	}
	var mac [6]byte
	copy(mac[:], hw)
	return mac, nil
}
