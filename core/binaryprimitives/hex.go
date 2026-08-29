package binaryprimitives

// --- hex encode/decode (encoding/hex transitively imports reflect; §1) -----

const hexDigits = "0123456789abcdef"

// EncodeHex renders b as lower-case hex, two characters per byte, with no
// separators (e.g. []byte{0xAB, 0x01} -> "ab01"). The form a text store
// serialises a raw byte slice (a salt, a derived hash) through.
func EncodeHex(b []byte) string {
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hexDigits[v>>4]
		out[i*2+1] = hexDigits[v&0x0f]
	}
	return string(out)
}

// DecodeHex parses a hex string (upper, lower, or mixed case) produced by
// EncodeHex (or an equivalent encoder) back into bytes. ok is false for an
// odd-length or non-hex string; the caller decides whether that is malformed
// input worth a specific error.
func DecodeHex(s string) (b []byte, ok bool) {
	if len(s)%2 != 0 {
		return nil, false
	}
	out := make([]byte, len(s)/2)
	for i := 0; i < len(out); i++ {
		hi, ok1 := HexNibble(s[i*2])
		lo, ok2 := HexNibble(s[i*2+1])
		if !ok1 || !ok2 {
			return nil, false
		}
		out[i] = hi<<4 | lo
	}
	return out, true
}

// HexNibble decodes one hex digit character ('0'-'9', 'a'-'f', 'A'-'F') to
// its 4-bit value. ok is false for any other byte.
func HexNibble(c byte) (byte, bool) {
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
