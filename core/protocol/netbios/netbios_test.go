package netbios

import (
	"bytes"
	"errors"
	"testing"
)

// Datagram and NB-IPX round-trips are covered in nbipx_test.go (ported from the
// legacy suite). These cover the RFC 1002 / SMB-Direct session-packet codec.

func TestSessionPacketRoundTrip(t *testing.T) {
	t.Parallel()
	s := &SessionPacket{Type: SessionRequest, Payload: []byte("hello world")}
	wire, err := s.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	// Type(1) + 24-bit length(3) + payload.
	if wire[0] != byte(SessionRequest) {
		t.Errorf("type byte = %#x, want %#x", wire[0], SessionRequest)
	}
	l := int(wire[1])<<16 | int(wire[2])<<8 | int(wire[3])
	if l != len(s.Payload) {
		t.Errorf("length = %d, want %d", l, len(s.Payload))
	}
	got, err := DecodeSessionPacket(wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Type != s.Type || !bytes.Equal(got.Payload, s.Payload) {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestDecodeSessionPacketErrors(t *testing.T) {
	t.Parallel()
	if _, err := DecodeSessionPacket([]byte{0x00, 0x00}); !errors.Is(err, ErrShortSession) {
		t.Errorf("short: err = %v, want ErrShortSession", err)
	}
	// Header claims 100 bytes but only the 4-byte header is present.
	if _, err := DecodeSessionPacket([]byte{0x00, 0x00, 0x00, 0x64}); !errors.Is(err, ErrTruncated) {
		t.Errorf("truncated: err = %v, want ErrTruncated", err)
	}
}
