package atp

import (
	"bytes"
	"testing"
)

func TestATPHeaderWireGolden(t *testing.T) {
	t.Parallel()
	h := Header{
		Control:  0x40,
		Bitmap:   0xFF,
		TransID:  0x1234,
		UserData: 0xDEADBEEF,
	}
	want := []byte{0x40, 0xFF, 0x12, 0x34, 0xDE, 0xAD, 0xBE, 0xEF}

	buf := make([]byte, h.WireSize())
	n, err := h.MarshalWire(buf)
	if err != nil {
		t.Fatalf("MarshalWire: %v", err)
	}
	if n != HeaderSize {
		t.Fatalf("n = %d, want %d", n, HeaderSize)
	}
	if !bytes.Equal(buf, want) {
		t.Fatalf("MarshalWire = % x, want % x", buf, want)
	}

	var out Header
	if _, err := out.UnmarshalWire(buf); err != nil {
		t.Fatalf("UnmarshalWire: %v", err)
	}
	if out != h {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", out, h)
	}
}

func TestATPHeaderShortBuffer(t *testing.T) {
	t.Parallel()
	h := Header{}
	if _, err := h.MarshalWire(make([]byte, 7)); err == nil {
		t.Fatal("expected ErrShortBuffer on short marshal")
	}
	if _, err := h.UnmarshalWire(make([]byte, 7)); err == nil {
		t.Fatal("expected ErrShortBuffer on short unmarshal")
	}
}
