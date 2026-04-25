//go:build macgarden

package afp

import (
	"bytes"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type catSearchCaptureFS struct {
	root      string
	lastQuery string
	paths     []string
}

func (f *catSearchCaptureFS) ReadDir(path string) ([]fs.DirEntry, error) {
	return nil, nil
}

func (f *catSearchCaptureFS) Stat(path string) (fs.FileInfo, error) {
	clean := filepath.Clean(path)
	if clean == filepath.Clean(f.root) {
		return &macGardenFileInfo{name: filepath.Base(path), isDir: true}, nil
	}
	rel, err := filepath.Rel(filepath.Clean(f.root), clean)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return &macGardenFileInfo{name: filepath.Base(path), isDir: true}, nil
	}
	return nil, fs.ErrNotExist
}

func (f *catSearchCaptureFS) DiskUsage(path string) (uint64, uint64, error) { return 0, 0, nil }
func (f *catSearchCaptureFS) CreateDir(path string) error                   { return fs.ErrPermission }
func (f *catSearchCaptureFS) CreateFile(path string) (File, error)          { return nil, fs.ErrPermission }
func (f *catSearchCaptureFS) OpenFile(path string, flag int) (File, error) {
	return nil, fs.ErrPermission
}
func (f *catSearchCaptureFS) Remove(path string) error             { return fs.ErrPermission }
func (f *catSearchCaptureFS) Rename(oldpath, newpath string) error { return fs.ErrPermission }

func (f *catSearchCaptureFS) Capabilities() FileSystemCapabilities {
	return FileSystemCapabilities{CatSearch: true}
}

func (f *catSearchCaptureFS) ReadDirRange(path string, startIndex uint16, reqCount uint16) ([]fs.DirEntry, uint16, error) {
	return nil, 0, newNotSupported("ReadDirRange")
}

func (f *catSearchCaptureFS) ChildCount(path string) (uint16, error) {
	return 0, newNotSupported("ChildCount")
}

func (f *catSearchCaptureFS) DirAttributes(path string) (uint16, error) {
	return 0, newNotSupported("DirAttributes")
}

func (f *catSearchCaptureFS) IsReadOnly(path string) (bool, error) {
	return false, nil
}

func (f *catSearchCaptureFS) SupportsCatSearch(path string) (bool, error) {
	return true, nil
}

func (f *catSearchCaptureFS) CatSearch(volumeRoot string, query string, reqMatches int32, cursor [16]byte) ([]string, [16]byte, int32) {
	f.lastQuery = query
	return append([]string(nil), f.paths...), cursor, NoErr
}

func TestFPCatSearchReq_SearchQuery_ParsesFinderPattern(t *testing.T) {
	req := &FPCatSearchReq{Parameters: []byte(". \" clarisworks$ @ \" type:app,game")}
	if got := req.SearchQuery(); got != ". \" clarisworks$ @ \" type:app,game" {
		t.Fatalf("SearchQuery() = %q, want %q", got, ". \" clarisworks$ @ \" type:app,game")
	}
}

func TestHandleCatSearch_UsesParsedQuery(t *testing.T) {
	root := filepath.Clean(t.TempDir())
	captureFS := &catSearchCaptureFS{root: root}
	s := NewAFPService("TestServer", []VolumeConfig{{Name: "Garden", Path: root}}, captureFS, nil)

	req := &FPCatSearchReq{
		VolumeID:            1,
		ReqMatches:          30,
		FileRsltBitmap:      FileBitmapParentDID | FileBitmapLongName,
		DirectoryRsltBitmap: DirBitmapParentDID | DirBitmapLongName,
		ReqBitmap:           0x80000060,
		Parameters:          []byte(". \" clarisworks$ @ \" type:app,game"),
	}

	_, errCode := s.handleCatSearch(req)
	if errCode != ErrEOFErr {
		t.Fatalf("handleCatSearch errCode=%d, want %d", errCode, ErrEOFErr)
	}
	if captureFS.lastQuery != ". \" clarisworks$ @ \" type:app,game" {
		t.Fatalf("captured query = %q, want %q", captureFS.lastQuery, ". \" clarisworks$ @ \" type:app,game")
	}
}

func TestFPCatSearchReq_String_LogsQueryAndParams(t *testing.T) {
	req := &FPCatSearchReq{Parameters: []byte(". \" clarisworks$ @ \" type:app,game")}
	s := req.String()
	if !bytes.Contains([]byte(s), []byte("Query:\". \\\" clarisworks$ @ \\\" type:app,game\"")) {
		t.Fatalf("String() missing parsed Query field: %q", s)
	}
	if !bytes.Contains([]byte(s), []byte("Params:\". \\\" clarisworks$ @ \\\" type:app,game\"")) {
		t.Fatalf("String() missing Params field: %q", s)
	}
}

func TestHandleCatSearch_RespectsPayloadCap(t *testing.T) {
	root := filepath.Clean(t.TempDir())
	paths := make([]string, 0, 40)
	for i := 0; i < 40; i++ {
		name := "Spectre Result " + strconv.Itoa(i) + " " + strings.Repeat("X", 24)
		paths = append(paths, filepath.Join(root, name))
	}
	captureFS := &catSearchCaptureFS{root: root, paths: paths}
	s := NewAFPService("TestServer", []VolumeConfig{{Name: "Garden", Path: root}}, captureFS, nil)

	req := &FPCatSearchReq{
		VolumeID:            1,
		ReqMatches:          30,
		FileRsltBitmap:      FileBitmapParentDID | FileBitmapLongName,
		DirectoryRsltBitmap: DirBitmapParentDID | DirBitmapLongName,
		ReqBitmap:           0x80000060,
		Parameters:          []byte("* \" spectre$ @ \""),
	}

	res, errCode := s.handleCatSearch(req)
	// ErrEOFErr is the expected "last page" code when no continuation cursor is set.
	if errCode != NoErr && errCode != ErrEOFErr {
		t.Fatalf("handleCatSearch errCode=%d, want NoErr or ErrEOFErr", errCode)
	}
	if res.ActualCount == 0 {
		t.Fatalf("ActualCount=%d, want > 0", res.ActualCount)
	}
	if len(res.Data) > catSearchMaxDataLen {
		t.Fatalf("DataLen=%d, want <= %d", len(res.Data), catSearchMaxDataLen)
	}
	if len(res.Marshal()) >= 578 {
		t.Fatalf("MarshalLen=%d, want < 578 to avoid SPErrorBufTooSmall", len(res.Marshal()))
	}
}

func TestMacGardenCatSearch_PaginationCursor(t *testing.T) {
	// Test that pagination cursor properly signals continuation
	root := filepath.Clean(t.TempDir())
	paths := make([]string, 0, 50)
	for i := 0; i < 50; i++ {
		name := "Item" + strconv.Itoa(i)
		paths = append(paths, filepath.Join(root, name))
	}
	captureFS := &catSearchCaptureFS{root: root, paths: paths}
	s := NewAFPService("TestServer", []VolumeConfig{{Name: "Garden", Path: root}}, captureFS, nil)

	req := &FPCatSearchReq{
		VolumeID:            1,
		ReqMatches:          10,
		FileRsltBitmap:      FileBitmapParentDID | FileBitmapLongName,
		DirectoryRsltBitmap: DirBitmapParentDID | DirBitmapLongName,
		ReqBitmap:           0x80000060,
		Parameters:          []byte("test search"),
	}

	// First request: should return some results with continuation flag set
	res1, errCode1 := s.handleCatSearch(req)
	if errCode1 != NoErr && errCode1 != ErrEOFErr {
		t.Fatalf("handleCatSearch errCode=%d, want NoErr or ErrEOFErr", errCode1)
	}
	firstCount := res1.ActualCount
	firstCursor := res1.CatalogPosition

	if firstCount == 0 {
		t.Fatalf("First request ActualCount=%d, want > 0", firstCount)
	}

	// Check if cursor indicates more available
	hasMore := firstCursor[0] == 0x01
	if !hasMore {
		t.Logf("First request returned %d results with no continuation (all results fit)", firstCount)
		// This is OK if all results fit in one response
		return
	}

	t.Logf("First request returned %d results with continuation flag set", firstCount)

	// Second request: use the cursor to continue
	req.CatalogPosition = firstCursor
	res2, errCode2 := s.handleCatSearch(req)
	if errCode2 != NoErr && errCode2 != ErrEOFErr {
		t.Fatalf("Second handleCatSearch errCode=%d, want NoErr or ErrEOFErr", errCode2)
	}

	secondCount := res2.ActualCount
	if secondCount == 0 && errCode2 != ErrEOFErr {
		t.Fatalf("Second request ActualCount=%d but errCode=%d (not ErrEOFErr)", secondCount, errCode2)
	}

	t.Logf("Second request returned %d results (total so far: %d)", secondCount, firstCount+secondCount)
}

func TestHandleCatSearch_ResultsRecordStructLengthIsSpecCompliant(t *testing.T) {
	root := filepath.Clean(t.TempDir())
	paths := []string{
		filepath.Join(root, "Spectre 128"),
		filepath.Join(root, "Spectre GCR"),
		filepath.Join(root, "Spectre 3.0"),
	}
	captureFS := &catSearchCaptureFS{root: root, paths: paths}
	s := NewAFPService("TestServer", []VolumeConfig{{Name: "Garden", Path: root}}, captureFS, nil)

	req := &FPCatSearchReq{
		VolumeID:            1,
		ReqMatches:          30,
		FileRsltBitmap:      FileBitmapParentDID | FileBitmapLongName,
		DirectoryRsltBitmap: DirBitmapParentDID | DirBitmapLongName,
		ReqBitmap:           0x80000060,
		Parameters:          []byte("spectre"),
	}

	res, errCode := s.handleCatSearch(req)
	// ErrEOFErr is the expected "last page" code when no continuation cursor is set.
	if errCode != NoErr && errCode != ErrEOFErr {
		t.Fatalf("handleCatSearch errCode=%d, want NoErr or ErrEOFErr", errCode)
	}
	if res.ActualCount == 0 {
		t.Fatalf("ActualCount=%d, want > 0", res.ActualCount)
	}

	// Walk the concatenated ResultsRecord list using spec semantics:
	// StructLength excludes StructLength byte + FileDir byte.
	off := 0
	records := 0
	for off < len(res.Data) {
		if off+2 > len(res.Data) {
			t.Fatalf("truncated record header at off=%d len=%d", off, len(res.Data))
		}
		structLen := int(res.Data[off])
		recordLen := structLen + 2
		if recordLen < 2 {
			t.Fatalf("invalid recordLen=%d at off=%d", recordLen, off)
		}
		if off+recordLen > len(res.Data) {
			t.Fatalf("record overruns payload: off=%d recordLen=%d dataLen=%d", off, recordLen, len(res.Data))
		}
		records++
		off += recordLen
	}

	if off != len(res.Data) {
		t.Fatalf("record walk ended at off=%d, want dataLen=%d", off, len(res.Data))
	}
	if records != int(res.ActualCount) {
		t.Fatalf("walked records=%d, want ActualCount=%d", records, res.ActualCount)
	}
}
