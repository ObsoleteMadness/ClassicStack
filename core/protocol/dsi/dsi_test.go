package dsi

import "testing"

func TestHeaderRoundTrip(t *testing.T) {
	h := Header{Flags: Reply, Command: Command, RequestID: 0x1234, ErrorOffset: 0xFFFFFFEC, DataLen: 42, Reserved: 0}
	b := h.Marshal()
	if len(b) != HeaderSize {
		t.Fatalf("Marshal length = %d, want %d", len(b), HeaderSize)
	}
	var got Header
	if !got.Unmarshal(b) {
		t.Fatal("Unmarshal returned false")
	}
	if got != h {
		t.Fatalf("round trip mismatch: got %+v, want %+v", got, h)
	}
}

func TestHeaderUnmarshalShort(t *testing.T) {
	var h Header
	if h.Unmarshal(make([]byte, HeaderSize-1)) {
		t.Fatal("Unmarshal accepted a short buffer")
	}
}

// TestHeaderWireBytes pins the exact byte layout (field order, big-endian) so a
// regression can't silently reorder fields — the value is drawn straight from the
// header diagram in dsi.go's doc comment.
func TestHeaderWireBytes(t *testing.T) {
	h := Header{Flags: Request, Command: OpenSession, RequestID: 1, ErrorOffset: 0, DataLen: 0, Reserved: 0}
	want := []byte{0x00, OpenSession, 0x00, 0x01, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	got := h.Marshal()
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("byte %d = %#x, want %#x (got %v)", i, got[i], want[i], got)
		}
	}
}
