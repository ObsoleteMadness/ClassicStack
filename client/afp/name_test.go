package afp

import (
	"bytes"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/encoding"
)

// TestAFPWirePath_TrademarkRoundTrip proves MacRoman ™ (0xAA) survives UTF-8 store
// paths — the StuffIt Deluxe™ Folder failure mode under csmount.
func TestAFPWirePath_TrademarkRoundTrip(t *testing.T) {
	const name = "StuffIt Deluxe™ Folder"
	wire := afpWirePath(name)
	// Wire form: NUL + MacRoman elements.
	if len(wire) < 2 || wire[0] != 0x00 {
		t.Fatalf("wire = %x, want leading NUL", wire)
	}
	mac := wire[1:]
	if !bytes.Contains(mac, []byte{0xAA}) {
		t.Fatalf("wire missing MacRoman ™ (0xAA): %x", mac)
	}
	// Must NOT contain the UTF-8 encoding of ™ (E2 84 A2).
	if bytes.Contains(mac, []byte{0xE2, 0x84, 0xA2}) {
		t.Fatalf("wire still has UTF-8 ™ bytes: %x", mac)
	}
	got := afpDecodeName(mac)
	if got != name {
		t.Fatalf("decode = %q, want %q", got, name)
	}
	// Encoding table agrees.
	if encoding.MacRomanToUTF8([]byte{0xAA}) != "™" {
		t.Fatal("encoding table 0xAA != ™")
	}
}
