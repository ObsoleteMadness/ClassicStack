package afp

import (
	"encoding/binary"
	"testing"
)

func TestHandleGetFileDirParms_ObjectNotFoundReturnsStructuredResponse(t *testing.T) {
	root := t.TempDir()
	s := NewService("TestServer", []VolumeConfig{{Name: "Vol", Path: root}}, &LocalFileSystem{}, nil)

	req := &FPGetFileDirParmsReq{
		VolumeID:   1,
		DirID:      CNIDRoot,
		FileBitmap: FileBitmapLongName,
		DirBitmap:  DirBitmapLongName,
		PathType:   2,
		Path:       "missing",
	}

	res, errCode := s.handleGetFileDirParms(req)
	if errCode != ErrObjectNotFound {
		t.Fatalf("errCode=%d, want ErrObjectNotFound (%d)", errCode, ErrObjectNotFound)
	}
	if res == nil {
		t.Fatalf("expected non-nil response on ErrObjectNotFound")
	}

	wire := res.Marshal()
	if len(wire) != 6 {
		t.Fatalf("wire len=%d, want 6 (bitmaps + flag + pad)", len(wire))
	}
	if got := binary.BigEndian.Uint16(wire[0:2]); got != req.FileBitmap {
		t.Fatalf("file bitmap=%#04x, want %#04x", got, req.FileBitmap)
	}
	if got := binary.BigEndian.Uint16(wire[2:4]); got != req.DirBitmap {
		t.Fatalf("dir bitmap=%#04x, want %#04x", got, req.DirBitmap)
	}
	if wire[5] != 0x00 {
		t.Fatalf("reserved pad byte=%#02x, want 0x00", wire[5])
	}
}

func TestHandleGetFileDirParms_ObjectNotFoundDirOnlyRequestUsesDirFlag(t *testing.T) {
	root := t.TempDir()
	s := NewService("TestServer", []VolumeConfig{{Name: "Vol", Path: root}}, &LocalFileSystem{}, nil)

	req := &FPGetFileDirParmsReq{
		VolumeID:   1,
		DirID:      CNIDRoot,
		FileBitmap: 0,
		DirBitmap:  DirBitmapLongName,
		PathType:   2,
		Path:       "missing",
	}

	res, errCode := s.handleGetFileDirParms(req)
	if errCode != ErrObjectNotFound {
		t.Fatalf("errCode=%d, want ErrObjectNotFound (%d)", errCode, ErrObjectNotFound)
	}
	if res == nil {
		t.Fatalf("expected non-nil response on ErrObjectNotFound")
	}

	wire := res.Marshal()
	if len(wire) != 6 {
		t.Fatalf("wire len=%d, want 6", len(wire))
	}
	if wire[4] != 0x80 {
		t.Fatalf("File/DirFlag=%#02x, want 0x80 for dir-only request", wire[4])
	}
}
