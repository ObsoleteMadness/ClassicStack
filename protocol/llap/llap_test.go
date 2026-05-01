package llap

import (
	"bytes"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		f    Frame
	}{
		{"data short header", Frame{DestinationNode: 1, SourceNode: 2, Type: TypeAppleTalkShortHeader, Payload: []byte{0xDE, 0xAD}}},
		{"data long header", Frame{DestinationNode: 0xFF, SourceNode: 0x42, Type: TypeAppleTalkLongHeader, Payload: bytes.Repeat([]byte{0x55}, 64)}},
		{"control ENQ", Frame{DestinationNode: 0xFE, SourceNode: 0xFE, Type: TypeENQ}},
		{"control CTS", Frame{DestinationNode: 0x10, SourceNode: 0x20, Type: TypeCTS}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := tc.f.Bytes()
			got, err := FrameFromBytes(b)
			if err != nil {
				t.Fatalf("FrameFromBytes: %v", err)
			}
			if got.DestinationNode != tc.f.DestinationNode || got.SourceNode != tc.f.SourceNode || got.Type != tc.f.Type {
				t.Fatalf("header mismatch: got %+v want %+v", got, tc.f)
			}
			if !bytes.Equal(got.Payload, tc.f.Payload) {
				t.Fatalf("payload mismatch: got %x want %x", got.Payload, tc.f.Payload)
			}
		})
	}
}

func TestFrameValidate(t *testing.T) {
	t.Parallel()
	if err := (Frame{Type: TypeENQ, Payload: []byte{0x00}}).Validate(); err == nil {
		t.Fatal("control frame with payload should fail validation")
	}
	if err := (Frame{Type: 0x77}).Validate(); err == nil {
		t.Fatal("unknown frame type should fail validation")
	}
	if err := (Frame{Type: TypeAppleTalkShortHeader, Payload: bytes.Repeat([]byte{0}, MaxDataSize+1)}).Validate(); err == nil {
		t.Fatal("oversize payload should fail validation")
	}
}

func TestFrameFromBytesShort(t *testing.T) {
	t.Parallel()
	if _, err := FrameFromBytes([]byte{0x01, 0x02}); err == nil {
		t.Fatal("expected error for too-short frame")
	}
}
