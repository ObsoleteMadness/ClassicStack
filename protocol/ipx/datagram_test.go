package ipx

import (
	"bytes"
	"testing"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	want := &Datagram{
		Hops:    1,
		Type:    4,
		DstNet:  [4]byte{0, 0, 0, 1},
		DstNode: [6]byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE},
		DstSock: [2]byte{0x04, 0x53},
		SrcNet:  [4]byte{0, 0, 0, 2},
		SrcNode: [6]byte{0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC},
		SrcSock: [2]byte{0x04, 0x52},
		Payload: []byte("hello"),
	}

	wire, err := want.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := Decode(wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Hops != want.Hops || got.Type != want.Type {
		t.Fatalf("hops/type mismatch: got %v want %v", got, want)
	}
	if got.DstNet != want.DstNet || got.DstNode != want.DstNode || got.DstSock != want.DstSock {
		t.Fatalf("dst mismatch")
	}
	if got.SrcNet != want.SrcNet || got.SrcNode != want.SrcNode || got.SrcSock != want.SrcSock {
		t.Fatalf("src mismatch")
	}
	if !bytes.Equal(got.Payload, want.Payload) {
		t.Fatalf("payload: got %q want %q", got.Payload, want.Payload)
	}
}

func TestDecodeShortPacket(t *testing.T) {
	if _, err := Decode([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected error decoding short packet")
	}
}
