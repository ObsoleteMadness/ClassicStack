package afp

// Pascal-string helpers mirroring core/service/afp/handlers.go. Big-endian integer
// codecs come from core/binaryprimitives (core ring: no encoding/binary).

// PutPString appends a Pascal string (1-byte length prefix + bytes, truncated to 255).
func PutPString(dst, s []byte) []byte {
	if len(s) > 255 {
		s = s[:255]
	}
	dst = append(dst, byte(len(s)))
	return append(dst, s...)
}

// PString reads a Pascal string from b at off; returns the bytes and the offset past
// it. ok=false if b is too short for the declared length.
func PString(b []byte, off int) (s []byte, next int, ok bool) {
	if off >= len(b) {
		return nil, off, false
	}
	n := int(b[off])
	off++
	if off+n > len(b) {
		return nil, off, false
	}
	return b[off : off+n], off + n, true
}
