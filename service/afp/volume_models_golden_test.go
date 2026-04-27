//go:build afp

package afp

import (
	"bytes"
	"testing"
)

// TestFPOpenVolRes_MarshalGolden pins the wire-format output of FPOpenVolRes.Marshal.
func TestFPOpenVolRes_MarshalGolden(t *testing.T) {
	t.Parallel()
	res := &FPOpenVolRes{
		Bitmap: 0x1234,
		Data:   []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE},
	}
	got := res.Marshal()
	want := goldenBytes(t, "fpopenvolres_basic.hex", got)
	if !bytes.Equal(got, want) {
		t.Fatalf("Marshal output drift:\n got:  %x\n want: %x", got, want)
	}
}

// TestFPGetVolParmsRes_MarshalGolden pins the wire-format output of FPGetVolParmsRes.Marshal.
func TestFPGetVolParmsRes_MarshalGolden(t *testing.T) {
	t.Parallel()
	res := &FPGetVolParmsRes{
		Bitmap: 0xBEEF,
		Data:   []byte("volparms-payload"),
	}
	got := res.Marshal()
	want := goldenBytes(t, "fpgetvolparmsres_basic.hex", got)
	if !bytes.Equal(got, want) {
		t.Fatalf("Marshal output drift:\n got:  %x\n want: %x", got, want)
	}
}
