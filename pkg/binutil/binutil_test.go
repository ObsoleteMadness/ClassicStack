package binutil

import (
	"bytes"
	"errors"
	"testing"
)

func TestRoundTripFixedWidth(t *testing.T) {
	t.Parallel()
	b := make([]byte, 15)
	off := 0
	for _, step := range []func() (int, error){
		func() (int, error) { return PutU8(b[off:], 0x12) },
		func() (int, error) { return PutU16(b[off:], 0x3456) },
		func() (int, error) { return PutU32(b[off:], 0x789ABCDE) },
	} {
		n, err := step()
		if err != nil {
			t.Fatalf("put: %v", err)
		}
		off += n
	}
	if off != 7 {
		t.Fatalf("offset = %d, want 7", off)
	}

	off = 0
	u8, n, err := GetU8(b[off:])
	if err != nil || u8 != 0x12 {
		t.Fatalf("GetU8: %x %v", u8, err)
	}
	off += n
	u16, n, err := GetU16(b[off:])
	if err != nil || u16 != 0x3456 {
		t.Fatalf("GetU16: %x %v", u16, err)
	}
	off += n
	u32, _, err := GetU32(b[off:])
	if err != nil || u32 != 0x789ABCDE {
		t.Fatalf("GetU32: %x %v", u32, err)
	}
}

func TestPStringRoundTrip(t *testing.T) {
	t.Parallel()
	b := make([]byte, 32)
	in := []byte("Volume")
	n, err := PutPString(b, in)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1+len(in) {
		t.Fatalf("n = %d, want %d", n, 1+len(in))
	}

	out, n2, err := GetPString(b)
	if err != nil {
		t.Fatal(err)
	}
	if n != n2 {
		t.Fatalf("asymmetric n: put=%d get=%d", n, n2)
	}
	if !bytes.Equal(in, out) {
		t.Fatalf("got %q, want %q", out, in)
	}
}

func TestShortBuffer(t *testing.T) {
	t.Parallel()
	if _, err := PutU32(make([]byte, 3), 0); !errors.Is(err, ErrShortBuffer) {
		t.Fatalf("expected ErrShortBuffer, got %v", err)
	}
	if _, _, err := GetU16(make([]byte, 1)); !errors.Is(err, ErrShortBuffer) {
		t.Fatalf("expected ErrShortBuffer, got %v", err)
	}
	if _, err := PutPString(make([]byte, 2), []byte("xxx")); !errors.Is(err, ErrShortBuffer) {
		t.Fatalf("expected ErrShortBuffer, got %v", err)
	}
}

func TestPStringTooLong(t *testing.T) {
	t.Parallel()
	long := make([]byte, 256)
	if _, err := PutPString(make([]byte, 300), long); !errors.Is(err, ErrMalformed) {
		t.Fatalf("expected ErrMalformed, got %v", err)
	}
}

func BenchmarkPutU32(b *testing.B) {
	buf := make([]byte, 4)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = PutU32(buf, uint32(i))
	}
}
