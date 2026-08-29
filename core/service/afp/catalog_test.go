package afp

import (
	"testing"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"

	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
)

// catalogPath builds a dirID-relative path-bearing request block: cmd(1) flag(1)
// volID(2) dirID(4) pathType(1) name... — the shape FPCreateFile/CreateDir/
// Delete/OpenDir share.
func catalogPath(cmd, flag uint8, volID uint16, dirID uint32, name string) []byte {
	b := []byte{cmd, flag}
	b = bp.AppendBE16(b, volID)
	b = bp.AppendBE32(b, dirID)
	b = append(b, PathTypeUTF8Names)
	b = putPString(b, []byte(name))
	return b
}

// TestCatalog_CreateFileSoftThenExists proves a soft FPCreateFile makes the file
// (visible to Stat) and a second soft create over it returns kFPObjectExists.
func TestCatalog_CreateFileSoftThenExists(t *testing.T) {
	svc, r := newRunningService(t)
	vol := svc.Volumes()[0]
	sessID, volID := openVolForFork(t, svc, r)

	code, _ := sendCmd(t, svc, r, sessID, 4, catalogPath(cmdCreateFile, 0, volID, metastore.CNIDRoot, "new.txt"))
	if code != afpNoErr {
		t.Fatalf("CreateFile result = %d, want 0", code)
	}
	if _, err := vol.Stat("new.txt"); err != nil {
		t.Fatalf("created file not present: %v", err)
	}

	// A second soft create over the existing file is rejected.
	code, _ = sendCmd(t, svc, r, sessID, 5, catalogPath(cmdCreateFile, 0, volID, metastore.CNIDRoot, "new.txt"))
	if code != afpErrObjectExists {
		t.Fatalf("soft re-create result = %d, want %d", code, afpErrObjectExists)
	}

	// A hard create over it succeeds (replace semantics).
	code, _ = sendCmd(t, svc, r, sessID, 6, catalogPath(cmdCreateFile, createFlagHard, volID, metastore.CNIDRoot, "new.txt"))
	if code != afpNoErr {
		t.Fatalf("hard re-create result = %d, want 0", code)
	}
}

// TestCatalog_CreateDirThenCreateFileInside proves FPCreateDir returns a usable
// directory id and FPCreateFile resolves a path relative to it.
func TestCatalog_CreateDirThenCreateFileInside(t *testing.T) {
	svc, r := newRunningService(t)
	vol := svc.Volumes()[0]
	sessID, volID := openVolForFork(t, svc, r)

	code, reply := sendCmd(t, svc, r, sessID, 4, catalogPath(cmdCreateDir, 0, volID, metastore.CNIDRoot, "Folder"))
	if code != afpNoErr {
		t.Fatalf("CreateDir result = %d, want 0", code)
	}
	dirID := bp.BE32(reply[0:4])
	if dirID == 0 || dirID == metastore.CNIDRoot {
		t.Fatalf("CreateDir returned dirID %d, want a fresh id", dirID)
	}
	if info, err := vol.Stat("Folder"); err != nil || !info.IsDir() {
		t.Fatalf("created dir not present as directory: info=%v err=%v", info, err)
	}

	// Create a file inside the new directory addressed by its dirID.
	code, _ = sendCmd(t, svc, r, sessID, 5, catalogPath(cmdCreateFile, 0, volID, dirID, "child.txt"))
	if code != afpNoErr {
		t.Fatalf("CreateFile in subdir result = %d, want 0", code)
	}
	if _, err := vol.Stat("Folder/child.txt"); err != nil {
		t.Fatalf("child file not at Folder/child.txt: %v", err)
	}
}

// TestCatalog_Delete proves FPDelete removes a file and that deleting a missing
// object reports kFPObjectNotFound while the volume root is refused.
func TestCatalog_Delete(t *testing.T) {
	svc, r := newRunningService(t)
	vol := svc.Volumes()[0]
	mustCreate(t, vol, "gone.txt")
	sessID, volID := openVolForFork(t, svc, r)

	code, _ := sendCmd(t, svc, r, sessID, 4, catalogPath(cmdDelete, 0, volID, metastore.CNIDRoot, "gone.txt"))
	if code != afpNoErr {
		t.Fatalf("Delete result = %d, want 0", code)
	}
	if _, err := vol.Stat("gone.txt"); err == nil {
		t.Fatal("file still present after Delete")
	}

	// Deleting it again → not found.
	code, _ = sendCmd(t, svc, r, sessID, 5, catalogPath(cmdDelete, 0, volID, metastore.CNIDRoot, "gone.txt"))
	if code != afpErrObjectNotFnd {
		t.Fatalf("re-delete result = %d, want %d", code, afpErrObjectNotFnd)
	}

	// Deleting the volume root (empty pathname) is refused.
	code, _ = sendCmd(t, svc, r, sessID, 6, catalogPath(cmdDelete, 0, volID, metastore.CNIDRoot, ""))
	if code != afpErrAccessDenied {
		t.Fatalf("delete-root result = %d, want %d", code, afpErrAccessDenied)
	}
}

// TestCatalog_Rename proves FPRename moves a leaf name in place, preserves the
// object's CNID, and rejects a rename onto an existing name.
func TestCatalog_Rename(t *testing.T) {
	svc, r := newRunningService(t)
	vol := svc.Volumes()[0]
	mustCreate(t, vol, "old.txt")
	sessID, volID := openVolForFork(t, svc, r)
	cnidBefore := vol.CNID("old.txt")

	rename := []byte{cmdRename, 0}
	rename = bp.AppendBE16(rename, volID)
	rename = bp.AppendBE32(rename, metastore.CNIDRoot)
	rename = append(rename, PathTypeUTF8Names)
	rename = putPString(rename, []byte("old.txt"))
	rename = append(rename, PathTypeUTF8Names)
	rename = putPString(rename, []byte("new.txt"))
	code, _ := sendCmd(t, svc, r, sessID, 4, rename)
	if code != afpNoErr {
		t.Fatalf("Rename result = %d, want 0", code)
	}
	if _, err := vol.Stat("new.txt"); err != nil {
		t.Fatalf("renamed file not at new.txt: %v", err)
	}
	if _, err := vol.Stat("old.txt"); err == nil {
		t.Fatal("old name still present after Rename")
	}
	if got := vol.CNID("new.txt"); got != cnidBefore {
		t.Fatalf("CNID after rename = %d, want %d (preserved)", got, cnidBefore)
	}

	// Renaming onto an existing name is rejected.
	mustCreate(t, vol, "taken.txt")
	rename2 := []byte{cmdRename, 0}
	rename2 = bp.AppendBE16(rename2, volID)
	rename2 = bp.AppendBE32(rename2, metastore.CNIDRoot)
	rename2 = append(rename2, PathTypeUTF8Names)
	rename2 = putPString(rename2, []byte("new.txt"))
	rename2 = append(rename2, PathTypeUTF8Names)
	rename2 = putPString(rename2, []byte("taken.txt"))
	code, _ = sendCmd(t, svc, r, sessID, 5, rename2)
	if code != afpErrObjectExists {
		t.Fatalf("rename-onto-existing result = %d, want %d", code, afpErrObjectExists)
	}
}

// TestCatalog_OpenDir proves FPOpenDir returns the directory's CNID and that
// opening a file (non-directory) reports kFPObjectTypeErr.
func TestCatalog_OpenDir(t *testing.T) {
	svc, r := newRunningService(t)
	vol := svc.Volumes()[0]
	if err := vol.FS().CreateDir("Docs"); err != nil {
		t.Fatalf("CreateDir: %v", err)
	}
	mustCreate(t, vol, "file.txt")
	sessID, volID := openVolForFork(t, svc, r)

	code, reply := sendCmd(t, svc, r, sessID, 4, catalogPath(cmdOpenDir, 0, volID, metastore.CNIDRoot, "Docs"))
	if code != afpNoErr {
		t.Fatalf("OpenDir result = %d, want 0", code)
	}
	if got := bp.BE32(reply[0:4]); got != vol.CNID("Docs") {
		t.Fatalf("OpenDir dirID = %d, want %d", got, vol.CNID("Docs"))
	}

	// Opening a file as a directory is a type error.
	code, _ = sendCmd(t, svc, r, sessID, 5, catalogPath(cmdOpenDir, 0, volID, metastore.CNIDRoot, "file.txt"))
	if code != afpErrObjectTypeErr {
		t.Fatalf("OpenDir on file result = %d, want %d", code, afpErrObjectTypeErr)
	}
}
