package ipx

import (
	"bytes"
	"errors"
	"testing"
)

// goldenIPXFrame1 is the IPX datagram from captures/ipx.pcap frame #1 (the
// bytes after the 14-byte Ethernet II header; eth.type 0x8137 = IPX). It is a
// 40-byte RIP/SAP-style broadcast: checksum 0xFFFF, length 0x0028, hops 0,
// type 0, dst net 0, dst node FF:FF:FF:FF:FF:FF, dst socket 0x0453.
//
// This is the M2 capture-replay vector: Decode(golden) then Encode must be
// byte-identical to the wire.
var goldenIPXFrame1 = []byte{
	0xff, 0xff, // checksum (disabled)
	0x00, 0x28, // length = 40
	0x00,                   // hops
	0x00,                   // type
	0x00, 0x00, 0x00, 0x00, // dst net
	0xff, 0xff, 0xff, 0xff, 0xff, 0xff, // dst node
	0x04, 0x53, // dst socket (0x0453 = RIP)
	0x00, 0x00, 0x00, 0x00, // src net
	0x00, 0x50, 0x56, 0xc0, 0x00, 0x01, // src node
	0x04, 0x53, // src socket
	0x00, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, // payload (10 bytes)
}

func TestCaptureReplay_Frame1(t *testing.T) {
	t.Parallel()
	d, err := Decode(goldenIPXFrame1)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if d.Length != 40 || d.Type != 0 || d.Hops != 0 {
		t.Errorf("header fields = len %d type %d hops %d", d.Length, d.Type, d.Hops)
	}
	if d.DstSock != [2]byte{0x04, 0x53} || d.SrcSock != [2]byte{0x04, 0x53} {
		t.Errorf("sockets = dst % x src % x", d.DstSock, d.SrcSock)
	}
	if d.DstNode != [6]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff} {
		t.Errorf("dst node = % x, want broadcast", d.DstNode)
	}
	if len(d.Payload) != 10 {
		t.Errorf("payload len = %d, want 10", len(d.Payload))
	}

	got, err := d.Encode(nil)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(got, goldenIPXFrame1) {
		t.Fatalf("re-encode not byte-identical:\n got % x\nwant % x", got, goldenIPXFrame1)
	}
}

func TestEncodeDisabledChecksum(t *testing.T) {
	t.Parallel()
	d := &Datagram{} // zero checksum → must serialise as 0xFFFF
	got, err := d.Encode(nil)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if got[0] != 0xFF || got[1] != 0xFF {
		t.Fatalf("checksum = % x, want ff ff", got[0:2])
	}
	if got[2] != 0x00 || got[3] != HeaderLen {
		t.Fatalf("length = % x, want 00 1e", got[2:4])
	}
}

func TestDecodeErrors(t *testing.T) {
	t.Parallel()
	if _, err := Decode(make([]byte, HeaderLen-1)); !errors.Is(err, ErrShort) {
		t.Errorf("short: err = %v, want ErrShort", err)
	}
	// Length field claims 100 bytes but buffer is only a header.
	b := make([]byte, HeaderLen)
	b[2], b[3] = 0x00, 0x64
	if _, err := Decode(b); !errors.Is(err, ErrBadLength) {
		t.Errorf("truncated: err = %v, want ErrBadLength", err)
	}
}

func TestEncodePreservesPrefix(t *testing.T) {
	t.Parallel()
	prefix := []byte{0xAA, 0xBB}
	d := &Datagram{}
	got, err := d.Encode(prefix)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.HasPrefix(got, prefix) {
		t.Fatalf("Encode dropped prefix: % x", got)
	}
}
