//go:build afp || all

package afp

import "testing"

func TestHandleGetFileDirParms_RejectsZeroBitmaps(t *testing.T) {
	root := t.TempDir()
	s := NewService("TestServer", []VolumeConfig{{Name: "Vol", Path: root}}, &LocalFileSystem{}, nil)

	res, errCode := s.handleGetFileDirParms(&FPGetFileDirParmsReq{
		VolumeID:   1,
		DirID:      CNIDRoot,
		FileBitmap: 0,
		DirBitmap:  0,
		PathType:   PathTypeLongNames,
		Path:       "",
	})
	if errCode != ErrBitmapErr {
		t.Fatalf("errCode=%d, want %d", errCode, ErrBitmapErr)
	}
	if res != nil {
		t.Fatalf("expected nil response on error, got %+v", res)
	}
}

func TestHandleGetFileDirParms_RejectsUnsupportedBitmapBits(t *testing.T) {
	root := t.TempDir()
	s := NewService("TestServer", []VolumeConfig{{Name: "Vol", Path: root}}, &LocalFileSystem{}, nil)

	// Bit 14 is not supported by our packer and must not be accepted.
	unsupported := uint16(1 << 14)
	res, errCode := s.handleGetFileDirParms(&FPGetFileDirParmsReq{
		VolumeID:   1,
		DirID:      CNIDRoot,
		FileBitmap: unsupported,
		DirBitmap:  0,
		PathType:   PathTypeLongNames,
		Path:       "",
	})
	if errCode != ErrBitmapErr {
		t.Fatalf("errCode=%d, want %d", errCode, ErrBitmapErr)
	}
	if res != nil {
		t.Fatalf("expected nil response on error, got %+v", res)
	}
}

func TestHandleGetFileDirParms_RejectsInvalidPathTypeWhenPathPresent(t *testing.T) {
	root := t.TempDir()
	s := NewService("TestServer", []VolumeConfig{{Name: "Vol", Path: root}}, &LocalFileSystem{}, nil)

	res, errCode := s.handleGetFileDirParms(&FPGetFileDirParmsReq{
		VolumeID:   1,
		DirID:      CNIDRoot,
		FileBitmap: FileBitmapLongName,
		DirBitmap:  0,
		PathType:   99,
		Path:       "x",
	})
	if errCode != ErrParamErr {
		t.Fatalf("errCode=%d, want %d", errCode, ErrParamErr)
	}
	if res != nil {
		t.Fatalf("expected nil response on error, got %+v", res)
	}
}
