package netbeui

import (
	"bytes"
	"errors"
	"testing"
)

// goldenAddNameQuery is the NBF body from captures/netbeui.pcap frame #1 (the
// bytes after the 14-byte Ethernet + 3-byte 802.2 LLC headers). It is a 44-byte
// non-session ADD_NAME_QUERY (command 0x01) registering "CLASSICSTACK":
// LENGTH 0x002C, DELIMITER 0xEFFF, RSP correlator 0x0002, zero dest name, and
// source name "CLASSICSTACK   \0".
//
// This is the M2 capture-replay vector: Decode then Encode must be
// byte-identical to the wire.
var goldenAddNameQuery = []byte{
	0x2c, 0x00, // LENGTH = 44 (LE)
	0xff, 0xef, // DELIMITER = 0xEFFF (LE)
	0x01,       // COMMAND = AddNameQuery
	0x00,       // DATA1
	0x00, 0x00, // DATA2
	0x00, 0x00, // XMIT correlator
	0x02, 0x00, // RSP correlator = 0x0002
	// dest name (16 bytes, all zero)
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	// source name "CLASSICSTACK   \0"
	0x43, 0x4c, 0x41, 0x53, 0x53, 0x49, 0x43, 0x53,
	0x54, 0x41, 0x43, 0x4b, 0x20, 0x20, 0x20, 0x00,
}

func TestCaptureReplay_AddNameQuery(t *testing.T) {
	t.Parallel()
	f, err := Decode(goldenAddNameQuery)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if f.Command != CmdAddNameQuery {
		t.Errorf("Command = %#x, want AddNameQuery", f.Command)
	}
	if f.RspCorrelator != 0x0002 {
		t.Errorf("RspCorrelator = %#x, want 0x0002", f.RspCorrelator)
	}
	if got := string(bytes.TrimRight(f.SourceName[:], "\x00 ")); got != "CLASSICSTACK" {
		t.Errorf("SourceName = %q, want CLASSICSTACK", got)
	}
	if len(f.Payload) != 0 {
		t.Errorf("Payload len = %d, want 0", len(f.Payload))
	}

	got, err := f.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(got, goldenAddNameQuery) {
		t.Fatalf("re-encode not byte-identical:\n got % x\nwant % x", got, goldenAddNameQuery)
	}
}

func TestSessionFrameRoundTrip(t *testing.T) {
	t.Parallel()
	f := &Frame{
		Command:      CmdDataOnlyLast, // 0x16, session command
		DestNumber:   0x05,
		SourceNumber: 0x09,
		Payload:      []byte("hello"),
	}
	enc, err := f.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(enc) != SessionHeaderLength+5 {
		t.Fatalf("len = %d, want %d", len(enc), SessionHeaderLength+5)
	}
	// LENGTH is the header length only (X'000E', [IBM SC30-3587] Table 5-25),
	// NOT header+payload. NT 3.51 silently discards frames that get this
	// wrong (netbeui.pcap: every payload-bearing DOL went un-acked while
	// zero-payload frames — accidentally correct — were accepted).
	if enc[0] != byte(SessionHeaderLength) || enc[1] != 0x00 {
		t.Fatalf("LENGTH = %#04x, want %#04x (header length only)",
			uint16(enc[0])|uint16(enc[1])<<8, SessionHeaderLength)
	}
	dec, err := Decode(enc)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if dec.DestNumber != 0x05 || dec.SourceNumber != 0x09 {
		t.Errorf("session numbers = %d/%d, want 5/9", dec.DestNumber, dec.SourceNumber)
	}
	if string(dec.Payload) != "hello" {
		t.Errorf("Payload = %q, want hello", dec.Payload)
	}
}

func TestDecodeErrors(t *testing.T) {
	t.Parallel()
	if _, err := Decode(make([]byte, commonPrefixLen-1)); !errors.Is(err, ErrShortFrame) {
		t.Errorf("short prefix: err = %v, want ErrShortFrame", err)
	}
	// Valid length but wrong delimiter.
	bad := make([]byte, NonSessionHeaderLength)
	bad[2], bad[3] = 0x00, 0x00 // delimiter zero
	if _, err := Decode(bad); !errors.Is(err, ErrBadDelimiter) {
		t.Errorf("bad delimiter: err = %v, want ErrBadDelimiter", err)
	}
}

func TestIsSessionCommand(t *testing.T) {
	t.Parallel()
	if IsSessionCommand(CmdAddNameQuery) {
		t.Error("AddNameQuery (0x01) should be non-session")
	}
	if !IsSessionCommand(CmdDataAck) {
		t.Error("DataAck (0x14) should be session")
	}
}
