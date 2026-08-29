package rip

import (
	"bytes"
	"testing"
)

// TestMarshalUnmarshal_RoundTrip pins the wire layout: operation(2) then
// 8-byte entries (network(4) hops(2) ticks(2)), all big-endian.
func TestMarshalUnmarshal_RoundTrip(t *testing.T) {
	p := &Packet{
		Operation: OpResponse,
		Entries: []Entry{
			{Network: [4]byte{0x00, 0x00, 0x00, 0x01}, Hops: 1, Ticks: 2},
			{Network: [4]byte{0xAB, 0xCD, 0xEF, 0x00}, Hops: 3, Ticks: 4},
		},
	}
	wire := p.Marshal(nil)

	want := []byte{
		0x00, 0x02, // OpResponse
		0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x02,
		0xAB, 0xCD, 0xEF, 0x00, 0x00, 0x03, 0x00, 0x04,
	}
	if !bytes.Equal(wire, want) {
		t.Fatalf("Marshal = % x, want % x", wire, want)
	}

	got, err := Unmarshal(wire)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Operation != p.Operation || len(got.Entries) != len(p.Entries) {
		t.Fatalf("Unmarshal = %+v, want %+v", got, p)
	}
	for i := range p.Entries {
		if got.Entries[i] != p.Entries[i] {
			t.Errorf("entry %d = %+v, want %+v", i, got.Entries[i], p.Entries[i])
		}
	}
}

// TestMarshal_AppendsToExistingPrefix checks Marshal's append-style contract:
// it grows dst rather than replacing it.
func TestMarshal_AppendsToExistingPrefix(t *testing.T) {
	prefix := []byte{0xAA, 0xBB}
	p := &Packet{Operation: OpRequest}
	got := p.Marshal(prefix)
	want := []byte{0xAA, 0xBB, 0x00, 0x01}
	if !bytes.Equal(got, want) {
		t.Fatalf("Marshal(prefix) = % x, want % x", got, want)
	}
}

// TestMarshal_NoEntries checks a bare operation with no entries marshals to
// just the 2-byte header.
func TestMarshal_NoEntries(t *testing.T) {
	p := &Packet{Operation: OpRequest}
	got := p.Marshal(nil)
	if !bytes.Equal(got, []byte{0x00, 0x01}) {
		t.Fatalf("Marshal(no entries) = % x, want 00 01", got)
	}
}

// TestUnmarshal_TooShort checks a buffer shorter than the 2-byte operation
// header is rejected.
func TestUnmarshal_TooShort(t *testing.T) {
	if _, err := Unmarshal(nil); err != ErrShort {
		t.Errorf("Unmarshal(nil) error = %v, want ErrShort", err)
	}
	if _, err := Unmarshal([]byte{0x00}); err != ErrShort {
		t.Errorf("Unmarshal(1 byte) error = %v, want ErrShort", err)
	}
}

// TestUnmarshal_TrailingPartialEntryIgnored checks a partial trailing entry
// (shorter than EntryLen — e.g. Ethernet minimum-frame padding after the
// real entries) is dropped rather than causing an out-of-range read or a
// garbage entry.
func TestUnmarshal_TrailingPartialEntryIgnored(t *testing.T) {
	wire := []byte{0x00, 0x02} // OpResponse
	wire = append(wire, []byte{0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x02}...)
	wire = append(wire, []byte{0x00, 0x00, 0x00}...) // 3 trailing pad bytes
	got, err := Unmarshal(wire)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(got.Entries) != 1 {
		t.Fatalf("got %d entries, want 1 (trailing partial entry dropped)", len(got.Entries))
	}
}

// TestUnmarshal_OperationOnly checks a header with zero entries (a wildcard
// request with no entries, or a keepalive-shaped packet) parses cleanly.
func TestUnmarshal_OperationOnly(t *testing.T) {
	got, err := Unmarshal([]byte{0x00, 0x01})
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Operation != OpRequest || len(got.Entries) != 0 {
		t.Fatalf("got %+v, want OpRequest with no entries", got)
	}
}
