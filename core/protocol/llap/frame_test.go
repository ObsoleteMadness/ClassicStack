package llap

import (
	"errors"
	"testing"
)

// TestControlRoundTrip proves EncodeControl/DecodeControl round-trip an ENQ and an ACK.
func TestControlRoundTrip(t *testing.T) {
	for _, c := range []ControlFrame{
		Enq(0xFE),
		Ack(0x20),
		{Dst: BroadcastNode, Src: 0x10, Type: TypeENQ},
	} {
		got, err := DecodeControl(EncodeControl(c))
		if err != nil {
			t.Fatalf("DecodeControl(%v): %v", c, err)
		}
		if got != c {
			t.Fatalf("round-trip = %v, want %v", got, c)
		}
	}
}

// TestEnqAckShape proves Enq/Ack build self-addressed control frames per spec.
func TestEnqAckShape(t *testing.T) {
	if e := Enq(0x42); e.Dst != 0x42 || e.Src != 0x42 || e.Type != TypeENQ {
		t.Fatalf("Enq = %v, want dst=src=0x42 type=ENQ", e)
	}
	if a := Ack(0x42); a.Dst != 0x42 || a.Src != 0x42 || a.Type != TypeACK {
		t.Fatalf("Ack = %v, want dst=src=0x42 type=ACK", a)
	}
}

// TestDecodeControlShort proves a runt frame is rejected.
func TestDecodeControlShort(t *testing.T) {
	if _, err := DecodeControl([]byte{0x01, 0x02}); !errors.Is(err, ErrShortLLAP) {
		t.Fatalf("DecodeControl(runt) err = %v, want ErrShortLLAP", err)
	}
	if _, _, _, ok := Header([]byte{0x01}); ok {
		t.Fatal("Header(runt) ok=true, want false")
	}
}

// TestIsControl proves the control/data classifier.
func TestIsControl(t *testing.T) {
	for typ, want := range map[uint8]bool{
		TypeENQ:      true,
		TypeACK:      true,
		TypeShortDDP: false,
		TypeLongDDP:  false,
		0x00:         false,
	} {
		if got := IsControl(typ); got != want {
			t.Fatalf("IsControl(%#x) = %v, want %v", typ, got, want)
		}
	}
}
