//go:build afp

package afp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHandleRename_MovesAppleDoubleSidecar(t *testing.T) {
	root := t.TempDir()
	s := NewService("TestServer", []VolumeConfig{{Name: "Vol", Path: root}}, &LocalFileSystem{}, nil)

	oldName := "Configuration"
	newName := "Configuration Renamed"
	oldPath := filepath.Join(root, oldName)
	newPath := filepath.Join(root, newName)
	oldAD := appleDoublePath(oldPath)
	newAD := appleDoublePath(newPath)

	if err := os.WriteFile(oldPath, []byte("x"), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if err := os.WriteFile(oldAD, []byte("ad"), 0644); err != nil {
		t.Fatalf("seed sidecar: %v", err)
	}

	_, errCode := s.handleRename(&FPRenameReq{
		VolumeID:    1,
		DirID:       CNIDRoot,
		PathType:    2,
		Name:        oldName,
		NewPathType: 2,
		NewName:     newName,
	})
	if errCode != NoErr {
		t.Fatalf("handleRename err = %d, want %d", errCode, NoErr)
	}
	if _, err := os.Stat(newAD); err != nil {
		t.Fatalf("new sidecar missing after rename: %v", err)
	}
	if _, err := os.Stat(oldAD); !os.IsNotExist(err) {
		t.Fatalf("old sidecar should be gone, stat err=%v", err)
	}
}

func TestHandleRename_DecodesMacRomanNewNameAndMovesSidecar(t *testing.T) {
	root := t.TempDir()
	s := NewService("TestServer", []VolumeConfig{{Name: "Vol", Path: root}}, &LocalFileSystem{}, nil)

	oldName := "Seed"
	newAFPName := string([]byte{'M', 'a', 'r', 'a', 't', 'h', 'o', 'n', ' ', 0xB0, ' ', '1', '.', '5'})
	newHostName := "Marathon ∞ 1.5"

	oldPath := filepath.Join(root, oldName)
	newPath := filepath.Join(root, newHostName)
	oldAD := appleDoublePath(oldPath)
	newAD := appleDoublePath(newPath)

	if err := os.WriteFile(oldPath, []byte("x"), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if err := os.WriteFile(oldAD, []byte("ad"), 0644); err != nil {
		t.Fatalf("seed sidecar: %v", err)
	}

	_, errCode := s.handleRename(&FPRenameReq{
		VolumeID:    1,
		DirID:       CNIDRoot,
		PathType:    2,
		Name:        oldName,
		NewPathType: 2,
		NewName:     newAFPName,
	})
	if errCode != NoErr {
		t.Fatalf("handleRename err = %d, want %d", errCode, NoErr)
	}

	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("renamed file missing with decoded MacRoman name: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Marathon � 1.5")); !os.IsNotExist(err) {
		t.Fatalf("unexpected replacement-character filename present, stat err=%v", err)
	}
	if _, err := os.Stat(newAD); err != nil {
		t.Fatalf("new sidecar missing after rename: %v", err)
	}
	if _, err := os.Stat(oldAD); !os.IsNotExist(err) {
		t.Fatalf("old sidecar should be gone, stat err=%v", err)
	}
}

func TestHandleMoveAndRename_MovesAppleDoubleSidecar(t *testing.T) {
	root := t.TempDir()
	s := NewService("TestServer", []VolumeConfig{{Name: "Vol", Path: root}}, &LocalFileSystem{}, nil)

	srcDir := filepath.Join(root, "src")
	dstDir := filepath.Join(root, "dst")
	if err := os.Mkdir(srcDir, 0755); err != nil {
		t.Fatalf("seed src dir: %v", err)
	}
	if err := os.Mkdir(dstDir, 0755); err != nil {
		t.Fatalf("seed dst dir: %v", err)
	}

	srcDID := s.getPathDID(1, srcDir)
	dstDID := s.getPathDID(1, dstDir)

	srcName := "Configuration"
	newName := "Configuration Moved"
	srcPath := filepath.Join(srcDir, srcName)
	dstPath := filepath.Join(dstDir, newName)
	srcAD := appleDoublePath(srcPath)
	dstAD := appleDoublePath(dstPath)

	if err := os.WriteFile(srcPath, []byte("x"), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if err := os.WriteFile(srcAD, []byte("ad"), 0644); err != nil {
		t.Fatalf("seed sidecar: %v", err)
	}

	_, errCode := s.handleMoveAndRename(&FPMoveAndRenameReq{
		VolumeID:    1,
		SrcDirID:    srcDID,
		SrcPathType: 2,
		SrcName:     srcName,
		DstDirID:    dstDID,
		DstPathType: 2,
		DstDirName:  "",
		NewPathType: 2,
		NewName:     newName,
	})
	if errCode != NoErr {
		t.Fatalf("handleMoveAndRename err = %d, want %d", errCode, NoErr)
	}
	if _, err := os.Stat(dstAD); err != nil {
		t.Fatalf("moved sidecar missing: %v", err)
	}
	if _, err := os.Stat(srcAD); !os.IsNotExist(err) {
		t.Fatalf("source sidecar should be gone, stat err=%v", err)
	}
}

func TestHandleMoveAndRename_LegacyMovesAppleDoubleSidecar(t *testing.T) {
	root := t.TempDir()
	s := NewService("TestServer", []VolumeConfig{{Name: "Vol", Path: root, AppleDoubleMode: AppleDoubleModeLegacy}}, &LocalFileSystem{}, nil)

	srcDir := filepath.Join(root, "src")
	dstDir := filepath.Join(root, "dst")
	if err := os.Mkdir(srcDir, 0755); err != nil {
		t.Fatalf("seed src dir: %v", err)
	}
	if err := os.Mkdir(dstDir, 0755); err != nil {
		t.Fatalf("seed dst dir: %v", err)
	}

	srcDID := s.getPathDID(1, srcDir)
	dstDID := s.getPathDID(1, dstDir)

	srcPath := filepath.Join(srcDir, "Configuration")
	dstPath := filepath.Join(dstDir, "Configuration Moved")
	srcAD := filepath.Join(srcDir, ".AppleDouble", "Configuration")
	dstAD := filepath.Join(dstDir, ".AppleDouble", "Configuration Moved")

	if err := os.WriteFile(srcPath, []byte("x"), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(srcAD), 0755); err != nil {
		t.Fatalf("seed legacy sidecar dir: %v", err)
	}
	if err := os.WriteFile(srcAD, []byte("ad"), 0644); err != nil {
		t.Fatalf("seed legacy sidecar: %v", err)
	}

	_, errCode := s.handleMoveAndRename(&FPMoveAndRenameReq{
		VolumeID:    1,
		SrcDirID:    srcDID,
		SrcPathType: 2,
		SrcName:     "Configuration",
		DstDirID:    dstDID,
		DstPathType: 2,
		NewPathType: 2,
		NewName:     "Configuration Moved",
	})
	if errCode != NoErr {
		t.Fatalf("handleMoveAndRename err = %d, want %d", errCode, NoErr)
	}
	if _, err := os.Stat(dstPath); err != nil {
		t.Fatalf("moved file missing: %v", err)
	}
	if _, err := os.Stat(dstAD); err != nil {
		t.Fatalf("moved legacy sidecar missing: %v", err)
	}
	if _, err := os.Stat(srcAD); !os.IsNotExist(err) {
		t.Fatalf("source legacy sidecar should be gone, stat err=%v", err)
	}
}

func TestHandleMoveAndRename_DstPathTypeZeroIgnoresDstDirMarkerPayload(t *testing.T) {
	root := t.TempDir()
	s := NewService("TestServer", []VolumeConfig{{Name: "Vol", Path: root}}, &LocalFileSystem{}, nil)

	srcDir := filepath.Join(root, "src")
	dstDir := filepath.Join(root, "dst")
	if err := os.Mkdir(srcDir, 0755); err != nil {
		t.Fatalf("seed src dir: %v", err)
	}
	if err := os.Mkdir(dstDir, 0755); err != nil {
		t.Fatalf("seed dst dir: %v", err)
	}

	srcDID := s.getPathDID(1, srcDir)
	dstDID := s.getPathDID(1, dstDir)

	if err := os.WriteFile(filepath.Join(srcDir, "MouseSkills.color"), []byte("x"), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	_, errCode := s.handleMoveAndRename(&FPMoveAndRenameReq{
		VolumeID:    1,
		SrcDirID:    srcDID,
		SrcPathType: 2,
		SrcName:     "MouseSkills.color",
		DstDirID:    dstDID,
		DstPathType: 0,
		DstDirName:  "\x11M",
		NewPathType: 2,
	})
	if errCode != NoErr {
		t.Fatalf("handleMoveAndRename err = %d, want %d", errCode, NoErr)
	}

	if _, err := os.Stat(filepath.Join(dstDir, "MouseSkills.color")); err != nil {
		t.Fatalf("moved file missing in destination dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dstDir, "0x11M", "MouseSkills.color")); !os.IsNotExist(err) {
		t.Fatalf("move unexpectedly used marker payload as destination subpath, stat err=%v", err)
	}
}

func TestHandleMoveAndRename_DecodesMacRomanNewName(t *testing.T) {
	root := t.TempDir()
	s := NewService("TestServer", []VolumeConfig{{Name: "Vol", Path: root}}, &LocalFileSystem{}, nil)

	srcDir := filepath.Join(root, "src")
	dstDir := filepath.Join(root, "dst")
	if err := os.Mkdir(srcDir, 0755); err != nil {
		t.Fatalf("seed src dir: %v", err)
	}
	if err := os.Mkdir(dstDir, 0755); err != nil {
		t.Fatalf("seed dst dir: %v", err)
	}

	srcDID := s.getPathDID(1, srcDir)
	dstDID := s.getPathDID(1, dstDir)

	if err := os.WriteFile(filepath.Join(srcDir, "Seed"), []byte("x"), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	_, errCode := s.handleMoveAndRename(&FPMoveAndRenameReq{
		VolumeID:    1,
		SrcDirID:    srcDID,
		SrcPathType: 2,
		SrcName:     "Seed",
		DstDirID:    dstDID,
		DstPathType: 2,
		NewPathType: 2,
		NewName:     string([]byte{'M', 'a', 'r', 'a', 't', 'h', 'o', 'n', ' ', 0xB0, ' ', '1', '.', '5'}),
	})
	if errCode != NoErr {
		t.Fatalf("handleMoveAndRename err = %d, want %d", errCode, NoErr)
	}

	if _, err := os.Stat(filepath.Join(dstDir, "Marathon \u221e 1.5")); err != nil {
		t.Fatalf("moved file missing with decoded MacRoman name: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dstDir, "Marathon \ufffd 1.5")); !os.IsNotExist(err) {
		t.Fatalf("unexpected replacement-character filename present, stat err=%v", err)
	}
}

func TestHandleDelete_DeletesAppleDoubleSidecar(t *testing.T) {
	root := t.TempDir()
	s := NewService("TestServer", []VolumeConfig{{Name: "Vol", Path: root}}, &LocalFileSystem{}, nil)

	name := "Configuration"
	targetPath := filepath.Join(root, name)
	targetAD := appleDoublePath(targetPath)

	if err := os.WriteFile(targetPath, []byte("x"), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if err := os.WriteFile(targetAD, []byte("ad"), 0644); err != nil {
		t.Fatalf("seed sidecar: %v", err)
	}

	_, errCode := s.handleDelete(&FPDeleteReq{
		VolumeID: 1,
		DirID:    CNIDRoot,
		PathType: 2,
		Path:     name,
	})
	if errCode != NoErr {
		t.Fatalf("handleDelete err = %d, want %d", errCode, NoErr)
	}
	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		t.Fatalf("target file should be gone, stat err=%v", err)
	}
	if _, err := os.Stat(targetAD); !os.IsNotExist(err) {
		t.Fatalf("sidecar should be gone, stat err=%v", err)
	}
}
