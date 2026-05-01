package llap

import "testing"

func FuzzFrameFromBytes(f *testing.F) {
	f.Add(make([]byte, 3))
	f.Add(make([]byte, 64))
	f.Fuzz(func(t *testing.T, data []byte) {
		fr, err := FrameFromBytes(data)
		if err != nil {
			return
		}
		_ = fr.Validate()
		_ = fr.Bytes()
	})
}
