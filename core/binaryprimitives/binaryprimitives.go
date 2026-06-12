package binaryprimitives

// --- big-endian readers -----------------------------------------------------

// BE16 decodes a big-endian uint16 from b[0:2].
func BE16(b []byte) uint16 { return uint16(b[0])<<8 | uint16(b[1]) }

// BE32 decodes a big-endian uint32 from b[0:4].
func BE32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

// BE64 decodes a big-endian uint64 from b[0:8].
func BE64(b []byte) uint64 {
	return uint64(b[0])<<56 | uint64(b[1])<<48 | uint64(b[2])<<40 | uint64(b[3])<<32 |
		uint64(b[4])<<24 | uint64(b[5])<<16 | uint64(b[6])<<8 | uint64(b[7])
}

// --- little-endian readers --------------------------------------------------

// LE16 decodes a little-endian uint16 from b[0:2].
func LE16(b []byte) uint16 { return uint16(b[0]) | uint16(b[1])<<8 }

// LE32 decodes a little-endian uint32 from b[0:4].
func LE32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

// LE64 decodes a little-endian uint64 from b[0:8].
func LE64(b []byte) uint64 {
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
		uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
}

// --- big-endian in-place writers --------------------------------------------

// PutBE16 writes v big-endian into dst[0:2].
func PutBE16(dst []byte, v uint16) {
	dst[0] = byte(v >> 8)
	dst[1] = byte(v)
}

// PutBE32 writes v big-endian into dst[0:4].
func PutBE32(dst []byte, v uint32) {
	dst[0] = byte(v >> 24)
	dst[1] = byte(v >> 16)
	dst[2] = byte(v >> 8)
	dst[3] = byte(v)
}

// PutBE64 writes v big-endian into dst[0:8].
func PutBE64(dst []byte, v uint64) {
	dst[0] = byte(v >> 56)
	dst[1] = byte(v >> 48)
	dst[2] = byte(v >> 40)
	dst[3] = byte(v >> 32)
	dst[4] = byte(v >> 24)
	dst[5] = byte(v >> 16)
	dst[6] = byte(v >> 8)
	dst[7] = byte(v)
}

// --- little-endian in-place writers -----------------------------------------

// PutLE16 writes v little-endian into dst[0:2].
func PutLE16(dst []byte, v uint16) {
	dst[0] = byte(v)
	dst[1] = byte(v >> 8)
}

// PutLE32 writes v little-endian into dst[0:4].
func PutLE32(dst []byte, v uint32) {
	dst[0] = byte(v)
	dst[1] = byte(v >> 8)
	dst[2] = byte(v >> 16)
	dst[3] = byte(v >> 24)
}

// PutLE64 writes v little-endian into dst[0:8].
func PutLE64(dst []byte, v uint64) {
	dst[0] = byte(v)
	dst[1] = byte(v >> 8)
	dst[2] = byte(v >> 16)
	dst[3] = byte(v >> 24)
	dst[4] = byte(v >> 32)
	dst[5] = byte(v >> 40)
	dst[6] = byte(v >> 48)
	dst[7] = byte(v >> 56)
}

// --- big-endian append writers ----------------------------------------------

// AppendBE16 appends v big-endian to dst and returns the grown slice.
func AppendBE16(dst []byte, v uint16) []byte {
	return append(dst, byte(v>>8), byte(v))
}

// AppendBE32 appends v big-endian to dst and returns the grown slice.
func AppendBE32(dst []byte, v uint32) []byte {
	return append(dst, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

// AppendBE64 appends v big-endian to dst and returns the grown slice.
func AppendBE64(dst []byte, v uint64) []byte {
	return append(dst,
		byte(v>>56), byte(v>>48), byte(v>>40), byte(v>>32),
		byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

// --- little-endian append writers -------------------------------------------

// AppendLE16 appends v little-endian to dst and returns the grown slice.
func AppendLE16(dst []byte, v uint16) []byte {
	return append(dst, byte(v), byte(v>>8))
}

// AppendLE32 appends v little-endian to dst and returns the grown slice.
func AppendLE32(dst []byte, v uint32) []byte {
	return append(dst, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

// AppendLE64 appends v little-endian to dst and returns the grown slice.
func AppendLE64(dst []byte, v uint64) []byte {
	return append(dst,
		byte(v), byte(v>>8), byte(v>>16), byte(v>>24),
		byte(v>>32), byte(v>>40), byte(v>>48), byte(v>>56))
}
