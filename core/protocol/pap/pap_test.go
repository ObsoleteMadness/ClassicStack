package pap

import (
	"errors"
	"testing"
)

func TestHeaderRoundTrip(t *testing.T) {
	t.Parallel()
	h := Header{ConnID: 0x12, Function: FuncData, FuncData: EOFFlag | 0x0034}
	got, err := ParseHeader(h.Encode())
	if err != nil {
		t.Fatalf("ParseHeader: %v", err)
	}
	if got != h {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", got, h)
	}
	if !got.IsEOF() {
		t.Error("IsEOF = false, want true (EOF flag set)")
	}
}

func TestEncodeLayout(t *testing.T) {
	t.Parallel()
	h := Header{ConnID: 0xAB, Function: FuncOpenConn, FuncData: 0xCDEF}
	const want uint32 = 0xAB01CDEF
	if got := h.Encode(); got != want {
		t.Fatalf("Encode = %#08x, want %#08x", got, want)
	}
}

func TestParseBadFunction(t *testing.T) {
	t.Parallel()
	// Function code 0x00 is below the known range.
	h, err := ParseHeader(0x12000000)
	if !errors.Is(err, ErrBadFunction) {
		t.Fatalf("err = %v, want ErrBadFunction", err)
	}
	// Header is still populated so tolerant callers can inspect it.
	if h.ConnID != 0x12 {
		t.Errorf("ConnID = %#x, want 0x12 (populated despite error)", h.ConnID)
	}
}

func TestParseAllKnownFunctions(t *testing.T) {
	t.Parallel()
	for fn := FuncOpenConn; fn <= FuncStatus; fn++ {
		if _, err := ParseHeader(uint32(fn) << 16); err != nil {
			t.Errorf("function %#x: unexpected error %v", fn, err)
		}
	}
}
