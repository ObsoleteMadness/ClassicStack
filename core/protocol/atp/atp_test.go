package atp

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

func TestHeaderWireGolden(t *testing.T) {
	t.Parallel()
	h := Header{
		Control:  0x40,
		Bitmap:   0xFF,
		TransID:  0x1234,
		UserData: 0xDEADBEEF,
	}
	want := []byte{0x40, 0xFF, 0x12, 0x34, 0xDE, 0xAD, 0xBE, 0xEF}

	got := h.Encode(nil)
	if !bytes.Equal(got, want) {
		t.Fatalf("Encode = % x, want % x", got, want)
	}

	out, err := Decode(got)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if out != h {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", out, h)
	}
}

func TestEncodeAppends(t *testing.T) {
	t.Parallel()
	prefix := []byte{0xAA, 0xBB}
	h := Header{Control: TREQ, Bitmap: 0x01, TransID: 0x0002, UserData: 0x03}
	got := h.Encode(prefix)
	if !bytes.HasPrefix(got, prefix) {
		t.Fatalf("Encode dropped the prefix: % x", got)
	}
	if len(got) != len(prefix)+HeaderSize {
		t.Fatalf("len = %d, want %d", len(got), len(prefix)+HeaderSize)
	}
}

func TestDecodeShort(t *testing.T) {
	t.Parallel()
	if _, err := Decode(make([]byte, HeaderSize-1)); !errors.Is(err, ErrShort) {
		t.Fatalf("Decode(short) err = %v, want ErrShort", err)
	}
}

func TestControlBits(t *testing.T) {
	t.Parallel()
	h := Header{Control: TRESP | XO | EOM | STS}
	if h.FuncCode() != FuncTResp {
		t.Errorf("FuncCode = %#x, want TResp", h.FuncCode())
	}
	if !h.XO() || !h.EOM() || !h.STS() {
		t.Errorf("flag bit decode failed for control %#x", h.Control)
	}
}

func TestMaxRespForPayload(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		bytes int
		want  int
	}{
		{0, 1},
		{1, 1},
		{MaxATPData, 1},
		{MaxATPData + 1, 2},
		{MaxATPData * MaxResponsePackets, 8},
		{MaxATPData*MaxResponsePackets + 1, 8},
	} {
		if got := MaxRespForPayload(tc.bytes); got != tc.want {
			t.Errorf("MaxRespForPayload(%d) = %d, want %d", tc.bytes, got, tc.want)
		}
	}
}

func TestTRelTimeoutRoundTrip(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		ind  TRelTimeout
		want time.Duration
	}{
		{TRel30s, 30 * time.Second},
		{TRel1m, time.Minute},
		{TRel2m, 2 * time.Minute},
		{TRel4m, 4 * time.Minute},
		{TRel8m, 8 * time.Minute},
	} {
		var h Header
		h.SetTRelTimeout(tc.ind)
		if got := h.GetTRelTimeout(); got != tc.ind {
			t.Errorf("GetTRelTimeout = %d, want %d", got, tc.ind)
		}
		if got := tc.ind.Duration(); got != tc.want {
			t.Errorf("%d.Duration() = %v, want %v", tc.ind, got, tc.want)
		}
	}
}
