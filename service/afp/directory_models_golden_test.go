//go:build afp || all

package afp

import (
	"bytes"
	"testing"
)

func TestFPOpenDirRes_MarshalGolden(t *testing.T) {
	t.Parallel()
	res := &FPOpenDirRes{DirID: 0xCAFEF00D}
	got := res.Marshal()
	want := goldenBytes(t, "fpopendirres_basic.hex", got)
	if !bytes.Equal(got, want) {
		t.Fatalf("Marshal output drift:\n got:  %x\n want: %x", got, want)
	}
}

func TestFPCreateDirRes_MarshalGolden(t *testing.T) {
	t.Parallel()
	res := &FPCreateDirRes{DirID: 0xDEADBEEF}
	got := res.Marshal()
	want := goldenBytes(t, "fpcreatedirres_basic.hex", got)
	if !bytes.Equal(got, want) {
		t.Fatalf("Marshal output drift:\n got:  %x\n want: %x", got, want)
	}
}

func TestFPEnumerateRes_MarshalGolden(t *testing.T) {
	t.Parallel()
	res := &FPEnumerateRes{
		FileBitmap: 0x07FB,
		DirBitmap:  0x0DFF,
		ActCount:   3,
		Data:       []byte("enumerate-payload"),
	}
	got := res.Marshal()
	want := goldenBytes(t, "fpenumerateres_basic.hex", got)
	if !bytes.Equal(got, want) {
		t.Fatalf("Marshal output drift:\n got:  %x\n want: %x", got, want)
	}
}
