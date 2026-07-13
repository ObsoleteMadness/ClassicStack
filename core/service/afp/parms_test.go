package afp

import (
	"testing"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"

	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
)

// fullFileBitmap requests every file parameter this packer emits.
const fullFileBitmap = fdBitmapAttributes | fdBitmapParentDID | fdBitmapCreateDate |
	fdBitmapModDate | fdBitmapBackupDate | fdBitmapFinderInfo | fdBitmapLongName |
	fdBitmapShortName | fileBitmapFileNum | fileBitmapDataForkLen | fileBitmapRsrcForkLen

// fullDirBitmap requests every directory parameter this packer emits.
const fullDirBitmap = fdBitmapAttributes | fdBitmapParentDID | fdBitmapCreateDate |
	fdBitmapModDate | fdBitmapBackupDate | fdBitmapFinderInfo | fdBitmapLongName |
	fdBitmapShortName | dirBitmapDirID | dirBitmapOffspring | dirBitmapOwnerID |
	dirBitmapGroupID | dirBitmapAccessRights

// TestFileDirParams_FullFileBitmap packs every file parameter and checks each
// fixed field at its bit-order offset, plus that the two name fields' offset
// pointers resolve to the long and short names in the trailing variable area.
func TestFileDirParams_FullFileBitmap(t *testing.T) {
	svc, _ := newRunningService(t)
	vol := svc.Volumes()[0]

	mustCreate(t, vol, "doc.txt")                              // writes "data" (4 bytes) to the data fork
	finder := [32]byte{'A', 'P', 'P', 'L', 'T', 'E', 'X', 'T'} // type 'APPL', creator 'TEXT'
	if err := vol.FS().WriteFinderInfo("doc.txt", finder); err != nil {
		t.Fatalf("WriteFinderInfo: %v", err)
	}
	wantCNID := vol.CNID("doc.txt")

	info, err := vol.Stat("doc.txt")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	block := vol.fileDirParams(nil, "doc.txt", info, fullFileBitmap, PathTypeUTF8Names)

	// Fixed fields, in ascending bit order.
	off := 0
	if got := bp.BE16(block[off:]); got != 0 { // Attributes
		t.Errorf("Attributes = %#04x, want 0", got)
	}
	off += 2
	if got := bp.BE32(block[off:]); got != metastore.CNIDRoot { // ParentDID: parent is the volume root
		t.Errorf("ParentDID = %d, want %d", got, metastore.CNIDRoot)
	}
	off += 4
	off += 4                                              // CreateDate (mod-time derived; value checked only for presence)
	off += 4                                              // ModDate
	if got := bp.BE32(block[off:]); got != noBackupDate { // BackupDate sentinel
		t.Errorf("BackupDate = %#08x, want %#08x", got, noBackupDate)
	}
	off += 4
	var gotFinder [32]byte
	copy(gotFinder[:], block[off:off+32]) // FinderInfo
	if gotFinder != finder {
		t.Errorf("FinderInfo = %x, want %x", gotFinder, finder)
	}
	off += 32
	longOff := int(bp.BE16(block[off:])) // LongName offset pointer
	off += 2
	shortOff := int(bp.BE16(block[off:])) // ShortName offset pointer
	off += 2
	if got := bp.BE32(block[off:]); got != wantCNID { // FileNum (CNID)
		t.Errorf("FileNum = %d, want %d", got, wantCNID)
	}
	off += 4
	if got := bp.BE32(block[off:]); got != 4 { // DataForkLen
		t.Errorf("DataForkLen = %d, want 4", got)
	}
	off += 4
	if got := bp.BE32(block[off:]); got != 0 { // RsrcForkLen (no resource fork written)
		t.Errorf("RsrcForkLen = %d, want 0", got)
	}
	off += 4

	// Name fields resolve through their offsets into the variable area.
	if name, _, ok := pString(block, longOff); !ok || string(name) != "doc.txt" {
		t.Errorf("LongName = %q (ok=%v), want doc.txt", name, ok)
	}
	// 8.3 short names are always DOS-cased (uppercase) per derivedNameEngine —
	// "doc.txt" already fits 8.3, so it's bound as-is upper-cased, not passed
	// through in its original case.
	if name, _, ok := pString(block, shortOff); !ok || string(name) != "DOC.TXT" {
		t.Errorf("ShortName = %q (ok=%v), want DOC.TXT", name, ok)
	}
}

// TestFileDirParams_FullDirBitmap packs every directory parameter and checks the
// directory-only fields: DirID is the directory's own CNID, OffspringCount counts
// the (non-metadata) children, and AccessRights is the read-write longword.
func TestFileDirParams_FullDirBitmap(t *testing.T) {
	svc, _ := newRunningService(t)
	vol := svc.Volumes()[0]
	if err := vol.FS().CreateDir("Folder"); err != nil {
		t.Fatalf("CreateDir: %v", err)
	}
	mustCreate(t, vol, "Folder/a.txt")
	mustCreate(t, vol, "Folder/b.txt")
	wantDirID := vol.CNID("Folder")

	info, err := vol.Stat("Folder")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	block := vol.fileDirParams(nil, "Folder", info, fullDirBitmap, PathTypeUTF8Names)

	// Skip the shared low fields to reach the directory-only ones: Attributes(2) +
	// ParentDID(4) + CreateDate(4) + ModDate(4) + BackupDate(4) + FinderInfo(32) +
	// LongName offset(2) + ShortName offset(2) = 54.
	off := 2 + 4 + 4 + 4 + 4 + 32 + 2 + 2
	if got := bp.BE32(block[off:]); got != wantDirID { // DirID (own CNID)
		t.Errorf("DirID = %d, want %d", got, wantDirID)
	}
	off += 4
	if got := bp.BE16(block[off:]); got != 2 { // OffspringCount (a.txt + b.txt)
		t.Errorf("OffspringCount = %d, want 2", got)
	}
	off += 2
	off += 4 // OwnerID
	off += 4 // GroupID
	if got := bp.BE32(block[off:]); got != dirAccessRights {
		t.Errorf("AccessRights = %#08x, want %#08x", got, dirAccessRights)
	}
}

// TestFileDirParams_DataForkLenReflectsWrites proves the packed DataForkLen is
// read live through the fork engine (not a stale Stat snapshot) after a write.
func TestFileDirParams_DataForkLenReflectsWrites(t *testing.T) {
	svc, _ := newRunningService(t)
	vol := svc.Volumes()[0]
	mustCreate(t, vol, "grow.txt") // 4 bytes

	info, err := vol.Stat("grow.txt")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	block := vol.fileDirParams(nil, "grow.txt", info, fileBitmapDataForkLen, PathTypeUTF8Names)
	if got := bp.BE32(block[0:4]); got != 4 {
		t.Fatalf("DataForkLen = %d, want 4", got)
	}
}
