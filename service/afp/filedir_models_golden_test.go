//go:build afp || all

package afp

import (
	"bytes"
	"testing"
)

func TestFPGetFileDirParmsRes_FileMarshalGolden(t *testing.T) {
	t.Parallel()
	res := &FPGetFileDirParmsRes{
		FileBitmap: 0x07FB,
		DirBitmap:  0x0DFF,
		IsFile:     true,
		Data:       []byte{0xAA, 0xBB, 0xCC},
	}
	got := res.Marshal()
	want := goldenBytes(t, "fpgetfiledirparmsres_file.hex", got)
	if !bytes.Equal(got, want) {
		t.Fatalf("Marshal output drift:\n got:  %x\n want: %x", got, want)
	}
}

func TestFPGetFileDirParmsRes_DirMarshalGolden(t *testing.T) {
	t.Parallel()
	res := &FPGetFileDirParmsRes{
		FileBitmap: 0x07FB,
		DirBitmap:  0x0DFF,
		IsFile:     false,
		Data:       []byte{0x11, 0x22, 0x33, 0x44},
	}
	got := res.Marshal()
	want := goldenBytes(t, "fpgetfiledirparmsres_dir.hex", got)
	if !bytes.Equal(got, want) {
		t.Fatalf("Marshal output drift:\n got:  %x\n want: %x", got, want)
	}
}

func TestFPGetDirParmsRes_MarshalGolden(t *testing.T) {
	t.Parallel()
	res := &FPGetDirParmsRes{
		Bitmap: 0x0DFF,
		Data:   []byte{0xDE, 0xAD, 0xBE, 0xEF},
	}
	got := res.Marshal()
	want := goldenBytes(t, "fpgetdirparmsres_basic.hex", got)
	if !bytes.Equal(got, want) {
		t.Fatalf("Marshal output drift:\n got:  %x\n want: %x", got, want)
	}
}

func TestFPGetFileParmsRes_MarshalGolden(t *testing.T) {
	t.Parallel()
	res := &FPGetFileParmsRes{
		Bitmap: 0x07FB,
		Data:   []byte{0xCA, 0xFE, 0xBA, 0xBE},
	}
	got := res.Marshal()
	want := goldenBytes(t, "fpgetfileparmsres_basic.hex", got)
	if !bytes.Equal(got, want) {
		t.Fatalf("Marshal output drift:\n got:  %x\n want: %x", got, want)
	}
}
