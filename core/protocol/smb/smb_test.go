package smb

import (
	"bytes"
	"testing"
)

// goldenHeaderFrame14 is the 32-byte SMB1 header from captures/ipx.pcap frame
// #14 (an SMB_COM_TRANSACTION / mailslot browse over NetBIOS-over-IPX). All
// fields beyond the command are zero on this request.
//
// This is the M2 capture-replay vector: DecodeHeader then Encode must be
// byte-identical to the wire.
var goldenHeaderFrame14 = []byte{
	0xff, 0x53, 0x4d, 0x42, // protocol identifier "\xffSMB"
	0x25,                   // [4]  command = SMB_COM_TRANSACTION
	0x00, 0x00, 0x00, 0x00, // [5]  status
	0x00,       // [9]  flags
	0x00, 0x00, // [10] flags2
	0x00, 0x00, // [12] PID high
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // [14] security features (8)
	0x00, 0x00, // [22] reserved
	0x00, 0x00, // [24] TID
	0x00, 0x00, // [26] PID low
	0x00, 0x00, // [28] UID
	0x00, 0x00, // [30] MID
}

func TestCaptureReplay_HeaderFrame14(t *testing.T) {
	t.Parallel()
	h, err := DecodeHeader(goldenHeaderFrame14)
	if err != nil {
		t.Fatalf("DecodeHeader: %v", err)
	}
	if h.Command != CommandTransaction {
		t.Errorf("Command = %#x, want Transaction", h.Command)
	}
	if h.IsResponse() {
		t.Error("IsResponse = true, want false (this is a request)")
	}

	got := h.Encode(nil)
	if !bytes.Equal(got, goldenHeaderFrame14) {
		t.Fatalf("re-encode not byte-identical:\n got % x\nwant % x", got, goldenHeaderFrame14)
	}
}

func TestHeaderRoundTrip(t *testing.T) {
	t.Parallel()
	h := Header{
		Command:  CommandNegotiate,
		Status:   0x12345678,
		Flags:    FlagReply,
		Flags2:   Flags2KnowsLongNames | Flags2NTStatus,
		PIDHigh:  0xABCD,
		Security: [8]byte{1, 2, 3, 4, 5, 6, 7, 8},
		TID:      0x1111,
		PIDLow:   0x2222,
		UID:      0x3333,
		MID:      0x4444,
	}
	got, err := DecodeHeader(h.Encode(nil))
	if err != nil {
		t.Fatalf("DecodeHeader: %v", err)
	}
	if got != h {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", got, h)
	}
	if !got.IsResponse() {
		t.Error("IsResponse = false, want true (FlagReply set)")
	}
}

func TestSequenceNumber(t *testing.T) {
	t.Parallel()
	// SequenceNumber sits at header offset 20 = Security[6:8], little-endian.
	h := Header{Security: [8]byte{0, 0, 0, 0, 0, 0, 0x34, 0x12}}
	if got := h.SequenceNumber(); got != 0x1234 {
		t.Fatalf("SequenceNumber = %#x, want 0x1234", got)
	}
}

func TestDecodeHeaderErrors(t *testing.T) {
	t.Parallel()
	if _, err := DecodeHeader(make([]byte, HeaderLen-1)); err != ErrShort {
		t.Errorf("short: err = %v, want ErrShort", err)
	}
	bad := make([]byte, HeaderLen)
	bad[0] = 0xEE // wrong magic
	if _, err := DecodeHeader(bad); err != ErrBadProtocol {
		t.Errorf("bad protocol: err = %v, want ErrBadProtocol", err)
	}
}

func TestEncodePreservesPrefix(t *testing.T) {
	t.Parallel()
	prefix := []byte{0xAA, 0xBB}
	got := Header{Command: CommandEcho}.Encode(prefix)
	if !bytes.HasPrefix(got, prefix) {
		t.Fatalf("Encode dropped prefix: % x", got)
	}
	if len(got) != len(prefix)+HeaderLen {
		t.Fatalf("len = %d, want %d", len(got), len(prefix)+HeaderLen)
	}
}
