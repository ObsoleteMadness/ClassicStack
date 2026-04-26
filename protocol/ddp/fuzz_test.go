package ddp

import "testing"

func FuzzDatagramFromLongHeaderBytes(f *testing.F) {
	// Seed with a minimum-valid DDP long header (13 bytes, no payload).
	f.Add(make([]byte, 13))
	f.Add(make([]byte, 64))
	f.Fuzz(func(t *testing.T, data []byte) {
		// Decoder must never panic on arbitrary input — including
		// truncated headers, oversized lengths, or bad checksums.
		_, _ = DatagramFromLongHeaderBytes(data, false)
		_, _ = DatagramFromLongHeaderBytes(data, true)
	})
}

func FuzzDatagramFromShortHeaderBytes(f *testing.F) {
	f.Add(uint8(0), uint8(0), make([]byte, 5))
	f.Add(uint8(1), uint8(2), make([]byte, 32))
	f.Fuzz(func(t *testing.T, dst, src uint8, data []byte) {
		_, _ = DatagramFromShortHeaderBytes(dst, src, data)
	})
}
