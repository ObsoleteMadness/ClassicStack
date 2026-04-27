//go:build afp

package dsi

import (
	"bytes"
	"testing"
)

func TestDSIHeaderWireGolden(t *testing.T) {
	t.Parallel()
	h := Header{
		Flags:       0x01,
		Command:     0x02,
		RequestID:   0x1234,
		ErrorOffset: 0xCAFEBABE,
		DataLen:     0x000000F0,
		Reserved:    0xDEADBEEF,
	}
	want := []byte{
		0x01, 0x02, 0x12, 0x34,
		0xCA, 0xFE, 0xBA, 0xBE,
		0x00, 0x00, 0x00, 0xF0,
		0xDE, 0xAD, 0xBE, 0xEF,
	}

	buf := make([]byte, h.WireSize())
	if _, err := h.MarshalWire(buf); err != nil {
		t.Fatalf("MarshalWire: %v", err)
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

func TestDSIHeaderShortBuffer(t *testing.T) {
	t.Parallel()
	h := Header{}
	if _, err := h.MarshalWire(make([]byte, 15)); err == nil {
		t.Fatal("expected error on short marshal")
	}
	if _, err := h.UnmarshalWire(make([]byte, 15)); err == nil {
		t.Fatal("expected error on short unmarshal")
	}
}
