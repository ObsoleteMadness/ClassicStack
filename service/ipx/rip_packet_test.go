package ipx

import (
	"bytes"
	"errors"
	"testing"
)

func TestRIPRoundTrip(t *testing.T) {
	want := &RIPPacket{
		Operation: RIPResponse,
		Entries: []RIPEntry{
			{Network: [4]byte{0xCA, 0xFE, 0xF0, 0x0D}, Hops: 1, Ticks: 1},
			{Network: [4]byte{0xDE, 0xAD, 0xBE, 0xEF}, Hops: 2, Ticks: 4},
		},
	}
	wire, err := EncodeRIP(want)
	if err != nil {
		t.Fatalf("EncodeRIP: %v", err)
	}
	if len(wire) != 2+8*2 {
		t.Fatalf("wire length: got %d want %d", len(wire), 2+8*2)
	}
	got, err := DecodeRIP(wire)
	if err != nil {
		t.Fatalf("DecodeRIP: %v", err)
	}
	if got.Operation != want.Operation {
		t.Fatalf("operation: got %d want %d", got.Operation, want.Operation)
	}
	if len(got.Entries) != len(want.Entries) {
		t.Fatalf("entries: got %d want %d", len(got.Entries), len(want.Entries))
	}
	for i := range got.Entries {
		if got.Entries[i] != want.Entries[i] {
			t.Errorf("entry %d: got %+v want %+v", i, got.Entries[i], want.Entries[i])
		}
	}
}

func TestRIPRequestMinimal(t *testing.T) {
	// A 2-byte body with operation=1 and no entries is a "wildcard"
	// request asking for everything the responder knows.
	wire, err := EncodeRIP(&RIPPacket{Operation: RIPRequest})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(wire, []byte{0, 1}) {
		t.Fatalf("wire: got %x want 0001", wire)
	}
	got, err := DecodeRIP(wire)
	if err != nil {
		t.Fatal(err)
	}
	if got.Operation != RIPRequest || len(got.Entries) != 0 {
		t.Fatalf("decoded: %+v", got)
	}
}

func TestRIPDecodeShort(t *testing.T) {
	if _, err := DecodeRIP([]byte{0}); !errors.Is(err, ErrShortRIP) {
		t.Fatalf("expected ErrShortRIP, got %v", err)
	}
}

func TestRIPDecodeIgnoresTrailingPad(t *testing.T) {
	// IPX packets pad to a 60-byte minimum frame; the decoder should
	// silently drop trailing bytes that don't form a complete entry.
	wire := append([]byte{0, 2}, []byte{0xCA, 0xFE, 0xF0, 0x0D, 0x00, 0x01, 0x00, 0x01}...)
	wire = append(wire, 0xAA, 0xBB) // 2 trailing bytes that would form a partial entry
	got, err := DecodeRIP(wire)
	if err != nil {
		t.Fatalf("DecodeRIP: %v", err)
	}
	if len(got.Entries) != 1 {
		t.Fatalf("entries: got %d want 1", len(got.Entries))
	}
}
