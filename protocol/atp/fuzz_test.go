package atp

import "testing"

func FuzzATPHeaderUnmarshal(f *testing.F) {
	f.Add(make([]byte, 8))
	f.Add(make([]byte, 32))
	f.Fuzz(func(t *testing.T, data []byte) {
		var h Header
		_, _ = h.UnmarshalWire(data)
	})
}
