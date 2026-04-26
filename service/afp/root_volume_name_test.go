package afp

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func decodeDirLongName(data []byte) (string, error) {
	if len(data) < 3 {
		return "", os.ErrInvalid
	}
	off := int(binary.BigEndian.Uint16(data[0:2]))
	if off >= len(data) {
		return "", os.ErrInvalid
	}
	nameLen := int(data[off])
	if off+1+nameLen > len(data) {
		return "", os.ErrInvalid
	}
	return string(data[off+1 : off+1+nameLen]), nil
}

func decodeDirAccessRights(data []byte) (uint32, error) {
	if len(data) < 4 {
		return 0, os.ErrInvalid
	}
	return binary.BigEndian.Uint32(data[:4]), nil
}

func TestHandleGetDirParms_RootUsesVolumeName(t *testing.T) {
	tmp := t.TempDir()
	backingDir := filepath.Join(tmp, "bar")
	if err := os.Mkdir(backingDir, 0o755); err != nil {
		t.Fatalf("mkdir backing dir: %v", err)
	}

	s := NewService("TestServer", []VolumeConfig{{Name: "foo", Path: backingDir}}, &LocalFileSystem{}, nil)

	res, errCode := s.handleGetDirParms(&FPGetDirParmsReq{
		VolumeID: 1,
		DirID:    CNIDRoot,
		Bitmap:   DirBitmapLongName,
		PathType: 2,
		Path:     "",
	})
	if errCode != NoErr {
		t.Fatalf("handleGetDirParms err = %d, want %d", errCode, NoErr)
	}

	gotName, err := decodeDirLongName(res.Data)
	if err != nil {
		t.Fatalf("decode dir long name: %v", err)
	}
	if gotName != "foo" {
		t.Fatalf("root name = %q, want %q", gotName, "foo")
	}
}

func TestHandleGetDirParms_ReadOnlyVolumeAccessRights(t *testing.T) {
	tmp := t.TempDir()
	backingDir := filepath.Join(tmp, "bar")
	if err := os.Mkdir(backingDir, 0o755); err != nil {
		t.Fatalf("mkdir backing dir: %v", err)
	}

	s := NewService("TestServer", []VolumeConfig{{Name: "foo", Path: backingDir, ReadOnly: true}}, &LocalFileSystem{}, nil)

	res, errCode := s.handleGetDirParms(&FPGetDirParmsReq{
		VolumeID: 1,
		DirID:    CNIDRoot,
		Bitmap:   DirBitmapAccessRights,
		PathType: 2,
		Path:     "",
	})
	if errCode != NoErr {
		t.Fatalf("handleGetDirParms err = %d, want %d", errCode, NoErr)
	}

	rights, err := decodeDirAccessRights(res.Data)
	if err != nil {
		t.Fatalf("decode dir access rights: %v", err)
	}
	if rights != 0x87030303 {
		t.Fatalf("dir access rights = %#08x, want %#08x", rights, uint32(0x87030303))
	}
}

func TestHandleGetDirParms_ReadOnlyVolumeAttributesDoNotUseWriteInhibitBit(t *testing.T) {
	tmp := t.TempDir()
	backingDir := filepath.Join(tmp, "bar")
	if err := os.Mkdir(backingDir, 0o755); err != nil {
		t.Fatalf("mkdir backing dir: %v", err)
	}

	s := NewService("TestServer", []VolumeConfig{{Name: "foo", Path: backingDir, ReadOnly: true}}, &LocalFileSystem{}, nil)

	res, errCode := s.handleGetDirParms(&FPGetDirParmsReq{
		VolumeID: 1,
		DirID:    CNIDRoot,
		Bitmap:   DirBitmapAttributes,
		PathType: 2,
		Path:     "",
	})
	if errCode != NoErr {
		t.Fatalf("handleGetDirParms err = %d, want %d", errCode, NoErr)
	}

	if len(res.Data) < 2 {
		t.Fatalf("dir attributes response too short: %d", len(res.Data))
	}
	attrs := binary.BigEndian.Uint16(res.Data[:2])
	if attrs&FileAttrWriteInhibit != 0 {
		t.Fatalf("dir attributes unexpectedly set WriteInhibit bit: %#04x", attrs)
	}
}
