//go:build tinygo

package csnet

import "strings"

// ParseMAC parses a colon-, dash-, or bare-hex six-octet hardware address into a
// fixed [6]byte. Hand-rolled (net.ParseMAC is unavailable under TinyGo) —
// accepts the same forms as the desktop build's net.ParseMAC wrapper, except the
// dot-separated Cisco form, which no ClassicStack caller uses.
func ParseMAC(s string) ([6]byte, error) {
	s = strings.TrimSpace(s)
	stripped := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ':' || c == '-' {
			continue
		}
		stripped = append(stripped, c)
	}
	if len(stripped) != 12 {
		return [6]byte{}, ErrBadMAC
	}
	var mac [6]byte
	for i := 0; i < 6; i++ {
		hi, ok1 := hexNibble(stripped[i*2])
		lo, ok2 := hexNibble(stripped[i*2+1])
		if !ok1 || !ok2 {
			return [6]byte{}, ErrBadMAC
		}
		mac[i] = hi<<4 | lo
	}
	return mac, nil
}

func hexNibble(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}
