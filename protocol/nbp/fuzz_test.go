package nbp

import "testing"

func FuzzParsePacket(f *testing.F) {
	f.Add(make([]byte, 8))
	// Seed with a minimal valid LkUp tuple (Foo:Bar@*).
	f.Add([]byte{
		(CtrlLkUp << 4) | 1,
		0x42, 0x00, 0x00, 0x00, 0x00, 0x00,
		3, 'F', 'o', 'o',
		3, 'B', 'a', 'r',
		1, '*',
	})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ParsePacket(data)
	})
}
