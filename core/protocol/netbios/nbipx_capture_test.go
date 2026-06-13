package netbios

import (
	"bytes"
	"testing"
)

// Capture-replay vectors from captures/ipx.pcap: the NB-IPX name-service and NMPI
// packets a Win9x/NWLink client emitted against a ClassicStack server named
// CLASSICSTACK. Each is the IPX *payload* (the bytes after the 30-byte IPX header
// on an Ethernet-II 0x8137 frame); the IPX header itself is covered by the
// core/protocol/ipx capture-replay test. Decode then re-Encode must be
// byte-identical to the wire — the M2/M7 strangler parity proof for the NBIPX
// codec the M7 NBIPX session transport (core/service/netbios/nbipx.go) rides on.

// captureNameServiceFrame2 is ipx.pcap frame #2: an IPX type-20 (NetBIOS
// broadcast) on socket 0x0455 carrying a 50-byte NBIPXNameServicePacket — 32
// zero router-network bytes, NameTypeFlag 0x00, DataStreamType 0x01 (FIND.NAME),
// and the 16-byte NetBIOS name "CLASSICSTACK".
var captureNameServiceFrame2 = []byte{
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // routers 0,1
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // routers 2,3
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // routers 4,5
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // routers 6,7
	0x00,                                                                                           // NameTypeFlag
	0x01,                                                                                           // DataStreamType = FIND.NAME
	0x43, 0x4c, 0x41, 0x53, 0x53, 0x49, 0x43, 0x53, 0x54, 0x41, 0x43, 0x4b, 0x20, 0x20, 0x20, 0x20, // "CLASSICSTACK    "
}

// captureNMPIClaimFrame3 is ipx.pcap frame #3: an NMPI ClaimName (opcode 0xF1) on
// socket 0x0551, NameType 0x01 (machine), claiming CLASSICSTACK with itself as the
// source name. The 52-byte fixed header carries no trailing payload.
var captureNMPIClaimFrame3 = []byte{
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // 32 router bytes
	0xf1,       // Opcode = NAME_CLAIM
	0x01,       // NameType = machine
	0x00, 0x00, // MessageID (LE)
	0x43, 0x4c, 0x41, 0x53, 0x53, 0x49, 0x43, 0x53, 0x54, 0x41, 0x43, 0x4b, 0x20, 0x20, 0x20, 0x20, // RequestedName
	0x43, 0x4c, 0x41, 0x53, 0x53, 0x49, 0x43, 0x53, 0x54, 0x41, 0x43, 0x4b, 0x20, 0x20, 0x20, 0x20, // SourceName
}

// captureNMPIMailslotFrame14 is ipx.pcap frame #14: an NMPI MailslotSend (opcode
// 0xFC) on socket 0x0553 — a browser \MAILSLOT\BROWSE host announcement to the
// group name WORKGROUP<1d>, source CLASSICSTACK, carrying the SMB transaction and
// mailslot path as the trailing payload. It proves the NMPI header/payload split
// round-trips with a real, non-empty payload.
var captureNMPIMailslotFrame14 = []byte{
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // 32 router bytes
	0xfc,       // Opcode = MAILSLOT_SEND
	0x01,       // NameType
	0x00, 0x00, // MessageID
	0x57, 0x4f, 0x52, 0x4b, 0x47, 0x52, 0x4f, 0x55, 0x50, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x1d, // RequestedName "WORKGROUP     <1d>"
	0x43, 0x4c, 0x41, 0x53, 0x53, 0x49, 0x43, 0x53, 0x54, 0x41, 0x43, 0x4b, 0x20, 0x20, 0x20, 0x20, // SourceName "CLASSICSTACK"
	// payload: the embedded SMB transaction + \MAILSLOT\BROWSE host announcement.
	0xff, 0x53, 0x4d, 0x42, 0x25, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x11,
	0x00, 0x00, 0x21, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xe8, 0x03, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x21, 0x00, 0x56, 0x00, 0x03, 0x00, 0x01, 0x00, 0x00, 0x00,
	0x02, 0x00, 0x32, 0x00, 0x5c, 0x4d, 0x41, 0x49, 0x4c, 0x53, 0x4c, 0x4f, 0x54, 0x5c, 0x42, 0x52,
	0x4f, 0x57, 0x53, 0x45, 0x00, 0x01, 0x03, 0xc0, 0xd4, 0x01, 0x00, 0x43, 0x4c, 0x41, 0x53, 0x53,
	0x49, 0x43, 0x53, 0x54, 0x41, 0x43, 0x4b, 0x00, 0x00, 0x00, 0x00, 0x04, 0x00, 0x03, 0x20, 0x40,
	0x00, 0x15, 0x04, 0x55, 0xaa, 0x00,
}

// TestCaptureReplay_NBIPXNameService proves the name-service body from ipx.pcap
// frame #2 decodes to a FIND.NAME for CLASSICSTACK and re-encodes byte-identically.
func TestCaptureReplay_NBIPXNameService(t *testing.T) {
	t.Parallel()
	p, err := DecodeNameService(captureNameServiceFrame2)
	if err != nil {
		t.Fatalf("DecodeNameService: %v", err)
	}
	if p.DataStreamType != NBIPXFindName {
		t.Errorf("DataStreamType = %#x, want FIND.NAME(%#x)", p.DataStreamType, NBIPXFindName)
	}
	if got := p.Name.String(); got != "CLASSICSTACK" {
		t.Errorf("name = %q, want CLASSICSTACK", got)
	}
	if got := EncodeNameService(p); !bytes.Equal(got, captureNameServiceFrame2) {
		t.Fatalf("re-encode not byte-identical:\n got % x\nwant % x", got, captureNameServiceFrame2)
	}
}

// TestCaptureReplay_NBIPXNameClaim proves the NMPI ClaimName from ipx.pcap frame
// #3 decodes to opcode 0xF1 claiming CLASSICSTACK and re-encodes byte-identically.
func TestCaptureReplay_NBIPXNameClaim(t *testing.T) {
	t.Parallel()
	p, err := DecodeNMPIPacket(captureNMPIClaimFrame3)
	if err != nil {
		t.Fatalf("DecodeNMPIPacket: %v", err)
	}
	if p.Opcode != NMPIOpNameClaim {
		t.Errorf("Opcode = %#x, want NAME_CLAIM(%#x)", p.Opcode, NMPIOpNameClaim)
	}
	if p.RequestedName.String() != "CLASSICSTACK" || p.SourceName.String() != "CLASSICSTACK" {
		t.Errorf("names req=%q src=%q", p.RequestedName.String(), p.SourceName.String())
	}
	if len(p.Payload) != 0 {
		t.Errorf("payload = %d bytes, want 0", len(p.Payload))
	}
	if got := EncodeNMPIPacket(p); !bytes.Equal(got, captureNMPIClaimFrame3) {
		t.Fatalf("re-encode not byte-identical:\n got % x\nwant % x", got, captureNMPIClaimFrame3)
	}
}

// TestCaptureReplay_NBIPXMailslot proves the NMPI MailslotSend from ipx.pcap frame
// #14 decodes to opcode 0xFC, splits its browser-announcement payload from the
// 52-byte header, and re-encodes byte-identically (header + payload).
func TestCaptureReplay_NBIPXMailslot(t *testing.T) {
	t.Parallel()
	p, err := DecodeNMPIPacket(captureNMPIMailslotFrame14)
	if err != nil {
		t.Fatalf("DecodeNMPIPacket: %v", err)
	}
	if p.Opcode != NMPIOpMailslotSend {
		t.Errorf("Opcode = %#x, want MAILSLOT_SEND(%#x)", p.Opcode, NMPIOpMailslotSend)
	}
	if p.SourceName.String() != "CLASSICSTACK" {
		t.Errorf("source = %q, want CLASSICSTACK", p.SourceName.String())
	}
	// The payload begins with the embedded SMB ("\xffSMB") and contains the
	// \MAILSLOT\BROWSE path — proof the header/payload split landed correctly.
	if len(p.Payload) < 4 || !bytes.Equal(p.Payload[:4], []byte{0xff, 'S', 'M', 'B'}) {
		t.Fatalf("payload did not start with the embedded SMB: % x", p.Payload[:min(8, len(p.Payload))])
	}
	if !bytes.Contains(p.Payload, []byte("\\MAILSLOT\\BROWSE")) {
		t.Error("payload missing the \\MAILSLOT\\BROWSE browser path")
	}
	if got := EncodeNMPIPacket(p); !bytes.Equal(got, captureNMPIMailslotFrame14) {
		t.Fatalf("re-encode not byte-identical:\n got % x\nwant % x", got, captureNMPIMailslotFrame14)
	}
}
