package netbeui

import (
	"bytes"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	want := &Frame{
		Command:            0x08, // datagram
		Data1:              0x00,
		ResponseCorrelator: 0x1234,
		Payload:            []byte("hi netbeui"),
	}
	copy(want.DestinationName[:], "WORKSTATION    \x20")
	copy(want.SourceName[:], "SERVER         \x20")

	wire, err := want.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := Decode(wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Command != want.Command || got.ResponseCorrelator != want.ResponseCorrelator {
		t.Fatalf("header mismatch: got %+v want %+v", got, want)
	}
	if got.DestinationName != want.DestinationName || got.SourceName != want.SourceName {
		t.Fatalf("name mismatch")
	}
	if !bytes.Equal(got.Payload, want.Payload) {
		t.Fatalf("payload: got %q want %q", got.Payload, want.Payload)
	}
}

func TestDecodeRejectsBadDelimiter(t *testing.T) {
	junk := make([]byte, HeaderLength)
	junk[0] = byte(HeaderLength)
	junk[2] = 0xAA
	junk[3] = 0xAA
	if _, err := Decode(junk); err != ErrBadDelimiter {
		t.Fatalf("expected ErrBadDelimiter, got %v", err)
	}
}

func TestDecodeRejectsShortInput(t *testing.T) {
	if _, err := Decode([]byte{1, 2, 3}); err != ErrShortFrame {
		t.Fatalf("expected ErrShortFrame, got %v", err)
	}
}
