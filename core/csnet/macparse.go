//go:build !tinygo

package csnet

import (
	"encoding/hex"
	"net"
	"strings"
)

// ParseMAC parses a colon-, dash-, dot-, or bare-hex six-octet hardware address
// into a fixed [6]byte. Colon/dash/dot forms go through net.ParseMAC (which
// rejects the 8-byte EUI-64 and 20-byte InfiniBand forms it also accepts —
// ClassicStack only ever wants a classic 6-byte Ethernet/LocalTalk station
// address); net.ParseMAC has no bare-hex form (no separators at all), so that
// case is decoded directly here.
func ParseMAC(s string) ([6]byte, error) {
	s = strings.TrimSpace(s)
	if hw, err := net.ParseMAC(s); err == nil && len(hw) == 6 {
		var mac [6]byte
		copy(mac[:], hw)
		return mac, nil
	}
	if len(s) == 12 {
		var mac [6]byte
		if _, err := hex.Decode(mac[:], []byte(s)); err == nil {
			return mac, nil
		}
	}
	return [6]byte{}, ErrBadMAC
}
