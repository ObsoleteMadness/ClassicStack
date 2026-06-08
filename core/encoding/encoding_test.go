package encoding

import (
	"errors"
	"testing"
)

func TestMacRomanRoundTrip(t *testing.T) {
	mac := []byte{0x48, 0x65, 0x6C, 0x6C, 0x6F, 0x20, 0x80}
	utf := MacRomanToUTF8(mac)
	back, err := UTF8ToMacRoman(utf)
	if err != nil {
		t.Fatalf("UTF8ToMacRoman error: %v", err)
	}
	if string(back) != string(mac) {
		t.Fatalf("roundtrip=%v want=%v", back, mac)
	}
}

func TestUTF8ToMacRoman_Unmappable(t *testing.T) {
	_, err := UTF8ToMacRoman("hi 😀")
	if !errors.Is(err, ErrUnmappableRune) {
		t.Fatalf("error=%v want ErrUnmappableRune", err)
	}
}
