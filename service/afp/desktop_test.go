package afp

import (
	"io/fs"
	"path/filepath"
	"testing"
)

type readOnlyDesktopFSTestDouble struct {
	LocalFileSystem
}

func (f *readOnlyDesktopFSTestDouble) CreateDir(path string) error {
	if filepath.Base(path) == ".AppleDesktop" {
		return fs.ErrPermission
	}
	return f.LocalFileSystem.CreateDir(path)
}

func (f *readOnlyDesktopFSTestDouble) IsReadOnly(_ string) (bool, error) {
	return true, nil
}

func TestHandleGetIcon_MissingReturnsItemNotFound(t *testing.T) {
	tmp := t.TempDir()
	fsys := &LocalFileSystem{}
	s := NewAFPService("TestServer", []VolumeConfig{{Name: "Vol1", Path: tmp}}, fsys, nil)

	openRes, errCode := s.handleOpenDT(&FPOpenDTReq{VolID: 1})
	if errCode != NoErr {
		t.Fatalf("handleOpenDT errCode=%d, want %d", errCode, NoErr)
	}

	req := &FPGetIconReq{
		DTRefNum: openRes.DTRefNum,
		Creator:  [4]byte{'T', 'E', 'S', 'T'},
		Type:     [4]byte{'T', 'Y', 'P', 'E'},
		IType:    1,
		Size:     128,
	}
	res, errCode := s.handleGetIcon(req)
	if res == nil {
		t.Fatalf("handleGetIcon res=nil, want non-nil structured response on error")
	}
	if len(res.Data) != 0 {
		t.Fatalf("handleGetIcon on miss returned %d data bytes, want 0", len(res.Data))
	}
	if errCode != ErrItemNotFound {
		t.Fatalf("handleGetIcon errCode=%d, want ErrItemNotFound (%d)", errCode, ErrItemNotFound)
	}
}

func TestHandleGetIcon_SizeZeroPresentProbe(t *testing.T) {
	tmp := t.TempDir()
	fsys := &LocalFileSystem{}
	s := NewAFPService("TestServer", []VolumeConfig{{Name: "Vol1", Path: tmp}}, fsys, nil)

	openRes, errCode := s.handleOpenDT(&FPOpenDTReq{VolID: 1})
	if errCode != NoErr {
		t.Fatalf("handleOpenDT errCode=%d, want %d", errCode, NoErr)
	}

	creator := [4]byte{'T', 'E', 'S', 'T'}
	fileType := [4]byte{'T', 'Y', 'P', 'E'}
	iconData := []byte{1, 2, 3, 4, 5, 6, 7, 8}

	_, errCode = s.handleAddIcon(&FPAddIconReq{
		DTRefNum: openRes.DTRefNum,
		Creator:  creator,
		Type:     fileType,
		IType:    1,
		Tag:      0,
		Size:     uint16(len(iconData)),
		Data:     iconData,
	})
	if errCode != NoErr {
		t.Fatalf("handleAddIcon errCode=%d, want %d", errCode, NoErr)
	}

	res, errCode := s.handleGetIcon(&FPGetIconReq{
		DTRefNum: openRes.DTRefNum,
		Creator:  creator,
		Type:     fileType,
		IType:    1,
		Size:     0,
	})
	if errCode != NoErr {
		t.Fatalf("handleGetIcon(size=0) errCode=%d, want %d", errCode, NoErr)
	}
	if res == nil {
		t.Fatalf("handleGetIcon(size=0) returned nil response")
	}
	if len(res.Data) != 0 {
		t.Fatalf("handleGetIcon(size=0) returned %d bytes, want 0", len(res.Data))
	}
}

func TestHandleOpenDT_ReadOnlyBackendIgnoresAppleDesktopCreateFailure(t *testing.T) {
	tmp := t.TempDir()
	fsys := &readOnlyDesktopFSTestDouble{}
	s := NewAFPService("TestServer", []VolumeConfig{{Name: "Vol1", Path: tmp}}, fsys, nil)

	openRes, errCode := s.handleOpenDT(&FPOpenDTReq{VolID: 1})
	if errCode != NoErr {
		t.Fatalf("handleOpenDT errCode=%d, want %d", errCode, NoErr)
	}
	if openRes.DTRefNum == 0 {
		t.Fatalf("handleOpenDT DTRefNum=%d, want non-zero", openRes.DTRefNum)
	}
}
