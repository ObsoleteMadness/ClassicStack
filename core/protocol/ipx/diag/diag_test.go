package diag

import (
	"bytes"
	"errors"
	"testing"
)

func TestRequestRoundTrip(t *testing.T) {
	t.Parallel()
	req := Request{Exclusions: [][6]byte{
		{0x00, 0x50, 0x56, 0xC0, 0x00, 0x01},
		{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF},
	}}
	b, err := req.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if b[0] != 2 {
		t.Fatalf("count byte = %d, want 2", b[0])
	}
	got, err := UnmarshalRequest(b)
	if err != nil {
		t.Fatalf("UnmarshalRequest: %v", err)
	}
	if len(got.Exclusions) != 2 || got.Exclusions[1] != req.Exclusions[1] {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if !got.Excludes(req.Exclusions[0]) {
		t.Fatal("Excludes should report a listed node")
	}
	if got.Excludes([6]byte{1, 2, 3, 4, 5, 6}) {
		t.Fatal("Excludes should not report an absent node")
	}
}

func TestEmptyRequest(t *testing.T) {
	t.Parallel()
	// A zero-length payload is an implicit empty-exclusion ping.
	got, err := UnmarshalRequest(nil)
	if err != nil || len(got.Exclusions) != 0 {
		t.Fatalf("empty request: %+v err=%v", got, err)
	}
	// A single zero byte is the explicit empty form Marshal emits.
	b, err := (Request{}).Marshal()
	if err != nil || !bytes.Equal(b, []byte{0x00}) {
		t.Fatalf("empty Marshal = %v err=%v", b, err)
	}
}

func TestResponseRoundTrip(t *testing.T) {
	t.Parallel()
	resp := SimpleResponse()
	b := resp.Marshal()
	if !bytes.Equal(b, []byte{0x01, CompIPX}) {
		t.Fatalf("SimpleResponse wire = %v", b)
	}
	got, err := UnmarshalResponse(b)
	if err != nil {
		t.Fatalf("UnmarshalResponse: %v", err)
	}
	if len(got.Components) != 1 || got.Components[0].Type != CompIPX {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestUnmarshalResponseShort(t *testing.T) {
	t.Parallel()
	if _, err := UnmarshalResponse(nil); !errors.Is(err, ErrShort) {
		t.Fatalf("want ErrShort for empty, got %v", err)
	}
	// count says 2 but only one type byte follows.
	if _, err := UnmarshalResponse([]byte{0x02, CompIPX}); !errors.Is(err, ErrShort) {
		t.Fatalf("want ErrShort for truncated, got %v", err)
	}
}
