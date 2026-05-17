package netbios

import (
	"bytes"
	"errors"
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
	if _, err := DecodeSessionHeader([]byte{1, 2, 3}); !errors.Is(err, ErrShortNBIPX) {
		t.Fatalf("expected ErrShortNBIPX, got %v", err)
	}
}

func TestNameServiceRoundTrip(t *testing.T) {
	want := &NBIPXNameServicePacket{
		NameTypeFlag:   0x40,
		DataStreamType: NBIPXFindName,
		Name:           NewName("CLASSICSTACK", NameTypeFileServer),
	}
	want.Routers[0] = [4]byte{0xCA, 0xFE, 0xF0, 0x0D}
	wire := EncodeNameService(want)
	if len(wire) != NBIPXNameServiceLen {
		t.Fatalf("wire length: got %d want %d", len(wire), NBIPXNameServiceLen)
	}
	got, err := DecodeNameService(wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.NameTypeFlag != want.NameTypeFlag {
		t.Fatalf("NameTypeFlag: got %#x want %#x", got.NameTypeFlag, want.NameTypeFlag)
	}
	if got.DataStreamType != want.DataStreamType {
		t.Fatalf("DataStreamType: got %#x want %#x", got.DataStreamType, want.DataStreamType)
	}
	if got.Name != want.Name {
		t.Fatalf("name mismatch: got %q want %q", got.Name.String(), want.Name.String())
	}
	if got.Routers[0] != want.Routers[0] {
		t.Fatalf("router[0] mismatch: got %v want %v", got.Routers[0], want.Routers[0])
	}
}

func TestNameServiceShort(t *testing.T) {
	if _, err := DecodeNameService([]byte{1, 2, 3}); !errors.Is(err, ErrShortNBIPX) {
		t.Fatalf("expected ErrShortNBIPX, got %v", err)
	}
}

func TestNameServiceDecodeLegacyNameOnly(t *testing.T) {
	legacy := NewName("CLASSICSTACK", NameTypeFileServer)
	wire := make([]byte, NameLength)
	copy(wire, legacy[:])
	got, err := DecodeNameService(wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.DataStreamType != NBIPXFindName {
		t.Fatalf("DataStreamType: got %#x want %#x", got.DataStreamType, NBIPXFindName)
	}
	if got.Name != legacy {
		t.Fatalf("name mismatch: got %q want %q", got.Name.String(), legacy.String())
	}
}

func TestDatagramRoundTrip(t *testing.T) {
	want := &Datagram{
		Destination: NewName("WORKGROUP", NameTypeGroup),
		Source:      NewName("CLASSICSTACK", NameTypeFileServer),
		Payload:     []byte("payload"),
	}
	wire, err := want.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := DecodeDatagram(wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Destination != want.Destination {
		t.Fatalf("destination mismatch: got %q want %q", got.Destination.String(), want.Destination.String())
	}
	if got.Source != want.Source {
		t.Fatalf("source mismatch: got %q want %q", got.Source.String(), want.Source.String())
	}
	if !bytes.Equal(got.Payload, want.Payload) {
		t.Fatalf("payload mismatch: got %q want %q", got.Payload, want.Payload)
	}
}

func TestDatagramShort(t *testing.T) {
	if _, err := DecodeDatagram([]byte{1, 2, 3}); !errors.Is(err, ErrShortDatagram) {
		t.Fatalf("expected ErrShortDatagram, got %v", err)
	}
}

func TestEncodeNMPIPacketLayout(t *testing.T) {
	p := &NMPIPacket{
		Opcode:        NMPIOpMailslotSend,
		NameType:      NMPINameTypeMachine,
		MessageID:     0x1234,
		RequestedName: NewName("WORKGROUP", NameTypeGroup),
		SourceName:    NewName("CLASSICSTACK", NameTypeFileServer),
		Payload:       []byte("payload"),
	}
	wire := EncodeNMPIPacket(p)
	if len(wire) != NMPIFixedHeaderLen+len(p.Payload) {
		t.Fatalf("wire length: got %d want %d", len(wire), NMPIFixedHeaderLen+len(p.Payload))
	}
	if wire[32] != NMPIOpMailslotSend {
		t.Fatalf("opcode: got %#x want %#x", wire[32], NMPIOpMailslotSend)
	}
	if wire[33] != NMPINameTypeMachine {
		t.Fatalf("name type: got %#x want %#x", wire[33], NMPINameTypeMachine)
	}
	if wire[34] != 0x34 || wire[35] != 0x12 {
		t.Fatalf("message id bytes: got [%#x %#x] want [0x34 0x12]", wire[34], wire[35])
	}
}

func TestDecodeNMPIPacketRoundTrip(t *testing.T) {
	want := &NMPIPacket{
		Opcode:        NMPIOpNameQuery,
		NameType:      NMPINameTypeMachine,
		MessageID:     0x0042,
		RequestedName: NewName("CLASSICSTACK", NameTypeFileServer),
		SourceName:    NewName("W98CLIENT", NameTypeWorkstation),
		Payload:       []byte("x"),
	}
	wire := EncodeNMPIPacket(want)
	got, err := DecodeNMPIPacket(wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Opcode != want.Opcode || got.NameType != want.NameType || got.MessageID != want.MessageID {
		t.Fatalf("header mismatch: got opcode=%#x nameType=%#x msg=%#x", got.Opcode, got.NameType, got.MessageID)
	}
	if got.RequestedName != want.RequestedName || got.SourceName != want.SourceName {
		t.Fatalf("name mismatch")
	}
}
