package fs

import (
	"bytes"
	"errors"
	"testing"
)

// TestMacRomanUTF8_TrademarkRoundTrip is the codec-level form of the old
// service/afp TestWriteAFPName_EncodesToMacRoman / enumerate MacRoman cases:
// "tm™" stores as UTF-8 and re-encodes to MacRoman with the trademark byte 0xAA.
func TestMacRomanUTF8_TrademarkRoundTrip(t *testing.T) {
	c := NewMacRomanUTF8FilenameCodec()

	// Wire is MacRoman: 't','m',0xAA  (™ == 0xAA in MacRoman)
	wire := []byte{'t', 'm', 0xAA}
	stored, err := c.Decode(wire, WireMacRoman)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if string(stored) != "tm™" {
		t.Fatalf("stored = %q, want %q", string(stored), "tm™")
	}
	back, err := c.Encode(stored, WireMacRoman)
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}
	if !bytes.Equal(back, wire) {
		t.Fatalf("MacRoman roundtrip = %x, want %x", back, wire)
	}
}

// TestReservedCharTokenRoundTrip is the codec-level form of the old
// TestHostTokenRoundTrip_WhenEnabled: a wire '/' is escaped to the "0x2F"
// token in the store and restored on the way out.
func TestReservedCharTokenRoundTrip(t *testing.T) {
	c := NewMacRomanUTF8FilenameCodec()

	stored, err := c.Decode([]byte("Hello/World"), WireUTF8)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if string(stored) != "Hello0x2FWorld" {
		t.Fatalf("stored = %q, want %q", string(stored), "Hello0x2FWorld")
	}
	back, err := c.Encode(stored, WireUTF8)
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}
	if string(back) != "Hello/World" {
		t.Fatalf("reserved-char roundtrip = %q, want %q", string(back), "Hello/World")
	}
}

// TestSMBWireEncodings exercises the new WireANSI / WireUTF16 paths the SMB
// service threads from its dialect/Unicode flag. Only the identity codec
// advertises them; macroman-utf8 must report ErrWireUnsupported.
func TestSMBWireEncodings(t *testing.T) {
	id := NewIdentityFilenameCodec()

	// UTF-16 (SMB NT): round-trip a name with a non-ASCII rune.
	utf16Wire := mustEncode(t, id, mustDecode(t, id, []byte("café-Ä"), WireUTF8), WireUTF16)
	gotStored, err := id.Decode(utf16Wire, WireUTF16)
	if err != nil {
		t.Fatalf("Decode UTF16 error: %v", err)
	}
	if string(gotStored) != "café-Ä" {
		t.Fatalf("utf16 stored = %q, want %q", string(gotStored), "café-Ä")
	}

	// ANSI (SMB legacy/DOS, CP437): "café" -> 'c','a','f',0x82 ; round-trip.
	ansiStored, err := id.Decode([]byte{'c', 'a', 'f', 0x82}, WireANSI)
	if err != nil {
		t.Fatalf("Decode ANSI error: %v", err)
	}
	if string(ansiStored) != "café" {
		t.Fatalf("ansi stored = %q, want %q", string(ansiStored), "café")
	}
	ansiBack, err := id.Encode(ansiStored, WireANSI)
	if err != nil {
		t.Fatalf("Encode ANSI error: %v", err)
	}
	if !bytes.Equal(ansiBack, []byte{'c', 'a', 'f', 0x82}) {
		t.Fatalf("ansi roundtrip = %x", ansiBack)
	}

	// macroman-utf8 does not advertise UTF-16/ANSI -> fail loudly.
	mc := NewMacRomanUTF8FilenameCodec()
	if _, err := mc.Decode([]byte{0x41, 0x00}, WireUTF16); !errors.Is(err, ErrWireUnsupported) {
		t.Fatalf("macroman-utf8 UTF16 err = %v, want ErrWireUnsupported", err)
	}
}

// TestUTF16TruncatedIsUnrepresentable: an odd-length UTF-16 wire name is a bad
// name (ErrUnrepresentable), not a panic or silent drop.
func TestUTF16TruncatedIsUnrepresentable(t *testing.T) {
	id := NewIdentityFilenameCodec()
	_, err := id.Decode([]byte{0x41, 0x00, 0x42}, WireUTF16)
	if !errors.Is(err, ErrUnrepresentable) {
		t.Fatalf("err = %v, want ErrUnrepresentable", err)
	}
}

// TestWireAdvertisement: a codec must reject charsets it does not list in Wire().
func TestWireAdvertisement(t *testing.T) {
	mc := NewMacRomanUTF8FilenameCodec()
	for _, w := range []WireEncoding{WireANSI, WireUTF16} {
		if _, err := mc.Encode(StoredName("x"), w); !errors.Is(err, ErrWireUnsupported) {
			t.Fatalf("Encode(%v) err = %v, want ErrWireUnsupported", w, err)
		}
	}
}

func mustDecode(t *testing.T, c FilenameCodec, wire []byte, w WireEncoding) StoredName {
	t.Helper()
	s, err := c.Decode(wire, w)
	if err != nil {
		t.Fatalf("Decode(%v) error: %v", w, err)
	}
	return s
}

func mustEncode(t *testing.T, c FilenameCodec, stored StoredName, w WireEncoding) []byte {
	t.Helper()
	b, err := c.Encode(stored, w)
	if err != nil {
		t.Fatalf("Encode(%v) error: %v", w, err)
	}
	return b
}
