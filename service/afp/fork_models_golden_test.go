//go:build afp

package afp

import (
	"bytes"
	"testing"
)

func TestFPOpenForkRes_MarshalGolden(t *testing.T) {
	t.Parallel()
	res := &FPOpenForkRes{
		Bitmap: 0x07FB,
		ForkID: 0x1234,
		Data:   []byte{0xDE, 0xAD, 0xBE, 0xEF},
	}
	got := res.Marshal()
	want := goldenBytes(t, "fpopenforkres_basic.hex", got)
	if !bytes.Equal(got, want) {
		t.Fatalf("Marshal output drift:\n got:  %x\n want: %x", got, want)
	}
}

func TestFPWriteRes_MarshalGolden(t *testing.T) {
	t.Parallel()
	res := &FPWriteRes{LastWritten: 0x12345678}
	got := res.Marshal()
	want := goldenBytes(t, "fpwriteres_basic.hex", got)
	if !bytes.Equal(got, want) {
		t.Fatalf("Marshal output drift:\n got:  %x\n want: %x", got, want)
	}
}

func TestFPByteRangeLockRes_MarshalGolden(t *testing.T) {
	t.Parallel()
	res := &FPByteRangeLockRes{Offset: 0x0BADF00D}
	got := res.Marshal()
	want := goldenBytes(t, "fpbyterangelockres_basic.hex", got)
	if !bytes.Equal(got, want) {
		t.Fatalf("Marshal output drift:\n got:  %x\n want: %x", got, want)
	}
}

func TestFPGetForkParmsRes_MarshalGolden(t *testing.T) {
	t.Parallel()
	res := &FPGetForkParmsRes{
		Bitmap: 0x0600,
		Data:   []byte{0x00, 0x00, 0x10, 0x00, 0x00, 0x00, 0x20, 0x00},
	}
	got := res.Marshal()
	want := goldenBytes(t, "fpgetforkparmsres_basic.hex", got)
	if !bytes.Equal(got, want) {
		t.Fatalf("Marshal output drift:\n got:  %x\n want: %x", got, want)
	}
}
