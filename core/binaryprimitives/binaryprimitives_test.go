package binaryprimitives

import (
	"bytes"
	"testing"
)

func TestBigEndianRoundTrip(t *testing.T) {
	b := make([]byte, 8)

	PutBE16(b, 0x0102)
	if got := BE16(b); got != 0x0102 {
		t.Fatalf("BE16 = %#x, want 0x0102", got)
	}
	if !bytes.Equal(b[:2], []byte{0x01, 0x02}) {
		t.Fatalf("PutBE16 bytes = % x, want 01 02", b[:2])
	}

	PutBE32(b, 0x01020304)
	if got := BE32(b); got != 0x01020304 {
		t.Fatalf("BE32 = %#x, want 0x01020304", got)
	}
	if !bytes.Equal(b[:4], []byte{0x01, 0x02, 0x03, 0x04}) {
		t.Fatalf("PutBE32 bytes = % x", b[:4])
	}

	PutBE64(b, 0x0102030405060708)
	if got := BE64(b); got != 0x0102030405060708 {
		t.Fatalf("BE64 = %#x", got)
	}
	if !bytes.Equal(b, []byte{1, 2, 3, 4, 5, 6, 7, 8}) {
		t.Fatalf("PutBE64 bytes = % x", b)
	}
}

func TestLittleEndianRoundTrip(t *testing.T) {
	b := make([]byte, 8)

	PutLE16(b, 0x0102)
	if got := LE16(b); got != 0x0102 {
		t.Fatalf("LE16 = %#x", got)
	}
	if !bytes.Equal(b[:2], []byte{0x02, 0x01}) {
		t.Fatalf("PutLE16 bytes = % x, want 02 01", b[:2])
	}

	PutLE32(b, 0x01020304)
	if got := LE32(b); got != 0x01020304 {
		t.Fatalf("LE32 = %#x", got)
	}
	if !bytes.Equal(b[:4], []byte{0x04, 0x03, 0x02, 0x01}) {
		t.Fatalf("PutLE32 bytes = % x", b[:4])
	}

	PutLE64(b, 0x0102030405060708)
	if got := LE64(b); got != 0x0102030405060708 {
		t.Fatalf("LE64 = %#x", got)
	}
	if !bytes.Equal(b, []byte{8, 7, 6, 5, 4, 3, 2, 1}) {
		t.Fatalf("PutLE64 bytes = % x", b)
	}
}

func TestAppendWriters(t *testing.T) {
	got := AppendBE16(nil, 0x0102)
	got = AppendBE32(got, 0x03040506)
	got = AppendLE16(got, 0x0708)
	got = AppendLE32(got, 0x090A0B0C)
	want := []byte{
		0x01, 0x02, // BE16
		0x03, 0x04, 0x05, 0x06, // BE32
		0x08, 0x07, // LE16
		0x0C, 0x0B, 0x0A, 0x09, // LE32
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("append chain = % x, want % x", got, want)
	}

	if !bytes.Equal(AppendBE64(nil, 0x0102030405060708), []byte{1, 2, 3, 4, 5, 6, 7, 8}) {
		t.Fatal("AppendBE64 mismatch")
	}
	if !bytes.Equal(AppendLE64(nil, 0x0102030405060708), []byte{8, 7, 6, 5, 4, 3, 2, 1}) {
		t.Fatal("AppendLE64 mismatch")
	}
}
