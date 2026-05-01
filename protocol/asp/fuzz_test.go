//go:build afp || all

package asp

import "testing"

func FuzzParseCommandPacket(f *testing.F) {
	f.Add(uint32(0), []byte{})
	f.Add(uint32(0x01000000), []byte{0x01, 0x02, 0x03})
	f.Fuzz(func(_ *testing.T, ud uint32, payload []byte) {
		_ = ParseCommandPacket(ud, payload)
	})
}

func FuzzParseWritePacket(f *testing.F) {
	f.Add(uint32(0), []byte{})
	f.Add(uint32(0xDEADBEEF), []byte{0xFF, 0x00, 0x42})
	f.Fuzz(func(_ *testing.T, ud uint32, payload []byte) {
		_ = ParseWritePacket(ud, payload)
	})
}

func FuzzParseOpenSessPacket(f *testing.F) {
	f.Add(uint32(0))
	f.Add(uint32(0x01000100))
	f.Fuzz(func(_ *testing.T, ud uint32) {
		_ = ParseOpenSessPacket(ud)
	})
}
