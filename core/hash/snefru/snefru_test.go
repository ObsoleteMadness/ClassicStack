package snefru

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"
)

// Vectors generated with Elliot Nunn's snefru_hash.py (the reference the ROM
// hash was validated against).
func TestSumVectors(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want string
	}{
		{"64 zero bytes", make([]byte, 64), "825ac7022417010cc9cbd09c05c37141"},
		{"64 x 'A'", bytes.Repeat([]byte{'A'}, 64), "26c6e957cbc3da084b83d75b5c219a20"},
		{"256 counting bytes", counting(256), "662bb71c2157c4128686f4a5455126ee"},
		{"1024 pattern (fold path)", pattern(1024), "fb4fc5343711418eb2d2823e76bc2107"},
	}
	for _, c := range cases {
		got, err := Sum(c.in)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if hex.EncodeToString(got[:]) != c.want {
			t.Fatalf("%s: got %x, want %s", c.name, got, c.want)
		}
	}
}

func counting(n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = byte(i)
	}
	return out
}

func pattern(n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = byte(i*7 + 3)
	}
	return out
}

func TestSumRejectsUnaligned(t *testing.T) {
	if _, err := Sum(make([]byte, 63)); !errors.Is(err, ErrInputSize) {
		t.Fatalf("err = %v, want ErrInputSize", err)
	}
}

// TestAppendTrailerReference pins AppendTrailer to snefru_hash.py's
// append_snefru(b'hello world payload') with the default 64-byte alignment:
// padded to 128 bytes total, last 16 = a5e4dd459d1faeb9ec562f748396b599.
func TestAppendTrailerReference(t *testing.T) {
	out, err := AppendTrailer([]byte("hello world payload"), 64)
	if err != nil {
		t.Fatalf("AppendTrailer: %v", err)
	}
	if len(out) != 128 {
		t.Fatalf("length = %d, want 128", len(out))
	}
	if hex.EncodeToString(out[112:]) != "a5e4dd459d1faeb9ec562f748396b599" {
		t.Fatalf("tail = %x", out[112:])
	}
	if !HasValidTrailer(out) {
		t.Fatal("HasValidTrailer rejected our own trailer")
	}
}

// TestAppendTrailerAlignment checks the ChainBoot constraints: the result is a
// multiple of the block size, at least two blocks long, and the hash occupies
// the last 16 bytes of the final block.
func TestAppendTrailerAlignment(t *testing.T) {
	for _, align := range []int{64, 256, 512} {
		for _, plen := range []int{0, 1, 19, align - 64, align, align*3 + 5} {
			out, err := AppendTrailer(make([]byte, plen), align)
			if err != nil {
				t.Fatalf("align %d len %d: %v", align, plen, err)
			}
			if len(out)%align != 0 {
				t.Fatalf("align %d len %d: result %d not block-aligned", align, plen, len(out))
			}
			if len(out) < 2*align {
				t.Fatalf("align %d len %d: result %d shorter than 2 blocks", align, plen, len(out))
			}
			if !HasValidTrailer(out) {
				t.Fatalf("align %d len %d: invalid trailer", align, plen)
			}
		}
	}
}

func TestHasValidTrailerRejects(t *testing.T) {
	out, err := AppendTrailer([]byte("payload"), 64)
	if err != nil {
		t.Fatal(err)
	}
	out[0] ^= 0xFF // corrupt the body
	if HasValidTrailer(out) {
		t.Fatal("corrupted payload accepted")
	}
	if HasValidTrailer(make([]byte, 64)) {
		t.Fatal("too-short payload accepted")
	}
}
