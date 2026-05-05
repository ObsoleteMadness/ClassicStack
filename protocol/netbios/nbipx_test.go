package netbios

import (
	"bytes"
	"testing"
)

func TestNewNamePadsAndUppercases(t *testing.T) {
	n := NewName("classicstack", NameTypeFileServer)
	want := []byte("CLASSICSTACK   ")
	if !bytes.Equal(n[:NameLength-1], want) {
		t.Fatalf("name bytes: got %q want %q", n[:NameLength-1], want)
	}
	if n.Type() != NameTypeFileServer {
		t.Fatalf("type: got %#x want %#x", n.Type(), NameTypeFileServer)
	}
	if n.String() != "CLASSICSTACK" {
		t.Fatalf("String: got %q", n.String())
	}
}

func TestNewNameTruncates(t *testing.T) {
	n := NewName("ABCDEFGHIJKLMNOPQRSTUV", NameTypeWorkstation)
	if n.String() != "ABCDEFGHIJKLMNO" {
		t.Fatalf("truncated: got %q want first 15 chars", n.String())
	}
}

func TestSessionHeaderRoundTrip(t *testing.T) {
	want := &NBIPXSessionHeader{
		ConnCtrlFlag:   NBIPXConnFlagSYS | NBIPXConnFlagACK,
		DataStreamType: NBIPXSessionInit,
		SourceConnID:   0x1234,
		DestConnID:     0xFFFF, // unassigned during session init
		SendSeq:        1,
		TotalDataLen:   0,
		Offset:         0,
		DataLen:        0,
		ConnCtrlByte:   0,
		Reserved:       0,
	}
	wire := EncodeSessionHeader(want)
	if len(wire) != NBIPXSessionHeaderLen {
		t.Fatalf("header length: got %d want %d", len(wire), NBIPXSessionHeaderLen)
	}
	got, err := DecodeSessionHeader(wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if *got != *want {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", *got, *want)
	}
}

func TestSessionHeaderShort(t *testing.T) {
	if _, err := DecodeSessionHeader([]byte{1, 2, 3}); err != ErrShortNBIPX {
		t.Fatalf("expected ErrShortNBIPX, got %v", err)
	}
}

func TestNameServiceRoundTrip(t *testing.T) {
	want := &NBIPXNameServicePacket{
		Name: NewName("CLASSICSTACK", NameTypeFileServer),
	}
	wire := EncodeNameService(want)
	if len(wire) != NameLength {
		t.Fatalf("wire length: got %d want %d", len(wire), NameLength)
	}
	got, err := DecodeNameService(wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Name != want.Name {
		t.Fatalf("name mismatch: got %q want %q", got.Name.String(), want.Name.String())
	}
}

func TestNameServiceShort(t *testing.T) {
	if _, err := DecodeNameService([]byte{1, 2, 3}); err != ErrShortNBIPX {
		t.Fatalf("expected ErrShortNBIPX, got %v", err)
	}
}
