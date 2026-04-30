//go:build afp || all

package afp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pgodw/omnitalk/pkg/encoding"
)

type enumStubInfo struct {
	name  string
	mode  fs.FileMode
	isDir bool
}

func (i *enumStubInfo) Name() string       { return i.name }
func (i *enumStubInfo) Size() int64        { return 0 }
func (i *enumStubInfo) Mode() fs.FileMode  { return i.mode }
func (i *enumStubInfo) ModTime() time.Time { return time.Time{} }
func (i *enumStubInfo) IsDir() bool        { return i.isDir }
func (i *enumStubInfo) Sys() any           { return nil }

type enumStubDirEntry struct{ info fs.FileInfo }

func (d enumStubDirEntry) Name() string               { return d.info.Name() }
func (d enumStubDirEntry) IsDir() bool                { return d.info.IsDir() }
func (d enumStubDirEntry) Type() fs.FileMode          { return d.info.Mode().Type() }
func (d enumStubDirEntry) Info() (fs.FileInfo, error) { return d.info, nil }

type childCountSpyFS struct {
	root            string
	childCountCalls int
	readDirCalls    []string
}

type rangeSpyFS struct {
	root           string
	readDirCalls   []string
	rangeCalls     []string
	lastStartIndex uint16
	lastReqCount   uint16
}

type rangeEmptySpyFS struct {
	root string
}

func (s *childCountSpyFS) ReadDir(path string) ([]fs.DirEntry, error) {
	s.readDirCalls = append(s.readDirCalls, filepath.Clean(path))
	if filepath.Clean(path) == filepath.Clean(s.root) {
		return []fs.DirEntry{
			enumStubDirEntry{info: &enumStubInfo{name: "Apps", mode: fs.ModeDir | 0o555, isDir: true}},
			enumStubDirEntry{info: &enumStubInfo{name: "Games", mode: fs.ModeDir | 0o555, isDir: true}},
		}, nil
	}
	return nil, fs.ErrPermission
}

func (s *childCountSpyFS) Stat(path string) (fs.FileInfo, error) {
	clean := filepath.Clean(path)
	if clean == filepath.Clean(s.root) || clean == filepath.Join(s.root, "Apps") || clean == filepath.Join(s.root, "Games") {
		return &enumStubInfo{name: filepath.Base(clean), mode: fs.ModeDir | 0o555, isDir: true}, nil
	}
	return nil, fs.ErrNotExist
}

func (s *childCountSpyFS) DiskUsage(path string) (uint64, uint64, error) { return 0, 0, nil }
func (s *childCountSpyFS) CreateDir(path string) error                   { return fs.ErrPermission }
func (s *childCountSpyFS) CreateFile(path string) (File, error)          { return nil, fs.ErrPermission }
func (s *childCountSpyFS) OpenFile(path string, flag int) (File, error)  { return nil, fs.ErrPermission }
func (s *childCountSpyFS) Remove(path string) error                      { return fs.ErrPermission }
func (s *childCountSpyFS) Rename(oldpath, newpath string) error          { return fs.ErrPermission }
func (s *childCountSpyFS) Capabilities() FileSystemCapabilities {
	return FileSystemCapabilities{ChildCount: true}
}
func (s *childCountSpyFS) CatSearch(volumeRoot string, query string, reqMatches int32, cursor [16]byte) ([]string, [16]byte, int32) {
	return nil, cursor, ErrCallNotSupported
}
func (s *childCountSpyFS) ReadDirRange(path string, startIndex uint16, reqCount uint16) ([]fs.DirEntry, uint16, error) {
	return nil, 0, newNotSupported("ReadDirRange")
}
func (s *childCountSpyFS) DirAttributes(path string) (uint16, error)   { return 0, nil }
func (s *childCountSpyFS) IsReadOnly(path string) (bool, error)        { return false, nil }
func (s *childCountSpyFS) SupportsCatSearch(path string) (bool, error) { return false, nil }

func (s *rangeSpyFS) ReadDir(path string) ([]fs.DirEntry, error) {
	s.readDirCalls = append(s.readDirCalls, filepath.Clean(path))
	return nil, fs.ErrPermission
}

func (s *rangeSpyFS) Stat(path string) (fs.FileInfo, error) {
	clean := filepath.Clean(path)
	if clean == filepath.Clean(s.root) {
		return &enumStubInfo{name: filepath.Base(clean), mode: fs.ModeDir | 0o555, isDir: true}, nil
	}
	if clean == filepath.Join(s.root, "Gamma") || clean == filepath.Join(s.root, "Delta") {
		return &enumStubInfo{name: filepath.Base(clean), mode: fs.ModeDir | 0o555, isDir: true}, nil
	}
	return nil, fs.ErrNotExist
}

func (s *rangeSpyFS) DiskUsage(path string) (uint64, uint64, error) { return 0, 0, nil }
func (s *rangeSpyFS) CreateDir(path string) error                   { return fs.ErrPermission }
func (s *rangeSpyFS) CreateFile(path string) (File, error)          { return nil, fs.ErrPermission }
func (s *rangeSpyFS) OpenFile(path string, flag int) (File, error)  { return nil, fs.ErrPermission }
func (s *rangeSpyFS) Remove(path string) error                      { return fs.ErrPermission }
func (s *rangeSpyFS) Rename(oldpath, newpath string) error          { return fs.ErrPermission }
func (s *rangeSpyFS) Capabilities() FileSystemCapabilities {
	return FileSystemCapabilities{ReadDirRange: true}
}
func (s *rangeSpyFS) CatSearch(volumeRoot string, query string, reqMatches int32, cursor [16]byte) ([]string, [16]byte, int32) {
	return nil, cursor, ErrCallNotSupported
}
func (s *rangeSpyFS) ChildCount(path string) (uint16, error) { return 0, newNotSupported("ChildCount") }
func (s *rangeSpyFS) DirAttributes(path string) (uint16, error) {
	return 0, nil
}
func (s *rangeSpyFS) IsReadOnly(path string) (bool, error)        { return false, nil }
func (s *rangeSpyFS) SupportsCatSearch(path string) (bool, error) { return false, nil }

func (s *rangeSpyFS) ReadDirRange(path string, startIndex uint16, reqCount uint16) ([]fs.DirEntry, uint16, error) {
	s.rangeCalls = append(s.rangeCalls, filepath.Clean(path))
	s.lastStartIndex = startIndex
	s.lastReqCount = reqCount
	return []fs.DirEntry{
		enumStubDirEntry{info: &enumStubInfo{name: "Gamma", mode: fs.ModeDir | 0o555, isDir: true}},
		enumStubDirEntry{info: &enumStubInfo{name: "Delta", mode: fs.ModeDir | 0o555, isDir: true}},
	}, 7, nil
}

func (s *rangeEmptySpyFS) ReadDir(path string) ([]fs.DirEntry, error) {
	return nil, fs.ErrPermission
}

func (s *rangeEmptySpyFS) Stat(path string) (fs.FileInfo, error) {
	if filepath.Clean(path) == filepath.Clean(s.root) {
		return &enumStubInfo{name: filepath.Base(path), mode: fs.ModeDir | 0o555, isDir: true}, nil
	}
	return nil, fs.ErrNotExist
}

func (s *rangeEmptySpyFS) DiskUsage(path string) (uint64, uint64, error) { return 0, 0, nil }
func (s *rangeEmptySpyFS) CreateDir(path string) error                   { return fs.ErrPermission }
func (s *rangeEmptySpyFS) CreateFile(path string) (File, error)          { return nil, fs.ErrPermission }
func (s *rangeEmptySpyFS) OpenFile(path string, flag int) (File, error)  { return nil, fs.ErrPermission }
func (s *rangeEmptySpyFS) Remove(path string) error                      { return fs.ErrPermission }
func (s *rangeEmptySpyFS) Rename(oldpath, newpath string) error          { return fs.ErrPermission }
func (s *rangeEmptySpyFS) Capabilities() FileSystemCapabilities {
	return FileSystemCapabilities{ReadDirRange: true}
}
func (s *rangeEmptySpyFS) CatSearch(volumeRoot string, query string, reqMatches int32, cursor [16]byte) ([]string, [16]byte, int32) {
	return nil, cursor, ErrCallNotSupported
}
func (s *rangeEmptySpyFS) ChildCount(path string) (uint16, error) {
	return 0, newNotSupported("ChildCount")
}
func (s *rangeEmptySpyFS) DirAttributes(path string) (uint16, error) {
	return 0, nil
}
func (s *rangeEmptySpyFS) IsReadOnly(path string) (bool, error)        { return false, nil }
func (s *rangeEmptySpyFS) SupportsCatSearch(path string) (bool, error) { return false, nil }

func (s *rangeEmptySpyFS) ReadDirRange(path string, startIndex uint16, reqCount uint16) ([]fs.DirEntry, uint16, error) {
	// Deliberately returns an empty page with a bogus non-zero visibleCount to
	// emulate a backend that does not provide a reliable total count.
	return nil, 1000, nil
}

func (s *childCountSpyFS) ChildCount(path string) (uint16, error) {
	s.childCountCalls++
	switch filepath.Clean(path) {
	case filepath.Join(s.root, "Apps"):
		return 11, nil
	case filepath.Join(s.root, "Games"):
		return 22, nil
	default:
		return 0, newNotSupported("ChildCount")
	}
}

type denyReadDirFS struct {
	*LocalFileSystem
	denyPath string
}

func (d *denyReadDirFS) ReadDir(path string) ([]fs.DirEntry, error) {
	if filepath.Clean(path) == filepath.Clean(d.denyPath) {
		return nil, fs.ErrPermission
	}
	return d.LocalFileSystem.ReadDir(path)
}

func firstEnumerateLongName(entryData []byte) ([]byte, error) {
	if len(entryData) < 5 {
		return nil, fmt.Errorf("enumerate entry too short")
	}
	entryLen := int(entryData[0])
	if entryLen <= 0 || entryLen > len(entryData) {
		return nil, fmt.Errorf("invalid enumerate entry length")
	}
	entry := entryData[:entryLen]
	if len(entry) < 4 {
		return nil, fmt.Errorf("enumerate entry header too short")
	}

	// In FPEnumerate entries, parameters start at byte 2 (len + isDir).
	nameOff := int(binary.BigEndian.Uint16(entry[2:4]))
	namePos := 2 + nameOff
	if namePos >= len(entry) {
		return nil, fmt.Errorf("long name offset out of range")
	}
	nameLen := int(entry[namePos])
	if namePos+1+nameLen > len(entry) {
		return nil, fmt.Errorf("long name length out of range")
	}
	return append([]byte(nil), entry[namePos+1:namePos+1+nameLen]...), nil
}

func TestHandleEnumerate_LongNameEncodedAsMacRoman(t *testing.T) {
	root := t.TempDir()
	s := NewService("TestServer", []VolumeConfig{{Name: "Vol", Path: root}}, &LocalFileSystem{}, nil)

	hostName := "Netscape Navigator™ 2.02"
	if err := os.WriteFile(filepath.Join(root, hostName), []byte("x"), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	req := &FPEnumerateReq{
		VolumeID:   1,
		DirID:      CNIDRoot,
		FileBitmap: FileBitmapLongName,
		DirBitmap:  DirBitmapLongName,
		ReqCount:   64,
		StartIndex: 1,
		MaxReply:   1152,
		PathType:   2,
		Path:       "",
	}

	res, errCode := s.handleEnumerate(req)
	if errCode != NoErr {
		t.Fatalf("handleEnumerate err = %d, want %d", errCode, NoErr)
	}
	if res.ActCount == 0 {
		t.Fatalf("expected at least one enumerate entry")
	}

	gotName, err := firstEnumerateLongName(res.Data)
	if err != nil {
		t.Fatalf("parse enumerate long name: %v", err)
	}
	wantName := encoding.UTF8ToMacRoman(hostName)
	if !bytes.Equal(gotName, wantName) {
		t.Fatalf("enumerate name bytes = %x, want %x", gotName, wantName)
	}
	if !bytes.Contains(gotName, []byte{0xAA}) {
		t.Fatalf("expected MacRoman trademark byte 0xAA in enumerate name, got %x", gotName)
	}
}

func TestHandleEnumerate_PathDecodesMacRoman(t *testing.T) {
	root := t.TempDir()
	s := NewService("TestServer", []VolumeConfig{{Name: "Vol", Path: root}}, &LocalFileSystem{}, nil)

	dirName := "Folder™"
	fileName := "Inside™.txt"
	dirPath := filepath.Join(root, dirName)
	if err := os.Mkdir(dirPath, 0755); err != nil {
		t.Fatalf("seed dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirPath, fileName), []byte("x"), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	req := &FPEnumerateReq{
		VolumeID:   1,
		DirID:      CNIDRoot,
		FileBitmap: FileBitmapLongName,
		DirBitmap:  DirBitmapLongName,
		ReqCount:   64,
		StartIndex: 1,
		MaxReply:   1152,
		PathType:   2,
		Path:       "Folder\xaa",
	}

	res, errCode := s.handleEnumerate(req)
	if errCode != NoErr {
		t.Fatalf("handleEnumerate err = %d, want %d", errCode, NoErr)
	}
	if res.ActCount == 0 {
		t.Fatalf("expected enumerate result in decoded MacRoman directory")
	}

	gotName, err := firstEnumerateLongName(res.Data)
	if err != nil {
		t.Fatalf("parse enumerate long name: %v", err)
	}
	wantName := encoding.UTF8ToMacRoman(fileName)
	if !bytes.Equal(gotName, wantName) {
		t.Fatalf("enumerate name bytes = %x, want %x", gotName, wantName)
	}
}

// TestHandleEnumerate_SidecarsExcludedFromCount verifies that AppleDouble
// sidecar files (._name) are not counted in ActCount and are not returned as
// enumerable entries.
func TestHandleEnumerate_SidecarsExcludedFromCount(t *testing.T) {
	root := t.TempDir()
	s := NewService("TestServer", []VolumeConfig{{Name: "Vol", Path: root}}, &LocalFileSystem{}, nil)

	// Create 2 real files and a sidecar for each — 4 filesystem entries total.
	for _, name := range []string{"Alpha", "Beta"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0644); err != nil {
			t.Fatalf("seed file %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(root, "._"+name), []byte("ad"), 0644); err != nil {
			t.Fatalf("seed sidecar %s: %v", name, err)
		}
	}

	req := &FPEnumerateReq{
		VolumeID:   1,
		DirID:      CNIDRoot,
		FileBitmap: FileBitmapLongName,
		DirBitmap:  DirBitmapLongName,
		ReqCount:   64,
		StartIndex: 1,
		MaxReply:   4096,
		PathType:   2,
		Path:       "",
	}

	res, errCode := s.handleEnumerate(req)
	if errCode != NoErr {
		t.Fatalf("handleEnumerate err = %d, want NoErr", errCode)
	}
	if res.ActCount != 2 {
		t.Fatalf("ActCount = %d, want 2 (sidecars must not be counted)", res.ActCount)
	}
}

// TestHandleEnumerate_EndOfDirUsesVisibleCount verifies that the -5018
// end-of-directory signal is based on the number of visible (non-sidecar)
// entries, not the raw filesystem entry count.
func TestHandleEnumerate_EndOfDirUsesVisibleCount(t *testing.T) {
	root := t.TempDir()
	s := NewService("TestServer", []VolumeConfig{{Name: "Vol", Path: root}}, &LocalFileSystem{}, nil)

	// 2 real files + 2 sidecars = 4 raw entries, but only 2 visible.
	for _, name := range []string{"Alpha", "Beta"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0644); err != nil {
			t.Fatalf("seed file %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(root, "._"+name), []byte("ad"), 0644); err != nil {
			t.Fatalf("seed sidecar %s: %v", name, err)
		}
	}

	// StartIndex=3 is beyond the 2 visible entries: must return -5018.
	req := &FPEnumerateReq{
		VolumeID:   1,
		DirID:      CNIDRoot,
		FileBitmap: FileBitmapLongName,
		DirBitmap:  DirBitmapLongName,
		ReqCount:   64,
		StartIndex: 3,
		MaxReply:   4096,
		PathType:   2,
		Path:       "",
	}

	_, errCode := s.handleEnumerate(req)
	if errCode != ErrObjectNotFound {
		t.Fatalf("errCode = %d, want ErrObjectNotFound (%d) when StartIndex exceeds visible count", errCode, ErrObjectNotFound)
	}

	// StartIndex=2 is the last visible entry: must return NoErr with ActCount=1.
	req.StartIndex = 2
	res, errCode := s.handleEnumerate(req)
	if errCode != NoErr {
		t.Fatalf("errCode = %d, want NoErr for last visible entry", errCode)
	}
	if res.ActCount != 1 {
		t.Fatalf("ActCount = %d, want 1 for last visible entry", res.ActCount)
	}
}

func TestHandleEnumerate_UsesChildCountWithoutRecursiveReadDir(t *testing.T) {
	root := t.TempDir()
	spy := &childCountSpyFS{root: root}
	s := NewService("TestServer", []VolumeConfig{{Name: "Vol", Path: root}}, spy, nil)

	req := &FPEnumerateReq{
		VolumeID:   1,
		DirID:      CNIDRoot,
		FileBitmap: 0,
		DirBitmap:  DirBitmapLongName | DirBitmapOffspringCount,
		ReqCount:   64,
		StartIndex: 1,
		MaxReply:   1152,
		PathType:   2,
		Path:       "",
	}

	res, errCode := s.handleEnumerate(req)
	if errCode != NoErr {
		t.Fatalf("handleEnumerate err = %d, want %d", errCode, NoErr)
	}
	if res.ActCount != 2 {
		t.Fatalf("ActCount = %d, want 2", res.ActCount)
	}
	if spy.childCountCalls != 2 {
		t.Fatalf("ChildCount calls = %d, want 2", spy.childCountCalls)
	}
	if len(spy.readDirCalls) != 1 || filepath.Clean(spy.readDirCalls[0]) != filepath.Clean(root) {
		t.Fatalf("ReadDir calls = %v, want only root enumerate", spy.readDirCalls)
	}
}

func TestHandleEnumerate_UsesReadDirRangeWhenAvailable(t *testing.T) {
	root := t.TempDir()
	spy := &rangeSpyFS{root: root}
	s := NewService("TestServer", []VolumeConfig{{Name: "Vol", Path: root}}, spy, nil)

	req := &FPEnumerateReq{
		VolumeID:   1,
		DirID:      CNIDRoot,
		FileBitmap: 0,
		DirBitmap:  DirBitmapLongName,
		ReqCount:   2,
		StartIndex: 3,
		MaxReply:   1152,
		PathType:   2,
		Path:       "",
	}

	res, errCode := s.handleEnumerate(req)
	if errCode != NoErr {
		t.Fatalf("handleEnumerate err = %d, want %d", errCode, NoErr)
	}
	if res.ActCount != 2 {
		t.Fatalf("ActCount = %d, want 2", res.ActCount)
	}
	if len(spy.rangeCalls) != 1 || filepath.Clean(spy.rangeCalls[0]) != filepath.Clean(root) {
		t.Fatalf("ReadDirRange calls = %v, want only root", spy.rangeCalls)
	}
	if spy.lastStartIndex != 3 || spy.lastReqCount != 2 {
		t.Fatalf("ReadDirRange args = (%d, %d), want (3, 2)", spy.lastStartIndex, spy.lastReqCount)
	}
	if len(spy.readDirCalls) != 0 {
		t.Fatalf("ReadDir calls = %v, want none", spy.readDirCalls)
	}
	if res.Data == nil || len(res.Data) == 0 {
		t.Fatal("expected enumerate data from range provider")
	}
}

func TestHandleEnumerate_RangeEmptyPageReturnsObjectNotFound(t *testing.T) {
	root := t.TempDir()
	spy := &rangeEmptySpyFS{root: root}
	s := NewService("TestServer", []VolumeConfig{{Name: "Vol", Path: root}}, spy, nil)

	req := &FPEnumerateReq{
		VolumeID:   1,
		DirID:      CNIDRoot,
		FileBitmap: 0,
		DirBitmap:  DirBitmapLongName,
		ReqCount:   64,
		StartIndex: 11,
		MaxReply:   1152,
		PathType:   2,
		Path:       "",
	}

	_, errCode := s.handleEnumerate(req)
	if errCode != ErrObjectNotFound {
		t.Fatalf("errCode = %d, want ErrObjectNotFound (%d)", errCode, ErrObjectNotFound)
	}
}

// TestHandleEnumerate_LegacyAppleDoubleDirExcluded verifies that legacy
// metadata directories are never treated as user-visible entries.
func TestHandleEnumerate_LegacyAppleDoubleDirExcluded(t *testing.T) {
	root := t.TempDir()
	options := DefaultOptions()
	options.AppleDoubleMode = AppleDoubleModeLegacy
	s := NewService("TestServer", []VolumeConfig{{Name: "Vol", Path: root}}, &LocalFileSystem{}, nil, options)

	for _, name := range []string{"Alpha", "Beta"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0644); err != nil {
			t.Fatalf("seed file %s: %v", name, err)
		}
	}

	legacyDir := filepath.Join(root, ".AppleDouble")
	if err := os.MkdirAll(legacyDir, 0755); err != nil {
		t.Fatalf("mkdir legacy metadata dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "Alpha"), []byte("ad"), 0644); err != nil {
		t.Fatalf("seed legacy sidecar: %v", err)
	}

	req := &FPEnumerateReq{
		VolumeID:   1,
		DirID:      CNIDRoot,
		FileBitmap: FileBitmapLongName,
		DirBitmap:  DirBitmapLongName,
		ReqCount:   64,
		StartIndex: 1,
		MaxReply:   4096,
		PathType:   2,
		Path:       "",
	}

	res, errCode := s.handleEnumerate(req)
	if errCode != NoErr {
		t.Fatalf("handleEnumerate err = %d, want NoErr", errCode)
	}
	if res.ActCount != 2 {
		t.Fatalf("ActCount = %d, want 2 (legacy metadata dir must be hidden)", res.ActCount)
	}

	// StartIndex=3 is beyond the two visible entries and must signal end-of-dir.
	req.StartIndex = 3
	_, errCode = s.handleEnumerate(req)
	if errCode != ErrObjectNotFound {
		t.Fatalf("errCode = %d, want ErrObjectNotFound (%d)", errCode, ErrObjectNotFound)
	}
}

// TestHandleEnumerate_LegacyAppleDoubleDirCaseInsensitive ensures that
// .AppleDouble metadata directories are hidden regardless of on-disk case.
func TestHandleEnumerate_LegacyAppleDoubleDirCaseInsensitive(t *testing.T) {
	root := t.TempDir()
	options := DefaultOptions()
	options.AppleDoubleMode = AppleDoubleModeLegacy
	s := NewService("TestServer", []VolumeConfig{{Name: "Vol", Path: root}}, &LocalFileSystem{}, nil, options)

	if err := os.WriteFile(filepath.Join(root, "Visible"), []byte("x"), 0644); err != nil {
		t.Fatalf("seed visible file: %v", err)
	}

	legacyDir := filepath.Join(root, ".appledouble")
	if err := os.MkdirAll(legacyDir, 0755); err != nil {
		t.Fatalf("mkdir lowercase metadata dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "Visible"), []byte("ad"), 0644); err != nil {
		t.Fatalf("seed lowercase legacy sidecar: %v", err)
	}

	req := &FPEnumerateReq{
		VolumeID:   1,
		DirID:      CNIDRoot,
		FileBitmap: FileBitmapLongName,
		DirBitmap:  DirBitmapLongName,
		ReqCount:   64,
		StartIndex: 1,
		MaxReply:   4096,
		PathType:   2,
		Path:       "",
	}

	res, errCode := s.handleEnumerate(req)
	if errCode != NoErr {
		t.Fatalf("handleEnumerate err = %d, want NoErr", errCode)
	}
	if res.ActCount != 1 {
		t.Fatalf("ActCount = %d, want 1 (case-variant legacy metadata dir must be hidden)", res.ActCount)
	}
}

func TestHandleEnumerate_ErrorsForBitmapAndReplyValidation(t *testing.T) {
	root := t.TempDir()
	s := NewService("TestServer", []VolumeConfig{{Name: "Vol", Path: root}}, &LocalFileSystem{}, nil)

	_, errCode := s.handleEnumerate(&FPEnumerateReq{
		VolumeID:   999,
		DirID:      CNIDRoot,
		FileBitmap: FileBitmapLongName,
		DirBitmap:  DirBitmapLongName,
		ReqCount:   1,
		StartIndex: 1,
		MaxReply:   4096,
		PathType:   2,
	})
	if errCode != ErrParamErr {
		t.Fatalf("unknown VolumeID errCode=%d, want ErrParamErr (%d)", errCode, ErrParamErr)
	}

	_, errCode = s.handleEnumerate(&FPEnumerateReq{
		VolumeID:   1,
		DirID:      CNIDRoot,
		FileBitmap: FileBitmapLongName,
		DirBitmap:  DirBitmapLongName,
		ReqCount:   1,
		StartIndex: 1,
		MaxReply:   4096,
		PathType:   99,
		Path:       "anything",
	})
	if errCode != ErrParamErr {
		t.Fatalf("bad PathType errCode=%d, want ErrParamErr (%d)", errCode, ErrParamErr)
	}

	_, errCode = s.handleEnumerate(&FPEnumerateReq{
		VolumeID:   1,
		DirID:      CNIDRoot,
		FileBitmap: 0,
		DirBitmap:  0,
		ReqCount:   1,
		StartIndex: 1,
		MaxReply:   4096,
		PathType:   2,
	})
	if errCode != ErrBitmapErr {
		t.Fatalf("empty bitmaps errCode=%d, want ErrBitmapErr (%d)", errCode, ErrBitmapErr)
	}

	_, errCode = s.handleEnumerate(&FPEnumerateReq{
		VolumeID:   1,
		DirID:      CNIDRoot,
		FileBitmap: 0x8000,
		DirBitmap:  DirBitmapLongName,
		ReqCount:   1,
		StartIndex: 1,
		MaxReply:   4096,
		PathType:   2,
	})
	if errCode != ErrBitmapErr {
		t.Fatalf("unsupported bitmap errCode=%d, want ErrBitmapErr (%d)", errCode, ErrBitmapErr)
	}

	_, errCode = s.handleEnumerate(&FPEnumerateReq{
		VolumeID:   1,
		DirID:      CNIDRoot,
		FileBitmap: FileBitmapLongName,
		DirBitmap:  DirBitmapLongName,
		ReqCount:   1,
		StartIndex: 1,
		MaxReply:   4,
		PathType:   2,
	})
	if errCode != ErrParamErr {
		t.Fatalf("small MaxReply errCode=%d, want ErrParamErr (%d)", errCode, ErrParamErr)
	}

	_, errCode = s.handleEnumerate(&FPEnumerateReq{
		VolumeID:   1,
		DirID:      CNIDRoot,
		FileBitmap: FileBitmapLongName,
		DirBitmap:  DirBitmapLongName,
		ReqCount:   1,
		StartIndex: 1,
		MaxReply:   4096,
		PathType:   2,
		Path:       string([]byte{'b', 'a', 'd', 0x00, 0x00, 0x00, 0x00, 'n', 'a', 'm', 'e'}),
	})
	if errCode != ErrParamErr {
		t.Fatalf("bad pathname errCode=%d, want ErrParamErr (%d)", errCode, ErrParamErr)
	}
}

func TestHandleEnumerate_ErrorsForDirectoryTarget(t *testing.T) {
	root := t.TempDir()
	s := NewService("TestServer", []VolumeConfig{{Name: "Vol", Path: root}}, &LocalFileSystem{}, nil)

	if err := os.WriteFile(filepath.Join(root, "afile"), []byte("x"), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	_, errCode := s.handleEnumerate(&FPEnumerateReq{
		VolumeID:   1,
		DirID:      99999,
		FileBitmap: FileBitmapLongName,
		DirBitmap:  DirBitmapLongName,
		ReqCount:   1,
		StartIndex: 1,
		MaxReply:   4096,
		PathType:   2,
	})
	if errCode != ErrDirNotFound {
		t.Fatalf("unknown DirID errCode=%d, want ErrDirNotFound (%d)", errCode, ErrDirNotFound)
	}

	_, errCode = s.handleEnumerate(&FPEnumerateReq{
		VolumeID:   1,
		DirID:      CNIDRoot,
		FileBitmap: FileBitmapLongName,
		DirBitmap:  DirBitmapLongName,
		ReqCount:   1,
		StartIndex: 1,
		MaxReply:   4096,
		PathType:   2,
		Path:       "does-not-exist",
	})
	if errCode != ErrDirNotFound {
		t.Fatalf("missing target dir errCode=%d, want ErrDirNotFound (%d)", errCode, ErrDirNotFound)
	}

	_, errCode = s.handleEnumerate(&FPEnumerateReq{
		VolumeID:   1,
		DirID:      CNIDRoot,
		FileBitmap: FileBitmapLongName,
		DirBitmap:  DirBitmapLongName,
		ReqCount:   1,
		StartIndex: 1,
		MaxReply:   4096,
		PathType:   2,
		Path:       "afile",
	})
	if errCode != ErrObjectTypeErr {
		t.Fatalf("file target errCode=%d, want ErrObjectTypeErr (%d)", errCode, ErrObjectTypeErr)
	}
}

func TestHandleEnumerate_AccessDeniedFromReadDir(t *testing.T) {
	root := t.TempDir()
	denyDir := filepath.Join(root, "deny")
	if err := os.MkdirAll(denyDir, 0755); err != nil {
		t.Fatalf("mkdir deny dir: %v", err)
	}

	s := NewService("TestServer", []VolumeConfig{{Name: "Vol", Path: root}}, &denyReadDirFS{LocalFileSystem: &LocalFileSystem{}, denyPath: denyDir}, nil)

	_, errCode := s.handleEnumerate(&FPEnumerateReq{
		VolumeID:   1,
		DirID:      CNIDRoot,
		FileBitmap: FileBitmapLongName,
		DirBitmap:  DirBitmapLongName,
		ReqCount:   1,
		StartIndex: 1,
		MaxReply:   4096,
		PathType:   2,
		Path:       "deny",
	})
	if errCode != ErrAccessDenied {
		t.Fatalf("ReadDir permission errCode=%d, want ErrAccessDenied (%d)", errCode, ErrAccessDenied)
	}
}

func TestHandleEnumerate_AcceptsFinderFullBitmaps(t *testing.T) {
	root := t.TempDir()
	s := NewService("TestServer", []VolumeConfig{{Name: "Vol", Path: root}}, &LocalFileSystem{}, nil)

	if err := os.WriteFile(filepath.Join(root, "Alpha"), []byte("x"), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	res, errCode := s.handleEnumerate(&FPEnumerateReq{
		VolumeID:   1,
		DirID:      CNIDRoot,
		FileBitmap: 0x077f,
		DirBitmap:  0x137f,
		ReqCount:   64,
		StartIndex: 1,
		MaxReply:   1152,
		PathType:   2,
		Path:       "",
	})
	if errCode != NoErr {
		t.Fatalf("handleEnumerate errCode=%d, want NoErr", errCode)
	}
	if res == nil {
		t.Fatalf("handleEnumerate returned nil response")
	}
	if res.ActCount == 0 {
		t.Fatalf("ActCount=%d, want at least 1", res.ActCount)
	}
}

func TestHandleEnumerate_RespectsMaxReplyIncludingHeader(t *testing.T) {
	root := t.TempDir()
	s := NewService("TestServer", []VolumeConfig{{Name: "Vol", Path: root}}, &LocalFileSystem{}, nil)

	for i := 0; i < 40; i++ {
		name := fmt.Sprintf("Item-%02d", i)
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0644); err != nil {
			t.Fatalf("seed file %s: %v", name, err)
		}
	}

	req := &FPEnumerateReq{
		VolumeID:   1,
		DirID:      CNIDRoot,
		FileBitmap: 0x077f,
		DirBitmap:  0x137f,
		ReqCount:   64,
		StartIndex: 1,
		MaxReply:   1152,
		PathType:   2,
		Path:       "",
	}

	res, errCode := s.handleEnumerate(req)
	if errCode != NoErr {
		t.Fatalf("handleEnumerate errCode=%d, want NoErr", errCode)
	}
	if res == nil {
		t.Fatalf("handleEnumerate returned nil response")
	}
	if len(res.Marshal()) > int(req.MaxReply) {
		t.Fatalf("reply len=%d exceeds MaxReply=%d", len(res.Marshal()), req.MaxReply)
	}
}
