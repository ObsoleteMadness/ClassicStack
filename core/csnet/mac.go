package csnet

import "errors"

// ErrBadMAC reports a MAC string ParseMAC could not parse as a six-octet
// hardware address.
var ErrBadMAC = errors.New("csnet: invalid MAC address")

const hexDigitsUpper = "0123456789ABCDEF"

// FormatMAC renders mac as upper-case colon-separated hex, e.g.
// "00:11:22:AA:BB:CC" — hand-rolled rather than fmt.Sprintf, which transitively
// imports reflect (§1).
func FormatMAC(mac [6]byte) string {
	out := make([]byte, 17)
	for i, b := range mac {
		out[i*3] = hexDigitsUpper[b>>4]
		out[i*3+1] = hexDigitsUpper[b&0x0f]
		if i < 5 {
			out[i*3+2] = ':'
		}
	}
	return string(out)
}
