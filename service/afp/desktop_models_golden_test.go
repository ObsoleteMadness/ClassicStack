//go:build afp

package afp

import (
	"bytes"
	"testing"
)

func TestFPOpenDTRes_MarshalGolden(t *testing.T) {
	t.Parallel()
	res := &FPOpenDTRes{DTRefNum: 0xCAFE}
	got := res.Marshal()
	want := goldenBytes(t, "fpopendtres_basic.hex", got)
	if !bytes.Equal(got, want) {
		t.Fatalf("Marshal output drift:\n got:  %x\n want: %x", got, want)
	}
}

func TestFPGetAPPLRes_MarshalGolden(t *testing.T) {
	t.Parallel()
	res := &FPGetAPPLRes{
		Bitmap:  0x07FB,
		APPLTag: 0xDEADBEEF,
		Data:    []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
	}
	got := res.Marshal()
	want := goldenBytes(t, "fpgetapplres_basic.hex", got)
	if !bytes.Equal(got, want) {
		t.Fatalf("Marshal output drift:\n got:  %x\n want: %x", got, want)
	}
}

func TestFPGetCommentRes_MarshalGolden(t *testing.T) {
	t.Parallel()
	res := &FPGetCommentRes{Comment: []byte("Hello, comment!")}
	got := res.Marshal()
	want := goldenBytes(t, "fpgetcommentres_basic.hex", got)
	if !bytes.Equal(got, want) {
		t.Fatalf("Marshal output drift:\n got:  %x\n want: %x", got, want)
	}
}
