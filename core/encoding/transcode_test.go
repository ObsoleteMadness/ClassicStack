package encoding

import (
	"errors"
	"testing"
)

func TestUTF16LERoundTrip(t *testing.T) {
	for _, s := range []string{"", "Report", "Ätest", "café", "𝄞clef"} {
		wire := UTF8ToUTF16LE(s)
		back, err := UTF16LEToUTF8(wire)
		if err != nil {
			t.Fatalf("UTF16LEToUTF8(%q) error: %v", s, err)
		}
		if back != s {
			t.Fatalf("utf16 roundtrip = %q want %q", back, s)
		}
	}
}

func TestUTF16LE_BOMStripped(t *testing.T) {
	// BOM (0xFFFE LE) + "Hi"
	wire := append([]byte{0xFF, 0xFE}, UTF8ToUTF16LE("Hi")...)
	got, err := UTF16LEToUTF8(wire)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if got != "Hi" {
		t.Fatalf("got %q want %q", got, "Hi")
	}
}

func TestUTF16LE_OddLength(t *testing.T) {
	_, err := UTF16LEToUTF8([]byte{0x41, 0x00, 0x42})
	if !errors.Is(err, ErrTruncatedUTF16) {
		t.Fatalf("error = %v want ErrTruncatedUTF16", err)
	}
}

func TestANSICP437RoundTrip(t *testing.T) {
	// 0xE1 in CP437 is ß; 0x9B is ¢.
	for _, b := range [][]byte{
		[]byte("README"),
		{0xE1, 't', 'e', 's', 't'},
		{0x9B, 0x80}, // ¢ Ç
	} {
		s, err := ANSIToUTF8(b, CP437)
		if err != nil {
			t.Fatalf("ANSIToUTF8 error: %v", err)
		}
		back, err := UTF8ToANSI(s, CP437)
		if err != nil {
			t.Fatalf("UTF8ToANSI error: %v", err)
		}
		if string(back) != string(b) {
			t.Fatalf("cp437 roundtrip = %v want %v", back, b)
		}
	}
}

func TestANSIUnmappable(t *testing.T) {
	_, err := UTF8ToANSI("emoji 😀", CP437)
	if !errors.Is(err, ErrUnmappableANSI) {
		t.Fatalf("error = %v want ErrUnmappableANSI", err)
	}
}
