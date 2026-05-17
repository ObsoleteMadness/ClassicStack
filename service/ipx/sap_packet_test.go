package ipx

import (
	"errors"
	"testing"
)

func TestSAPQueryRoundTrip(t *testing.T) {
	want := &SAPPacket{
		Operation:        SAPGeneralQuery,
		QueryServiceType: SAPServiceTypeNetBIOS,
	}
	wire, err := EncodeSAP(want)
	if err != nil {
		t.Fatalf("EncodeSAP: %v", err)
	}
	if len(wire) != 4 {
		t.Fatalf("wire length: got %d want 4", len(wire))
	}
	got, err := DecodeSAP(wire)
	if err != nil {
		t.Fatalf("DecodeSAP: %v", err)
	}
	if got.Operation != want.Operation || got.QueryServiceType != want.QueryServiceType {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestSAPResponseRoundTrip(t *testing.T) {
	want := &SAPPacket{
		Operation: SAPGeneralResponse,
		Entries: []SAPEntry{
			{
				ServiceType: SAPServiceTypeNetBIOS,
				Name:        "CLASSICSTACK",
				Network:     [4]byte{0xCA, 0xFE, 0xF0, 0x0D},
				Node:        [6]byte{0x02, 0, 0, 0, 0, 0x42},
				Socket:      [2]byte{0x04, 0x55},
				Hops:        1,
			},
			{
				ServiceType: SAPServiceTypeFileSrv,
				Name:        "ANOTHER_SERVER",
				Network:     [4]byte{0xCA, 0xFE, 0xF0, 0x0D},
				Node:        [6]byte{0x02, 0, 0, 0, 0, 0x99},
				Socket:      [2]byte{0x04, 0x51},
				Hops:        2,
			},
		},
	}
	wire, err := EncodeSAP(want)
	if err != nil {
		t.Fatalf("EncodeSAP: %v", err)
	}
	if len(wire) != 2+SAPEntrySize*2 {
		t.Fatalf("wire length: got %d want %d", len(wire), 2+SAPEntrySize*2)
	}
	got, err := DecodeSAP(wire)
	if err != nil {
		t.Fatalf("DecodeSAP: %v", err)
	}
	if got.Operation != want.Operation {
		t.Fatalf("op mismatch: got %d want %d", got.Operation, want.Operation)
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

func TestSAPEncodeRejectsTooManyEntries(t *testing.T) {
	too := &SAPPacket{Operation: SAPGeneralResponse}
	for range SAPMaxEntriesPerPacket + 1 {
		too.Entries = append(too.Entries, SAPEntry{Name: "x", Hops: 1})
	}
	if _, err := EncodeSAP(too); err == nil {
		t.Fatal("expected error for over-sized response")
	}
}

func TestSAPEncodeTruncatesLongName(t *testing.T) {
	// 47-byte limit (one byte reserved for the trailing null).
	long := make([]byte, 100)
	for i := range long {
		long[i] = 'X'
	}
	wire, err := EncodeSAP(&SAPPacket{
		Operation: SAPGeneralResponse,
		Entries: []SAPEntry{{
			ServiceType: SAPServiceTypeFileSrv,
			Name:        string(long),
			Hops:        1,
		}},
	})
	if err != nil {
		t.Fatalf("EncodeSAP: %v", err)
	}
	got, err := DecodeSAP(wire)
	if err != nil {
		t.Fatalf("DecodeSAP: %v", err)
	}
	if len(got.Entries[0].Name) != SAPNameLength-1 {
		t.Fatalf("name length: got %d want %d", len(got.Entries[0].Name), SAPNameLength-1)
	}
}

func TestSAPDecodeShort(t *testing.T) {
	if _, err := DecodeSAP([]byte{0}); !errors.Is(err, ErrShortSAP) {
		t.Fatalf("expected ErrShortSAP, got %v", err)
	}
	if _, err := DecodeSAP([]byte{0x00, 0x01, 0x00}); !errors.Is(err, ErrShortSAP) {
		t.Fatalf("expected ErrShortSAP for truncated query, got %v", err)
	}
}
