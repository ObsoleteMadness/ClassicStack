package binaryprimitives

import "testing"

func TestHexNibble(t *testing.T) {
	cases := []struct {
		in   byte
		want byte
		ok   bool
	}{
		{'0', 0, true}, {'9', 9, true},
		{'a', 10, true}, {'f', 15, true},
		{'A', 10, true}, {'F', 15, true},
		{'g', 0, false}, {'G', 0, false}, {' ', 0, false}, {':', 0, false},
	}
	for _, c := range cases {
		got, ok := HexNibble(c.in)
		if ok != c.ok {
			t.Errorf("HexNibble(%q) ok = %v, want %v", c.in, ok, c.ok)
			continue
		}
		if ok && got != c.want {
			t.Errorf("HexNibble(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestEncodeHex(t *testing.T) {
	cases := []struct {
		in   []byte
		want string
	}{
		{nil, ""},
		{[]byte{0x00}, "00"},
		{[]byte{0xff, 0x01, 0xab}, "ff01ab"},
	}
	for _, c := range cases {
		if got := EncodeHex(c.in); got != c.want {
			t.Errorf("EncodeHex(% x) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDecodeHex_RoundTrip(t *testing.T) {
	for _, in := range [][]byte{nil, {0x00}, {0xff, 0x01, 0xab}, {1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}} {
		enc := EncodeHex(in)
		dec, ok := DecodeHex(enc)
		if !ok {
			t.Fatalf("DecodeHex(%q) not ok", enc)
		}
		if len(in) != len(dec) {
			t.Fatalf("round-trip length %d != %d", len(dec), len(in))
		}
		for i := range in {
			if in[i] != dec[i] {
				t.Fatalf("round-trip mismatch at %d: got %x want %x", i, dec[i], in[i])
			}
		}
	}
}

func TestDecodeHex_CaseInsensitive(t *testing.T) {
	lower, ok := DecodeHex("ffab01")
	if !ok {
		t.Fatal("DecodeHex(lower) not ok")
	}
	upper, ok := DecodeHex("FFAB01")
	if !ok {
		t.Fatal("DecodeHex(upper) not ok")
	}
	mixed, ok := DecodeHex("FfAb01")
	if !ok {
		t.Fatal("DecodeHex(mixed) not ok")
	}
	want := []byte{0xff, 0xab, 0x01}
	for name, got := range map[string][]byte{"lower": lower, "upper": upper, "mixed": mixed} {
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
			t.Errorf("%s: DecodeHex = % x, want % x", name, got, want)
		}
	}
}

func TestDecodeHex_Rejects(t *testing.T) {
	if _, ok := DecodeHex("abc"); ok {
		t.Error("DecodeHex accepted odd-length input")
	}
	if _, ok := DecodeHex("zz"); ok {
		t.Error("DecodeHex accepted non-hex input")
	}
	if dec, ok := DecodeHex(""); !ok || len(dec) != 0 {
		t.Errorf("DecodeHex(\"\") = %v, %v, want empty slice, true", dec, ok)
	}
}
