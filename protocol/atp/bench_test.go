package atp

import "testing"

func BenchmarkHeaderMarshalWire(b *testing.B) {
	h := Header{Control: 0x40, Bitmap: 0xFF, TransID: 0x1234, UserData: 0xDEADBEEF}
	buf := make([]byte, HeaderSize)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = h.MarshalWire(buf)
	}
}

func BenchmarkHeaderUnmarshalWire(b *testing.B) {
	src := []byte{0x40, 0xFF, 0x12, 0x34, 0xDE, 0xAD, 0xBE, 0xEF}
	var h Header
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = h.UnmarshalWire(src)
	}
}

func BenchmarkHeaderRoundTrip(b *testing.B) {
	h := Header{Control: 0x40, Bitmap: 0xFF, TransID: 0x1234, UserData: 0xDEADBEEF}
	buf := make([]byte, HeaderSize)
	var out Header
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = h.MarshalWire(buf)
		_, _ = out.UnmarshalWire(buf)
	}
}
