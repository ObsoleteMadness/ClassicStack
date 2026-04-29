//go:build afp || all

package afp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStatPathWithAppleDoubleFallback_FindsSidecar(t *testing.T) {
	root := t.TempDir()
	s := NewService("TestServer", []VolumeConfig{{Name: "Vol", Path: root}}, &LocalFileSystem{}, nil)

	baseName := "Netscape Navigator\u2122 2.02"
	sidecar := filepath.Join(root, "._"+baseName)
	if err := os.WriteFile(sidecar, []byte("adouble"), 0644); err != nil {
		t.Fatalf("WriteFile sidecar: %v", err)
	}

	requested := filepath.Join(root, baseName)
	gotPath, info, err := s.statPathWithAppleDoubleFallback(requested)
	if err != nil {
		t.Fatalf("statPathWithAppleDoubleFallback error = %v", err)
	}
	if gotPath != sidecar {
		t.Fatalf("fallback path = %q, want %q", gotPath, sidecar)
	}
	if info.IsDir() {
		t.Fatalf("fallback info should be file")
	}
}

func TestHandleGetFileDirParms_FallsBackToAppleDoubleName(t *testing.T) {
	root := t.TempDir()
	s := NewService("TestServer", []VolumeConfig{{Name: "Vol", Path: root}}, &LocalFileSystem{}, nil)

	baseName := "Netscape Navigator\u2122 2.02"
	sidecar := filepath.Join(root, "._"+baseName)
	if err := os.WriteFile(sidecar, []byte("adouble"), 0644); err != nil {
		t.Fatalf("WriteFile sidecar: %v", err)
	}

	req := &FPGetFileDirParmsReq{
		VolumeID:   1,
		DirID:      CNIDRoot,
		FileBitmap: FileBitmapFileNum,
		DirBitmap:  0,
		PathType:   2,
		Path:       "Netscape Navigator\xaa 2.02",
	}

	res, errCode := s.handleGetFileDirParms(req)
	if errCode != NoErr {
		t.Fatalf("handleGetFileDirParms err = %d, want %d", errCode, NoErr)
	}
	if res == nil {
		t.Fatalf("expected non-nil response")
	}
	if !res.IsFile {
		t.Fatalf("expected file response for AppleDouble sidecar fallback")
	}
}

func TestHandleRemoveComment_FallsBackToAppleDoubleName(t *testing.T) {
	root := t.TempDir()
	s := NewService("TestServer", []VolumeConfig{{Name: "Vol", Path: root}}, &LocalFileSystem{}, nil)

	db := NewDesktopDB(root)
	s.desktopDBs[1] = db
	s.dtRefs[1] = 1

	baseName := "Netscape Navigator\u2122 2.02"
	targetPath := filepath.Join(root, baseName)
	commentBackend, ok := s.metaFor(1).(CommentBackend)
	if !ok {
		t.Fatalf("expected CommentBackend")
	}
	if err := commentBackend.WriteComment(targetPath, []byte("finder comment")); err != nil {
		t.Fatalf("WriteComment: %v", err)
	}

	req := &FPRemoveCommentReq{
		DTRefNum: 1,
		DirID:    CNIDRoot,
		PathType: 2,
		Path:     "Netscape Navigator\xaa 2.02",
	}

	_, errCode := s.handleRemoveComment(req)
	if errCode != NoErr {
		t.Fatalf("handleRemoveComment err = %d, want %d", errCode, NoErr)
	}
	if _, found := commentBackend.ReadComment(targetPath); found {
		t.Fatalf("expected comment to be removed")
	}
}

func TestHandleGetComment_FallsBackToUnicodeAppleDoubleName(t *testing.T) {
	root := t.TempDir()
	s := NewService("TestServer", []VolumeConfig{{Name: "Vol", Path: root}}, &LocalFileSystem{}, nil)
	s.dtRefs[1] = 1

	targetPath := filepath.Join(root, "CD-ROM Toolkit™ Installer")
	commentBackend, ok := s.metaFor(1).(CommentBackend)
	if !ok {
		t.Fatalf("expected CommentBackend")
	}
	if err := commentBackend.WriteComment(targetPath, []byte("finder comment")); err != nil {
		t.Fatalf("WriteComment: %v", err)
	}

	req := &FPGetCommentReq{
		DTRefNum: 1,
		DirID:    CNIDRoot,
		PathType: 2,
		Path:     "CD-ROM Toolkit\xaa Installer",
	}

	res, errCode := s.handleGetComment(req)
	if errCode != NoErr {
		t.Fatalf("handleGetComment err = %d, want %d", errCode, NoErr)
	}
	if string(res.Comment) != "finder comment" {
		t.Fatalf("comment = %q, want %q", string(res.Comment), "finder comment")
	}
}

func TestHandleRemoveComment_FallsBackToUnicodeAppleDoubleName(t *testing.T) {
	root := t.TempDir()
	s := NewService("TestServer", []VolumeConfig{{Name: "Vol", Path: root}}, &LocalFileSystem{}, nil)
	s.dtRefs[1] = 1

	targetPath := filepath.Join(root, "CD-ROM Toolkit™ Installer")
	commentBackend, ok := s.metaFor(1).(CommentBackend)
	if !ok {
		t.Fatalf("expected CommentBackend")
	}
	if err := commentBackend.WriteComment(targetPath, []byte("finder comment")); err != nil {
		t.Fatalf("WriteComment: %v", err)
	}

	req := &FPRemoveCommentReq{
		DTRefNum: 1,
		DirID:    CNIDRoot,
		PathType: 2,
		Path:     "CD-ROM Toolkit\xaa Installer",
	}

	_, errCode := s.handleRemoveComment(req)
	if errCode != NoErr {
		t.Fatalf("handleRemoveComment err = %d, want %d", errCode, NoErr)
	}
	if _, found := commentBackend.ReadComment(targetPath); found {
		t.Fatalf("expected comment to be removed from canonical sidecar")
	}
}

func TestStatPathWithAppleDoubleFallback_LegacyIconCarriageReturnAlias(t *testing.T) {
	root := t.TempDir()
	options := DefaultOptions()
	options.AppleDoubleMode = AppleDoubleModeLegacy
	s := NewService(
		"TestServer",
		[]VolumeConfig{{Name: "Vol", Path: root}},
		&LocalFileSystem{},
		nil,
		options,
	)

	actual := filepath.Join(root, "Icon_")
	if err := os.WriteFile(actual, []byte("icon"), 0644); err != nil {
		t.Fatalf("WriteFile actual: %v", err)
	}

	requested := filepath.Join(root, "Icon0x0D")
	gotPath, info, err := s.statPathWithAppleDoubleFallback(requested)
	if err != nil {
		t.Fatalf("statPathWithAppleDoubleFallback error = %v", err)
	}
	if gotPath != actual {
		t.Fatalf("fallback path = %q, want %q", gotPath, actual)
	}
	if info.IsDir() {
		t.Fatalf("fallback info should be file")
	}
}

func TestHandleGetComment_LegacyIconCarriageReturnAlias(t *testing.T) {
	root := t.TempDir()
	options := DefaultOptions()
	options.AppleDoubleMode = AppleDoubleModeLegacy
	s := NewService(
		"TestServer",
		[]VolumeConfig{{Name: "Vol", Path: root}},
		&LocalFileSystem{},
		nil,
		options,
	)
	s.dtRefs[1] = 1

	actual := filepath.Join(root, "Icon_")
	if err := os.WriteFile(actual, []byte("icon"), 0644); err != nil {
		t.Fatalf("WriteFile actual: %v", err)
	}

	commentBackend, ok := s.metaFor(1).(CommentBackend)
	if !ok {
		t.Fatalf("expected CommentBackend")
	}
	if err := commentBackend.WriteComment(actual, []byte("legacy comment")); err != nil {
		t.Fatalf("WriteComment: %v", err)
	}

	req := &FPGetCommentReq{
		DTRefNum: 1,
		DirID:    CNIDRoot,
		PathType: 2,
		Path:     "Icon0x0D",
	}

	res, errCode := s.handleGetComment(req)
	if errCode != NoErr {
		t.Fatalf("handleGetComment err = %d, want %d", errCode, NoErr)
	}
	if string(res.Comment) != "legacy comment" {
		t.Fatalf("comment = %q, want %q", string(res.Comment), "legacy comment")
	}
}

func TestHandleAddAPPL_LegacyIconCarriageReturnAlias(t *testing.T) {
	root := t.TempDir()
	options := DefaultOptions()
	options.AppleDoubleMode = AppleDoubleModeLegacy
	s := NewService(
		"TestServer",
		[]VolumeConfig{{Name: "Vol", Path: root}},
		&LocalFileSystem{},
		nil,
		options,
	)
	s.desktopDBs[1] = NewDesktopDB(root)
	s.dtRefs[1] = 1

	actual := filepath.Join(root, "Icon_")
	if err := os.WriteFile(actual, []byte("icon"), 0644); err != nil {
		t.Fatalf("WriteFile actual: %v", err)
	}

	var creator [4]byte
	copy(creator[:], "SPNT")
	req := &FPAddAPPLReq{
		DTRefNum: 1,
		DirID:    CNIDRoot,
		Creator:  creator,
		Tag:      123,
		PathType: 2,
		Path:     "Icon0x0D",
	}

	_, errCode := s.handleAddAPPL(req)
	if errCode != NoErr {
		t.Fatalf("handleAddAPPL err = %d, want %d", errCode, NoErr)
	}
}

func TestHandleGetAPPL_LegacyIconCarriageReturnAlias(t *testing.T) {
	root := t.TempDir()
	options := DefaultOptions()
	options.AppleDoubleMode = AppleDoubleModeLegacy
	s := NewService(
		"TestServer",
		[]VolumeConfig{{Name: "Vol", Path: root}},
		&LocalFileSystem{},
		nil,
		options,
	)
	db := NewDesktopDB(root)
	s.desktopDBs[1] = db
	s.dtRefs[1] = 1

	actual := filepath.Join(root, "Icon_")
	if err := os.WriteFile(actual, []byte("icon"), 0644); err != nil {
		t.Fatalf("WriteFile actual: %v", err)
	}

	var creator [4]byte
	copy(creator[:], "SPNT")
	if err := db.AddAPPL(creator, 123, CNIDRoot, "Icon0x0D"); err != nil {
		t.Fatalf("AddAPPL seed: %v", err)
	}

	req := &FPGetAPPLReq{
		DTRefNum:  1,
		Bitmap:    FileBitmapFileNum,
		Creator:   creator,
		APPLIndex: 0,
	}

	res, errCode := s.handleGetAPPL(req)
	if errCode != NoErr {
		t.Fatalf("handleGetAPPL err = %d, want %d", errCode, NoErr)
	}
	if res == nil {
		t.Fatalf("expected non-nil response")
	}
	if len(res.Data) == 0 {
		t.Fatalf("expected file parameter payload")
	}
}
