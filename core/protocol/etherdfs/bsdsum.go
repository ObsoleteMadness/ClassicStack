package etherdfs

// BSDChecksum computes the 16-bit BSD checksum over b, the algorithm the
// EtherDFS protocol uses to optionally guard a frame's payload (everything from
// the version+flags byte onward). For each byte the 16-bit accumulator is
// rotated right by one bit and the byte is added, with the sum kept to 16 bits.
//
// This is the classic BSD `sum` rotate-and-add checksum; it is ported in spirit
// from the reference EtherDFS server's bsd_cksum() (M. Viste / E. Voirin).
func BSDChecksum(b []byte) uint16 {
	var sum uint16
	for _, c := range b {
		// Rotate the 16-bit accumulator right by one bit.
		sum = (sum >> 1) | (sum << 15)
		sum += uint16(c)
	}
	return sum
}
