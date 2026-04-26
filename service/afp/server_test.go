package afp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAFP_FPGetSrvrParms(t *testing.T) {
	s := NewService("TestServer", []VolumeConfig{
		{Name: "Vol1", Path: "/tmp/vol1"},
		{Name: "Vol2", Path: "/tmp/vol2"},
	}, nil, nil) // no need for real FS for this test

	resStruct, errCode := s.handleGetSrvrParms(&FPGetSrvrParmsReq{})
	if errCode != NoErr {
		t.Fatalf("Expected NoErr, got %v", errCode)
	}

	res := resStruct.Marshal()

	buf := bytes.NewReader(res)

	var serverTime uint32
	if err := binary.Read(buf, binary.BigEndian, &serverTime); err != nil {
		t.Fatal(err)
	}

	// Just ensure it parsed a number (which should be toAFPTime(time.Now()))
	if serverTime == 0 {
		t.Fatalf("Expected non-zero AFP timestamp")
	}

	numVols, err := buf.ReadByte()
	if err != nil {
		t.Fatal(err)
	}
	if numVols != 2 {
		t.Fatalf("Expected 2 vols, got %d", numVols)
	}

	for i := 0; i < int(numVols); i++ {
		flags, err := buf.ReadByte()
		if err != nil {
			t.Fatal(err)
		}
		if flags != 0 {
			t.Errorf("Expected flags to be 0 for 'No Password', got %02x", flags)
		}

		nameLen, err := buf.ReadByte()
		if err != nil {
			t.Fatal(err)
		}

		nameBuf := make([]byte, nameLen)
		if _, err := buf.Read(nameBuf); err != nil {
			t.Fatal(err)
		}

		name := string(nameBuf)
		if i == 0 && name != "Vol1" {
			t.Errorf("Expected Vol1, got %s", name)
		}
		if i == 1 && name != "Vol2" {
			t.Errorf("Expected Vol2, got %s", name)
		}
	}
}

func TestAFP_FPGetSrvrParms_NoPerEntryPadding(t *testing.T) {
	s := NewService("TestServer", []VolumeConfig{
		{Name: "Test Volume", Path: "/tmp/test"},
		{Name: "Volume 68K", Path: "/tmp/68k"},
	}, nil, nil)

	resStruct, errCode := s.handleGetSrvrParms(&FPGetSrvrParmsReq{})
	if errCode != NoErr {
		t.Fatalf("Expected NoErr, got %v", errCode)
	}

	res := resStruct.Marshal()
	if len(res) < 6 {
		t.Fatalf("Expected non-trivial FPGetSrvrParms reply, got %d bytes", len(res))
	}

	if got := res[4]; got != 2 {
		t.Fatalf("Expected 2 vols, got %d", got)
	}

	vols := res[5:]
	expected := append([]byte{0x00, byte(len("Test Volume"))}, []byte("Test Volume")...)
	expected = append(expected, 0x00, byte(len("Volume 68K")))
	expected = append(expected, []byte("Volume 68K")...)

	if !bytes.Equal(vols, expected) {
		t.Fatalf("Unexpected volume payload bytes: got=%x want=%x", vols, expected)
	}

	var parsed FPGetSrvrParmsRes
	if err := parsed.Unmarshal(res); err != nil {
		t.Fatalf("Expected parse success, got error: %v", err)
	}
	if len(parsed.Volumes) != 2 {
		t.Fatalf("Expected 2 parsed volumes, got %d", len(parsed.Volumes))
	}
	if parsed.Volumes[0].Name != "Test Volume" {
		t.Fatalf("Expected first volume name Test Volume, got %q", parsed.Volumes[0].Name)
	}
	if parsed.Volumes[1].Name != "Volume 68K" {
		t.Fatalf("Expected second volume name Volume 68K, got %q", parsed.Volumes[1].Name)
	}
}

func TestAFP_PersistentVolumeIDs_AreDeterministicByName(t *testing.T) {
	configs := []VolumeConfig{
		{Name: "Archive", Path: t.TempDir()},
		{Name: "Games", Path: t.TempDir()},
	}
	opts := DefaultOptions()
	opts.PersistentVolumeIDs = true

	s1 := NewService("TestServer", configs, nil, nil, opts)
	s2 := NewService("TestServer", configs, nil, nil, opts)

	if len(s1.Volumes) != len(s2.Volumes) {
		t.Fatalf("volume count mismatch: %d vs %d", len(s1.Volumes), len(s2.Volumes))
	}
	for i := range s1.Volumes {
		if s1.Volumes[i].ID == 0 {
			t.Fatalf("volume %q has zero ID", s1.Volumes[i].Config.Name)
		}
		if s1.Volumes[i].ID != s2.Volumes[i].ID {
			t.Fatalf("volume %q ID mismatch across instances: %d vs %d", s1.Volumes[i].Config.Name, s1.Volumes[i].ID, s2.Volumes[i].ID)
		}
	}
}

func TestAFP_PersistentVolumeIDs_ResolveNameCollisions(t *testing.T) {
	configs := []VolumeConfig{
		{Name: "Shared", Path: filepath.Join(t.TempDir(), "a")},
		{Name: "Shared", Path: filepath.Join(t.TempDir(), "b")},
	}
	opts := DefaultOptions()
	opts.PersistentVolumeIDs = true

	s := NewService("TestServer", configs, nil, nil, opts)
	if len(s.Volumes) != 2 {
		t.Fatalf("expected 2 volumes, got %d", len(s.Volumes))
	}
	if s.Volumes[0].ID == s.Volumes[1].ID {
		t.Fatalf("expected unique IDs for colliding names, got %d", s.Volumes[0].ID)
	}
}

func TestAFP_PersistentVolumeIDs_AreReturnedByOpenVol(t *testing.T) {
	root := t.TempDir()
	opts := DefaultOptions()
	opts.PersistentVolumeIDs = true

	s := NewService("TestServer", []VolumeConfig{{Name: "Archive", Path: root}}, &LocalFileSystem{}, nil, opts)
	if len(s.Volumes) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(s.Volumes))
	}
	wantID := s.Volumes[0].ID

	res, errCode := s.handleOpenVol(&FPOpenVolReq{Bitmap: VolBitmapVolID, VolName: "Archive"})
	if errCode != NoErr {
		t.Fatalf("handleOpenVol errCode=%d, want %d", errCode, NoErr)
	}
	if res.Bitmap&VolBitmapVolID == 0 {
		t.Fatalf("response bitmap missing VolID bit: %#04x", res.Bitmap)
	}
	if len(res.Data) < 2 {
		t.Fatalf("response data too short: %d", len(res.Data))
	}
	gotID := binary.BigEndian.Uint16(res.Data[:2])
	if gotID != wantID {
		t.Fatalf("openvol returned VolumeID=%d, want %d", gotID, wantID)
	}
}

func TestAFP_PersistentVolumeIDs_AreReturnedByGetVolParms(t *testing.T) {
	root := t.TempDir()
	opts := DefaultOptions()
	opts.PersistentVolumeIDs = true

	s := NewService("TestServer", []VolumeConfig{{Name: "Archive", Path: root}}, &LocalFileSystem{}, nil, opts)
	if len(s.Volumes) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(s.Volumes))
	}
	wantID := s.Volumes[0].ID

	res, errCode := s.handleGetVolParms(&FPGetVolParmsReq{VolumeID: wantID, Bitmap: VolBitmapVolID})
	if errCode != NoErr {
		t.Fatalf("handleGetVolParms errCode=%d, want %d", errCode, NoErr)
	}
	if res.Bitmap&VolBitmapVolID == 0 {
		t.Fatalf("response bitmap missing VolID bit: %#04x", res.Bitmap)
	}
	if len(res.Data) < 2 {
		t.Fatalf("response data too short: %d", len(res.Data))
	}
	gotID := binary.BigEndian.Uint16(res.Data[:2])
	if gotID != wantID {
		t.Fatalf("getvolparms returned VolumeID=%d, want %d", gotID, wantID)
	}
}

func TestAFP_FPGetSrvrParms_VolumeFlags(t *testing.T) {
	s := NewService("TestServer", []VolumeConfig{
		{Name: "ReadOnly", Path: "/tmp/ro", ReadOnly: true},
		{Name: "Protected", Path: "/tmp/pw", Password: "secret"},
		{Name: "Both", Path: "/tmp/both", Password: "secret", ReadOnly: true},
	}, nil, nil)

	resStruct, errCode := s.handleGetSrvrParms(&FPGetSrvrParmsReq{})
	if errCode != NoErr {
		t.Fatalf("Expected NoErr, got %v", errCode)
	}

	res := resStruct.Marshal()
	var parsed FPGetSrvrParmsRes
	if err := parsed.Unmarshal(res); err != nil {
		t.Fatalf("Expected parse success, got error: %v", err)
	}
	if len(parsed.Volumes) != 3 {
		t.Fatalf("Expected 3 parsed volumes, got %d", len(parsed.Volumes))
	}

	if parsed.Volumes[0].Flags != 0 {
		t.Fatalf("Expected ReadOnly flags=%#02x, got %#02x", uint8(0), parsed.Volumes[0].Flags)
	}
	if parsed.Volumes[1].Flags != VolInfoFlagHasPassword {
		t.Fatalf("Expected Protected flags=%#02x, got %#02x", VolInfoFlagHasPassword, parsed.Volumes[1].Flags)
	}
	if parsed.Volumes[2].Flags != VolInfoFlagHasPassword {
		t.Fatalf("Expected Both flags=%#02x, got %#02x", VolInfoFlagHasPassword, parsed.Volumes[2].Flags)
	}
}

func TestAFP_GetVolParms_AttributesReadOnlyBitOnly(t *testing.T) {
	s := NewService("TestServer", []VolumeConfig{
		{Name: "RW", Path: "/tmp/rw"},
		{Name: "RO", Path: "/tmp/ro", ReadOnly: true},
	}, nil, nil)

	rwRes, rwErr := s.handleGetVolParms(&FPGetVolParmsReq{VolumeID: 1, Bitmap: VolBitmapAttributes})
	if rwErr != NoErr {
		t.Fatalf("RW handleGetVolParms err = %d, want %d", rwErr, NoErr)
	}
	if len(rwRes.Data) < 2 {
		t.Fatalf("RW response too short: %d", len(rwRes.Data))
	}
	rwAttrs := binary.BigEndian.Uint16(rwRes.Data[:2])
	if rwAttrs != 0 {
		t.Fatalf("RW attrs = %#04x, want %#04x", rwAttrs, uint16(0))
	}

	roRes, roErr := s.handleGetVolParms(&FPGetVolParmsReq{VolumeID: 2, Bitmap: VolBitmapAttributes})
	if roErr != NoErr {
		t.Fatalf("RO handleGetVolParms err = %d, want %d", roErr, NoErr)
	}
	if len(roRes.Data) < 2 {
		t.Fatalf("RO response too short: %d", len(roRes.Data))
	}
	roAttrs := binary.BigEndian.Uint16(roRes.Data[:2])
	if roAttrs != VolAttrReadOnly {
		t.Fatalf("RO attrs = %#04x, want %#04x", roAttrs, VolAttrReadOnly)
	}
}

func TestAFP_OtherMethods(t *testing.T) {
	s := NewService("TestServer", []VolumeConfig{
		{Name: "Vol1", Path: "/tmp/vol1"},
	}, nil, nil)

	// FPGetSrvrInfo
	infoReq := []byte{FPGetSrvrInfo}
	infoRes, errCode := s.HandleCommand(infoReq)
	if errCode != NoErr {
		t.Errorf("Expected NoErr, got %v", errCode)
	}
	if len(infoRes) < 8 {
		t.Errorf("Expected info res to be populated")
	}

	// FPLogin - no uam
	loginReq := []byte{FPLogin, byte(len(Version20))}
	loginReq = append(loginReq, []byte(Version20)...)
	loginReq = append(loginReq, byte(len(UAMNoUserAuthent)))
	loginReq = append(loginReq, []byte(UAMNoUserAuthent)...)
	loginRes, errCode := s.HandleCommand(loginReq)
	if errCode != NoErr {
		t.Errorf("Expected FPLogin NoErr, got %v", errCode)
	}
	if len(loginRes) != 4 {
		t.Errorf("Expected FPLogin to return 4 bytes, got %d", len(loginRes))
	}

	// FPLogout
	logoutRes, errCode := s.HandleCommand([]byte{FPLogout})
	if errCode != NoErr {
		t.Errorf("Expected FPLogout NoErr, got %v", errCode)
	}
	if logoutRes != nil {
		t.Errorf("Expected nil res for FPLogout")
	}

	// FPCloseDir
	closeDirRes, errCode := s.HandleCommand([]byte{FPCloseDir, 0x00, 0x01, 0x02, 0x00, 0x00, 0x00, 0x00}) // fake payload
	if errCode != NoErr {
		t.Errorf("Expected FPCloseDir NoErr, got %v", errCode)
	}
	if closeDirRes != nil {
		t.Errorf("Expected nil res for FPCloseDir")
	}

	// FPGetSrvrMsg — cmd(0), pad(1), MessageType(2:4), Bitmap(4:6)
	getSrvrMsgReq := []byte{FPGetSrvrMsg, 0x00, 0x00, 0x00, 0x00, 0x00}
	res, errCode := s.HandleCommand(getSrvrMsgReq)
	if errCode != NoErr {
		t.Errorf("Expected FPGetSrvrMsg to succeed, got %v", errCode)
	}
	if res == nil {
		t.Errorf("Expected non-nil res for FPGetSrvrMsg")
	}

	// Unhandled Method — use a command code that has no case in the switch
	_, errCode = s.HandleCommand([]byte{0xFF})
	if errCode != ErrCallNotSupported {
		t.Errorf("Expected unknown command to return ErrCallNotSupported, got %v", errCode)
	}

	// FPCreateFile: cmd(0), flag(1), VolumeID(2:4), DirID(4:8), PathType(8), PathLen(9), PathName(10:...)
	// Send a zero-length path — the handler will resolve to the root DID path (/tmp/vol1) which is a dir,
	// so ErrAccessDenied is expected when creating a file with no name; just verify it parses (not ErrParamErr).
	createFileReq := []byte{FPCreateFile, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x02, 0x02, 0x00}
	_, errCode = s.HandleCommand(createFileReq)
	if errCode == ErrParamErr {
		t.Errorf("FPCreateFile should parse successfully, got ErrParamErr")
	}

	// FPCreateDir: cmd(0), pad(1), VolumeID(2:4), DirID(4:8), PathType(8), PathLen(9), PathName(10:...)
	createDirReq := []byte{FPCreateDir, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x02, 0x02, 0x00}
	_, errCode = s.HandleCommand(createDirReq)
	if errCode == ErrParamErr {
		t.Errorf("FPCreateDir should parse successfully, got ErrParamErr")
	}

	// FPDelete: cmd(0), pad(1), VolumeID(2:4), DirID(4:8), PathType(8), PathLen(9), PathName(10:...)
	deleteReq := []byte{FPDelete, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x02, 0x02, 0x00}
	_, errCode = s.HandleCommand(deleteReq)
	if errCode == ErrParamErr {
		t.Errorf("FPDelete should parse successfully, got ErrParamErr")
	}
	_ = res
}

// Add more complex methods that require fs interaction using a stub or simple struct
type mockFS struct {
	t            *testing.T
	totalBytes   uint64
	freeBytes    uint64
	diskUsageErr error
}

func (m *mockFS) ReadDir(name string) ([]fs.DirEntry, error) {
	return nil, nil
}
func (m *mockFS) Stat(name string) (fs.FileInfo, error) {
	return nil, nil
}
func (m *mockFS) DiskUsage(name string) (uint64, uint64, error) {
	if m.diskUsageErr != nil {
		return 0, 0, m.diskUsageErr
	}
	if m.totalBytes == 0 && m.freeBytes == 0 {
		return defaultAFPBytesTotal, defaultAFPBytesFree, nil
	}
	return m.totalBytes, m.freeBytes, nil
}
func (m *mockFS) StatWithMetadataFallback(name string) (string, fs.FileInfo, error) {
	info, err := m.Stat(name)
	return name, info, err
}
func (m *mockFS) ReadForkMetadata(name string) (ForkMetadata, error) {
	return ForkMetadata{}, nil
}
func (m *mockFS) WriteFinderInfo(name string, finderInfo [32]byte) error {
	return nil
}
func (m *mockFS) OpenResourceFork(name string, writable bool) (File, ResourceForkInfo, error) {
	return nil, ResourceForkInfo{}, nil
}
func (m *mockFS) TruncateResourceFork(file File, info ResourceForkInfo, newLen int64) error {
	return nil
}
func (m *mockFS) MoveMetadata(oldpath, newpath string) error {
	return nil
}
func (m *mockFS) DeleteMetadata(path string) error {
	return nil
}
func (m *mockFS) CopyMetadata(srcPath, dstPath string) error {
	return nil
}
func (m *mockFS) CopyMetadataFrom(source ForkMetadataBackend, srcPath, dstPath string) error {
	return nil
}
func (m *mockFS) ExchangeMetadata(pathA, pathB string) error {
	return nil
}
func (m *mockFS) OpenFile(name string, flag int) (File, error) {
	return nil, nil
}
func (m *mockFS) Rename(oldpath, newpath string) error {
	return nil
}
func (m *mockFS) Capabilities() FileSystemCapabilities {
	return FileSystemCapabilities{
		ReadDirRange:  true,
		ChildCount:    true,
		DirAttributes: true,
		ReadOnlyState: true,
	}
}
func (m *mockFS) CatSearch(volumeRoot string, query string, reqMatches int32, cursor [16]byte) ([]string, [16]byte, int32) {
	return nil, cursor, ErrCallNotSupported
}
func (m *mockFS) ChildCount(path string) (uint16, error) {
	return 0, newNotSupported("ChildCount")
}
func (m *mockFS) ReadDirRange(path string, startIndex uint16, reqCount uint16) ([]fs.DirEntry, uint16, error) {
	return nil, 0, newNotSupported("ReadDirRange")
}
func (m *mockFS) DirAttributes(path string) (uint16, error) {
	return 0, nil
}
func (m *mockFS) IsReadOnly(path string) (bool, error) {
	return false, nil
}
func (m *mockFS) SupportsCatSearch(path string) (bool, error) {
	return false, nil
}

func TestAFP_FSDependentMethods(t *testing.T) {
	s := NewService("TestServer", []VolumeConfig{
		{Name: "Vol1", Path: "/tmp/vol1"},
	}, &mockFS{t: t}, nil)

	// Add test for OpenVol
	openVolReq := []byte{FPOpenVol, 0x00} // Cmd + Pad
	// OpenVol requires VOLPBIT_VID to be present, otherwise AFPERR_BITMAP.
	openVolReq = append(openVolReq, []byte{0x00, 0x20}...) // Bitmap: VolID only
	openVolReq = append(openVolReq, byte(len("Vol1")))
	openVolReq = append(openVolReq, []byte("Vol1")...)
	openVolReq = append(openVolReq, byte(0)) // Password len = 0

	res, errCode := s.HandleCommand(openVolReq)
	if errCode != NoErr {
		t.Errorf("Expected OpenVol to succeed, got %v", errCode)
	}
	if len(res) == 0 {
		t.Errorf("Expected non-empty response for OpenVol")
	}

	// Vol ID usually starts from 1 based on array index
	volID := uint16(1)

	// GetVolParms
	getVolReq := make([]byte, 6)
	getVolReq[0] = FPGetVolParms
	binary.BigEndian.PutUint16(getVolReq[2:4], volID)
	binary.BigEndian.PutUint16(getVolReq[4:6], 0x0001) // Bitmap
	res, errCode = s.HandleCommand(getVolReq)
	if errCode != NoErr {
		t.Errorf("Expected GetVolParms to succeed, got %v", errCode)
	}
	if len(res) < 2 {
		t.Fatalf("Expected GetVolParms response bytes")
	}

	// FPSetVolParms: set backup date and verify it via FPGetVolParms.
	backupDate := uint32(1234)
	setVolReq := make([]byte, 10)
	setVolReq[0] = FPSetVolParms
	setVolReq[1] = 0x00 // pad
	binary.BigEndian.PutUint16(setVolReq[2:4], volID)
	binary.BigEndian.PutUint16(setVolReq[4:6], VolBitmapBackupDate)
	binary.BigEndian.PutUint32(setVolReq[6:10], backupDate)
	_, errCode = s.HandleCommand(setVolReq)
	if errCode != NoErr {
		t.Errorf("Expected FPSetVolParms to succeed, got %v", errCode)
	}

	// Request only the backup date field.
	getBackupReq := make([]byte, 6)
	getBackupReq[0] = FPGetVolParms
	binary.BigEndian.PutUint16(getBackupReq[2:4], volID)
	binary.BigEndian.PutUint16(getBackupReq[4:6], VolBitmapBackupDate)
	res, errCode = s.HandleCommand(getBackupReq)
	if errCode != NoErr {
		t.Errorf("Expected GetVolParms(BackupDate) to succeed, got %v", errCode)
	}
	if len(res) < 6 {
		t.Fatalf("Expected GetVolParms(BackupDate) response len >= 6, got %d", len(res))
	}
	// Response: bitmap(2) + backupDate(4).
	gotBackup := binary.BigEndian.Uint32(res[2:6])
	if gotBackup != backupDate {
		t.Fatalf("Expected backupDate %d, got %d", backupDate, gotBackup)
	}

	// OpenDir (DID 2 = root)
	openDirReq := make([]byte, 10)
	openDirReq[0] = FPOpenDir
	binary.BigEndian.PutUint16(openDirReq[2:4], volID)
	binary.BigEndian.PutUint32(openDirReq[4:8], 2) // DirID 2
	openDirReq[8] = 0                              // PathType
	openDirReq[9] = 0                              // Path length
	res, errCode = s.HandleCommand(openDirReq)
	if errCode != NoErr {
		t.Errorf("Expected OpenDir to succeed, got %v", errCode)
	}
	if len(res) < 4 {
		t.Errorf("Expected DirID back")
	}

	// CloseVol
	closeVolReq := make([]byte, 4)
	closeVolReq[0] = FPCloseVol
	binary.BigEndian.PutUint16(closeVolReq[2:4], volID)
	res, errCode = s.HandleCommand(closeVolReq)
	if errCode != NoErr {
		t.Errorf("Expected CloseVol to succeed, got %v", errCode)
	}
}

func TestAFP_GetVolParms_ModDateBytesFreeWireLayout(t *testing.T) {
	root := t.TempDir()
	// Ensure the volume root has a deterministic timestamp for ModDate packing.
	volMod := time.Date(2024, time.January, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(root, volMod, volMod); err != nil {
		t.Fatalf("Chtimes(root): %v", err)
	}

	s := NewService("TestServer", []VolumeConfig{{Name: "Vol1", Path: root}}, &mockFS{
		t:          t,
		totalBytes: uint64(math.MaxUint32) + 12345,
		freeBytes:  uint64(math.MaxUint32) + 99,
	}, nil)

	req := &FPGetVolParmsReq{VolumeID: 1, Bitmap: VolBitmapModDate | VolBitmapBytesFree}
	res, errCode := s.handleGetVolParms(req)
	if errCode != NoErr {
		t.Fatalf("Expected NoErr, got %d", errCode)
	}

	wire := res.Marshal()
	if len(wire) != 10 {
		t.Fatalf("Expected 10-byte response (bitmap + modDate + bytesFree), got %d", len(wire))
	}

	if gotBitmap := binary.BigEndian.Uint16(wire[0:2]); gotBitmap != req.Bitmap {
		t.Fatalf("Bitmap mismatch: got 0x%04x want 0x%04x", gotBitmap, req.Bitmap)
	}

	gotModDate := binary.BigEndian.Uint32(wire[2:6])
	if gotModDate == 0 {
		t.Fatalf("Expected non-zero ModDate")
	}

	gotBytesFree := binary.BigEndian.Uint32(wire[6:10])
	if gotBytesFree != math.MaxInt32 {
		t.Fatalf("BytesFree mismatch: got 0x%08x want 0x%08x", gotBytesFree, uint32(math.MaxInt32))
	}
}

func TestAFP_OpenVolPasswordEnforcement(t *testing.T) {
	s := NewService("TestServer", []VolumeConfig{
		{Name: "Vol1", Path: "/tmp/vol1", Password: "secret"},
	}, &mockFS{t: t}, nil)

	// Wire format we support:
	// cmd(0), pad(1), Bitmap(2:4), VolName(pascal string), pad to even boundary,
	// Password fixed 8 bytes (NUL padded).
	//
	// VolName="Vol1" => pascal length byte=4, name bytes=4, total=5; passIdx becomes odd,
	// so we include a pad byte before the password field.
	openVolReqOK := make([]byte, 18)
	openVolReqOK[0] = FPOpenVol
	openVolReqOK[1] = 0x00 // pad
	binary.BigEndian.PutUint16(openVolReqOK[2:4], VolBitmapVolID)
	openVolReqOK[4] = byte(len("Vol1"))
	copy(openVolReqOK[5:9], []byte("Vol1"))
	openVolReqOK[9] = 0x00                      // pad to even boundary for password field
	copy(openVolReqOK[10:18], []byte("secret")) // NUL padded by zeroed slice

	_, errCode := s.HandleCommand(openVolReqOK)
	if errCode != NoErr {
		t.Fatalf("Expected OpenVol to succeed with correct password, got err=%d", errCode)
	}

	openVolReqBad := make([]byte, 18)
	copy(openVolReqBad, openVolReqOK)
	copy(openVolReqBad[10:18], []byte("wrongpw")) // different password => should fail

	_, errCode = s.HandleCommand(openVolReqBad)
	if errCode != ErrAccessDenied {
		t.Fatalf("Expected OpenVol to fail with wrong password (ErrAccessDenied=%d), got %d", ErrAccessDenied, errCode)
	}
}

func (m *mockFS) CreateDir(name string) error {
	return nil
}

func (m *mockFS) Delete(name string) error {
	return nil
}

func (m *mockFS) CreateFile(name string) (File, error) {
	return nil, nil
}

func (m *mockFS) Remove(name string) error {
	return nil
}

func TestMemoryCNIDStore_ReservedIDs(t *testing.T) {
	store := NewMemoryCNIDStore()
	rootPath := filepath.Join("/volumes", "share")

	if got := store.EnsureReserved(rootPath, CNIDRoot); got != CNIDRoot {
		t.Fatalf("root CNID = %d, want %d", got, CNIDRoot)
	}
	if got := store.Ensure(filepath.Join(rootPath, "docs")); got <= CNIDRoot {
		t.Fatalf("dynamic CNID = %d, want > %d", got, CNIDRoot)
	}
	if path, ok := store.Path(CNIDRoot); !ok || path != rootPath {
		t.Fatalf("Path(root) = %q, %t", path, ok)
	}
}

func TestGetPathDID_RoundTrip(t *testing.T) {
	s := NewService("TestServer", []VolumeConfig{
		{Name: "Vol1", Path: filepath.Join("/volumes", "share")},
	}, nil, nil)
	const volumeID = uint16(1)

	paths := []string{
		filepath.Join("/volumes", "share"),
		filepath.Join("/volumes", "share", "docs"),
		filepath.Join("/volumes", "share", "docs", "2024"),
		filepath.Join("/volumes", "share", "music"),
	}

	// Assign DIDs and verify round-trip via getDIDPath.
	for _, p := range paths {
		did := s.getPathDID(volumeID, p)
		if p == filepath.Join("/volumes", "share") {
			if did != CNIDRoot {
				t.Errorf("root path %q: DID %d, want %d", p, did, CNIDRoot)
			}
			continue
		}
		if did <= CNIDRoot {
			t.Errorf("path %q: DID %d is in reserved range", p, did)
		}
		got, ok := s.getDIDPath(volumeID, did)
		if !ok {
			t.Errorf("path %q: getDIDPath(%d) returned not-found", p, did)
		}
		if got != p {
			t.Errorf("path %q: round-trip mismatch, got %q", p, got)
		}
	}

	// Calling getPathDID again must return the same DID (idempotent).
	for _, p := range paths {
		id1 := s.getPathDID(volumeID, p)
		id2 := s.getPathDID(volumeID, p)
		if id1 != id2 {
			t.Errorf("path %q: DID not stable across calls (%d != %d)", p, id1, id2)
		}
	}

	// All assigned DIDs must be unique.
	ids := make(map[uint32]string)
	for _, p := range paths {
		id := s.getPathDID(volumeID, p)
		if prev, exists := ids[id]; exists {
			t.Errorf("DID collision between %q and %q: both got %d", prev, p, id)
		}
		ids[id] = p
	}
}

func TestGetPathDID_RenamePreservesCNID(t *testing.T) {
	root := t.TempDir()
	s := NewService("TestServer", []VolumeConfig{{Name: "Mac", Path: root}}, &LocalFileSystem{}, nil)
	const volumeID = uint16(1)

	oldPath := filepath.Join(root, "SimpleText")
	newPath := filepath.Join(root, "SimpleText Renamed")
	if err := os.WriteFile(oldPath, []byte("hello"), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	oldCNID := s.getPathDID(volumeID, oldPath)
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatalf("rename file: %v", err)
	}
	s.rebindDIDSubtree(volumeID, oldPath, newPath)

	newCNID := s.getPathDID(volumeID, newPath)
	if newCNID != oldCNID {
		t.Fatalf("CNID changed across rename: old=%d new=%d", oldCNID, newCNID)
	}
	resolved, ok := s.getDIDPath(volumeID, oldCNID)
	if !ok || resolved != newPath {
		t.Fatalf("CNID lookup after rename = %q, %t, want %q", resolved, ok, newPath)
	}
}

func TestAFP_ByteRangeLock_TrashUsageMapInitFlow(t *testing.T) {
	root := t.TempDir()
	const (
		volName     = "Mac"
		trashName   = "Network Trash Folder"
		usageMap    = "Trash Can Usage Map"
		pathTypeAFP = 2 // long names
	)

	s := NewService("TestServer", []VolumeConfig{{Name: volName, Path: root}}, &LocalFileSystem{}, nil)

	if _, errCode := s.handleOpenVol(&FPOpenVolReq{Bitmap: VolBitmapVolID, VolName: volName}); errCode != NoErr {
		t.Fatalf("OpenVol failed: got %d", errCode)
	}

	trashRes, errCode := s.handleCreateDir(&FPCreateDirReq{
		VolumeID: 1,
		DirID:    2, // root DID
		PathType: pathTypeAFP,
		Path:     trashName,
	})
	if errCode != NoErr {
		t.Fatalf("CreateDir(%q) failed: got %d", trashName, errCode)
	}

	if _, errCode = s.handleCreateFile(&FPCreateFileReq{
		CreateFlag: FPCreateFileFlagHardCreate, // hard create
		VolumeID:   1,
		DirID:      trashRes.DirID,
		PathType:   pathTypeAFP,
		Path:       usageMap,
	}); errCode != NoErr {
		t.Fatalf("CreateFile(%q) failed: got %d", usageMap, errCode)
	}

	usageMapPath := filepath.Join(root, trashName, usageMap)
	if err := os.WriteFile(usageMapPath, make([]byte, 8), 0644); err != nil {
		t.Fatalf("seed usage map file: %v", err)
	}

	openForkRes, errCode := s.handleOpenFork(&FPOpenForkReq{
		Fork:       ForkData,
		VolumeID:   1,
		DirID:      trashRes.DirID,
		Bitmap:     0,
		AccessMode: 0x03, // read + write
		PathType:   pathTypeAFP,
		Path:       usageMap,
	})
	if errCode != NoErr {
		t.Fatalf("OpenFork(%q) failed: got %d", usageMap, errCode)
	}
	t.Cleanup(func() {
		_, _ = s.handleCloseFork(&FPCloseForkReq{OForkRefNum: openForkRes.ForkID})
	})

	// Mirror the Netatalk flow: walk usage-map bytes until a lock succeeds,
	// then create the matching "Trash Can #N" directory.
	index := int64(1)
	var lockedOffset int64
	for {
		index++
		lockRes, lockErr := s.handleByteRangeLock(&FPByteRangeLockReq{
			ForkID: openForkRes.ForkID,
			Offset: index,
			Length: 1,
		})
		if lockErr != NoErr {
			continue
		}
		lockedOffset = lockRes.Offset

		trashCanName := fmt.Sprintf("Trash Can #%d", index)
		if _, createErr := s.handleCreateDir(&FPCreateDirReq{
			VolumeID: 1,
			DirID:    trashRes.DirID,
			PathType: pathTypeAFP,
			Path:     trashCanName,
		}); createErr == NoErr {
			break
		}

		if _, unlockErr := s.handleByteRangeLock(&FPByteRangeLockReq{
			ForkID: openForkRes.ForkID,
			Unlock: true,
			Offset: index,
			Length: 1,
		}); unlockErr != NoErr {
			t.Fatalf("unlock on failed Trash Can create failed: got %d", unlockErr)
		}
	}

	if lockedOffset != 2 {
		t.Fatalf("expected first successful trash slot lock at offset 2, got %d", lockedOffset)
	}

	if _, errCode = s.handleByteRangeLock(&FPByteRangeLockReq{
		ForkID:  openForkRes.ForkID,
		FromEnd: true,
		Offset:  -1,
		Length:  1,
	}); errCode != NoErr {
		t.Fatalf("FromEnd lock failed: got %d", errCode)
	}

	if _, errCode = s.handleByteRangeLock(&FPByteRangeLockReq{
		ForkID: openForkRes.ForkID,
		Offset: -1,
		Length: 1,
	}); errCode != ErrParamErr {
		t.Fatalf("expected ErrParamErr for negative start-relative offset, got %d", errCode)
	}

	if _, errCode = s.handleByteRangeLock(&FPByteRangeLockReq{
		ForkID: openForkRes.ForkID,
		Unlock: true,
		Offset: 2,
		Length: 1,
	}); errCode != NoErr {
		t.Fatalf("unlock usage-map byte failed: got %d", errCode)
	}
}

func TestAFP_ByteRangeLock_ErrorSemantics(t *testing.T) {
	root := t.TempDir()
	s := NewService("TestServer", []VolumeConfig{{Name: "Mac", Path: root}}, &LocalFileSystem{}, nil)

	if _, errCode := s.handleOpenVol(&FPOpenVolReq{Bitmap: VolBitmapVolID, VolName: "Mac"}); errCode != NoErr {
		t.Fatalf("OpenVol failed: got %d", errCode)
	}

	if _, errCode := s.handleCreateFile(&FPCreateFileReq{
		CreateFlag: FPCreateFileFlagHardCreate,
		VolumeID:   1,
		DirID:      2,
		PathType:   2,
		Path:       "usage.map",
	}); errCode != NoErr {
		t.Fatalf("CreateFile failed: got %d", errCode)
	}
	if err := os.WriteFile(filepath.Join(root, "usage.map"), make([]byte, 16), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	f1, errCode := s.handleOpenFork(&FPOpenForkReq{
		Fork:       ForkData,
		VolumeID:   1,
		DirID:      2,
		AccessMode: 0x03,
		PathType:   2,
		Path:       "usage.map",
	})
	if errCode != NoErr {
		t.Fatalf("OpenFork f1 failed: got %d", errCode)
	}
	f2, errCode := s.handleOpenFork(&FPOpenForkReq{
		Fork:       ForkData,
		VolumeID:   1,
		DirID:      2,
		AccessMode: 0x03,
		PathType:   2,
		Path:       "usage.map",
	})
	if errCode != NoErr {
		t.Fatalf("OpenFork f2 failed: got %d", errCode)
	}
	t.Cleanup(func() {
		_, _ = s.handleCloseFork(&FPCloseForkReq{OForkRefNum: f1.ForkID})
		_, _ = s.handleCloseFork(&FPCloseForkReq{OForkRefNum: f2.ForkID})
	})

	if _, errCode = s.handleByteRangeLock(&FPByteRangeLockReq{ForkID: f1.ForkID, Offset: 5, Length: 1}); errCode != NoErr {
		t.Fatalf("initial lock failed: got %d", errCode)
	}

	if _, errCode = s.handleByteRangeLock(&FPByteRangeLockReq{ForkID: f1.ForkID, Offset: 5, Length: 1}); errCode != ErrRangeOverlap {
		t.Fatalf("expected ErrRangeOverlap=%d, got %d", ErrRangeOverlap, errCode)
	}

	if _, errCode = s.handleByteRangeLock(&FPByteRangeLockReq{ForkID: f2.ForkID, Offset: 5, Length: 1}); errCode != ErrLockErr {
		t.Fatalf("expected ErrLockErr=%d, got %d", ErrLockErr, errCode)
	}

	if _, errCode = s.handleByteRangeLock(&FPByteRangeLockReq{ForkID: f2.ForkID, Unlock: true, Offset: 5, Length: 1}); errCode != ErrRangeNotLocked {
		t.Fatalf("expected ErrRangeNotLocked=%d for foreign unlock, got %d", ErrRangeNotLocked, errCode)
	}

	if _, errCode = s.handleByteRangeLock(&FPByteRangeLockReq{ForkID: f1.ForkID, Unlock: true, Offset: 6, Length: 1}); errCode != ErrRangeNotLocked {
		t.Fatalf("expected ErrRangeNotLocked=%d for missing range unlock, got %d", ErrRangeNotLocked, errCode)
	}

	if _, errCode = s.handleByteRangeLock(&FPByteRangeLockReq{ForkID: f1.ForkID, Unlock: true, Offset: 5, Length: 1}); errCode != NoErr {
		t.Fatalf("owner unlock failed: got %d", errCode)
	}
}

func TestAFP_ByteRangeLock_NoMoreLocks(t *testing.T) {
	root := t.TempDir()
	s := NewService("TestServer", []VolumeConfig{{Name: "Mac", Path: root}}, &LocalFileSystem{}, nil)
	s.maxLocks = 1

	if _, errCode := s.handleOpenVol(&FPOpenVolReq{Bitmap: VolBitmapVolID, VolName: "Mac"}); errCode != NoErr {
		t.Fatalf("OpenVol failed: got %d", errCode)
	}

	if _, errCode := s.handleCreateFile(&FPCreateFileReq{
		CreateFlag: FPCreateFileFlagHardCreate,
		VolumeID:   1,
		DirID:      2,
		PathType:   2,
		Path:       "usage.map",
	}); errCode != NoErr {
		t.Fatalf("CreateFile failed: got %d", errCode)
	}
	if err := os.WriteFile(filepath.Join(root, "usage.map"), make([]byte, 16), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	f1, errCode := s.handleOpenFork(&FPOpenForkReq{
		Fork:       ForkData,
		VolumeID:   1,
		DirID:      2,
		AccessMode: 0x03,
		PathType:   2,
		Path:       "usage.map",
	})
	if errCode != NoErr {
		t.Fatalf("OpenFork failed: got %d", errCode)
	}
	t.Cleanup(func() {
		_, _ = s.handleCloseFork(&FPCloseForkReq{OForkRefNum: f1.ForkID})
	})

	if _, errCode = s.handleByteRangeLock(&FPByteRangeLockReq{ForkID: f1.ForkID, Offset: 1, Length: 1}); errCode != NoErr {
		t.Fatalf("first lock failed: got %d", errCode)
	}

	if _, errCode = s.handleByteRangeLock(&FPByteRangeLockReq{ForkID: f1.ForkID, Offset: 3, Length: 1}); errCode != ErrNoMoreLocks {
		t.Fatalf("expected ErrNoMoreLocks=%d, got %d", ErrNoMoreLocks, errCode)
	}
}
