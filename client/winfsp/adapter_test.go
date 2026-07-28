//go:build windows

package winfsp

import (
	"bytes"
	"testing"

	winfsp "github.com/winfsp/go-winfsp"
	"golang.org/x/sys/windows"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// newTestAdapter builds an Adapter over an in-memory ForkFS (memfs + appledouble), so the
// delegate mapping can be exercised without the WinFsp kernel driver.
func newTestAdapter(t *testing.T) *Adapter {
	t.Helper()
	forkFS, err := fs.BuildShare(fs.ShareSpec{
		Name:        "Test",
		FSType:      "memfs",
		ForkBackend: "appledouble",
	}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	return New(forkFS, Options{VolumeLabel: "Test"})
}

// TestCreateWriteReadStat drives the core file lifecycle through the delegates.
func TestCreateWriteReadStat(t *testing.T) {
	a := newTestAdapter(t)

	// Create a file.
	var info winfsp.FSP_FSCTL_FILE_INFO
	fileCtx, err := a.Create(nil, "\\hello.txt", 0, windows.GENERIC_WRITE, 0, nil, 0, &info)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		t.Errorf("new file marked as directory: attrs=%#x", info.FileAttributes)
	}

	// Write to it.
	payload := []byte("Hello, ClassicStack!")
	var winfo winfsp.FSP_FSCTL_FILE_INFO
	n, err := a.Write(nil, fileCtx, payload, 0, false, false, &winfo)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(payload) {
		t.Fatalf("Write n=%d, want %d", n, len(payload))
	}
	if winfo.FileSize != uint64(len(payload)) {
		t.Errorf("post-write FileSize=%d, want %d", winfo.FileSize, len(payload))
	}

	// Read it back.
	buf := make([]byte, len(payload))
	rn, err := a.Read(nil, fileCtx, buf, 0)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(buf[:rn], payload) {
		t.Errorf("Read got %q, want %q", buf[:rn], payload)
	}
	a.Close(nil, fileCtx)

	// Re-open and GetFileInfo.
	var oinfo winfsp.FSP_FSCTL_FILE_INFO
	openCtx, err := a.Open(nil, "\\hello.txt", 0, windows.GENERIC_READ, &oinfo)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	var ginfo winfsp.FSP_FSCTL_FILE_INFO
	if err := a.GetFileInfo(nil, openCtx, &ginfo); err != nil {
		t.Fatalf("GetFileInfo: %v", err)
	}
	if ginfo.FileSize != uint64(len(payload)) {
		t.Errorf("GetFileInfo FileSize=%d, want %d", ginfo.FileSize, len(payload))
	}
	a.Close(nil, openCtx)
}

// TestReadDirectory lists a directory through the delegate + fill callback.
func TestReadDirectory(t *testing.T) {
	a := newTestAdapter(t)

	for _, name := range []string{"\\a.txt", "\\b.txt"} {
		var info winfsp.FSP_FSCTL_FILE_INFO
		ctx, err := a.Create(nil, name, 0, windows.GENERIC_WRITE, 0, nil, 0, &info)
		if err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
		a.Close(nil, ctx)
	}

	// Open the root directory.
	var dinfo winfsp.FSP_FSCTL_FILE_INFO
	dirCtx, err := a.Open(nil, "\\", fileDirectoryFile, windows.GENERIC_READ, &dinfo)
	if err != nil {
		t.Fatalf("Open root: %v", err)
	}
	if dinfo.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		t.Errorf("root not marked directory: attrs=%#x", dinfo.FileAttributes)
	}

	seen := map[string]bool{}
	err = a.ReadDirectory(nil, dirCtx, "", func(name string, _ *winfsp.FSP_FSCTL_FILE_INFO) (bool, error) {
		seen[name] = true
		return true, nil
	})
	if err != nil {
		t.Fatalf("ReadDirectory: %v", err)
	}
	if !seen["a.txt"] || !seen["b.txt"] {
		t.Errorf("directory listing missing entries: %v", seen)
	}
	a.Close(nil, dirCtx)
}

// TestRenameAndDelete drives Rename and the Cleanup(delete) path.
func TestRenameAndDelete(t *testing.T) {
	a := newTestAdapter(t)

	var info winfsp.FSP_FSCTL_FILE_INFO
	ctx, err := a.Create(nil, "\\old.txt", 0, windows.GENERIC_WRITE, 0, nil, 0, &info)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	a.Close(nil, ctx)

	if err := a.Rename(nil, 0, "\\old.txt", "\\new.txt", false); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if _, err := a.fsys.Stat("old.txt"); err == nil {
		t.Errorf("old.txt still present after rename")
	}
	if _, err := a.fsys.Stat("new.txt"); err != nil {
		t.Errorf("new.txt missing after rename: %v", err)
	}

	// Delete via Cleanup(delete).
	var oinfo winfsp.FSP_FSCTL_FILE_INFO
	delCtx, err := a.Open(nil, "\\new.txt", 0, windows.GENERIC_READ, &oinfo)
	if err != nil {
		t.Fatalf("Open new.txt: %v", err)
	}
	a.Cleanup(nil, delCtx, "\\new.txt", fspCleanupDelete)
	a.Close(nil, delCtx)
	if _, err := a.fsys.Stat("new.txt"); err == nil {
		t.Errorf("new.txt still present after delete")
	}
}

// TestRenameKeepsHandleUsable renames a file while its WinFsp handle is still open and then
// drives handle delegates against it, exactly as WinFsp does after a successful rename. It
// regresses the STATUS_INTERNAL_ERROR bug where Rename closed the fork and left the handle
// with a nil fs.File / stale path: GetFileInfo/Read must succeed on the NEW path afterward.
func TestRenameKeepsHandleUsable(t *testing.T) {
	a := newTestAdapter(t)

	// Create + write, then re-open read-write and keep that handle for the rename.
	var cinfo winfsp.FSP_FSCTL_FILE_INFO
	ctx, err := a.Create(nil, "\\old.txt", 0, windows.GENERIC_WRITE, 0, nil, 0, &cinfo)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	payload := []byte("survives a rename")
	var winfo winfsp.FSP_FSCTL_FILE_INFO
	if _, err := a.Write(nil, ctx, payload, 0, false, false, &winfo); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Rename with the handle held open (file != 0) — the path WinFsp actually drives.
	if err := a.Rename(nil, ctx, "\\old.txt", "\\new.txt", false); err != nil {
		t.Fatalf("Rename with held handle: %v", err)
	}

	// WinFsp now re-issues handle delegates against the SAME context; they must work and see
	// the new path, not a nil file.
	var ginfo winfsp.FSP_FSCTL_FILE_INFO
	if err := a.GetFileInfo(nil, ctx, &ginfo); err != nil {
		t.Fatalf("GetFileInfo after rename (regresses STATUS_INTERNAL_ERROR): %v", err)
	}
	if ginfo.FileSize != uint64(len(payload)) {
		t.Errorf("post-rename FileSize=%d, want %d", ginfo.FileSize, len(payload))
	}
	buf := make([]byte, len(payload))
	rn, err := a.Read(nil, ctx, buf, 0)
	if err != nil {
		t.Fatalf("Read after rename: %v", err)
	}
	if !bytes.Equal(buf[:rn], payload) {
		t.Errorf("Read after rename got %q, want %q", buf[:rn], payload)
	}
	a.Close(nil, ctx)

	if _, err := a.fsys.Stat("old.txt"); err == nil {
		t.Errorf("old.txt still present after rename")
	}
	if _, err := a.fsys.Stat("new.txt"); err != nil {
		t.Errorf("new.txt missing after rename: %v", err)
	}
}

// TestRenameCaseMismatchedSource regresses the real STATUS_INTERNAL_ERROR bug: WinFsp
// upper-cases the source name it passes to Rename (it derives it from the normalized
// FileName, not the case-preserved Open path). On a case-sensitive backend (memfs here, a
// real AFP server on the wire) renaming that upper-cased name fails with "not found". The
// Adapter must instead use the open handle's authoritative, correctly-cased source path.
func TestRenameCaseMismatchedSource(t *testing.T) {
	a := newTestAdapter(t)

	var cinfo winfsp.FSP_FSCTL_FILE_INFO
	ctx, err := a.Create(nil, "\\mixedCase.txt", 0, windows.GENERIC_WRITE, 0, nil, 0, &cinfo)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// WinFsp hands the source in a different case than the file was created with, but the
	// held handle (ctx) still knows the real name.
	if err := a.Rename(nil, ctx, "\\MIXEDCASE.TXT", "\\renamed.txt", false); err != nil {
		t.Fatalf("Rename with case-mismatched source (regresses kFPObjectNotFound -> STATUS_INTERNAL_ERROR): %v", err)
	}
	a.Close(nil, ctx)

	if _, err := a.fsys.Stat("mixedCase.txt"); err == nil {
		t.Errorf("mixedCase.txt still present after rename")
	}
	if _, err := a.fsys.Stat("renamed.txt"); err != nil {
		t.Errorf("renamed.txt missing after rename: %v", err)
	}
}

// TestStreamSuffixRejected confirms a ':stream' path is rejected rather than routed to a
// fork (the mount surfaces only the fork backend's namespace).
func TestStreamSuffixRejected(t *testing.T) {
	a := newTestAdapter(t)
	var info winfsp.FSP_FSCTL_FILE_INFO
	if _, err := a.Open(nil, "\\file.txt:AFP_Resource", 0, windows.GENERIC_READ, &info); err == nil {
		t.Error("stream suffix should be rejected, got nil error")
	}
}
