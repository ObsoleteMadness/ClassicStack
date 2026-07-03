package afp

import (
	"testing"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// buildPathReq assembles a cmd(1) pad(1) volID(2) dirID(4) ... pathType(1)
// name(pascal) request, the common shape of the ported catalog commands.
func appendPascal(b []byte, s string) []byte {
	b = append(b, byte(len(s)))
	return append(b, []byte(s)...)
}

// TestGetFileParms proves FPGetFileParms returns a file's params and rejects a
// directory with kFPObjectNotFound (the file/dir kind guard).
func TestGetFileParms(t *testing.T) {
	svc, r := newRunningService(t)
	vol := svc.Volumes()[0]
	mustCreate(t, vol, "note.txt")
	if err := vol.FS().CreateDir("folder"); err != nil {
		t.Fatalf("CreateDir: %v", err)
	}
	sessID, volID := openVolForFork(t, svc, r)

	req := func(cmd byte, name string) []byte {
		b := []byte{cmd, 0}
		b = bp.AppendBE16(b, volID)
		b = bp.AppendBE32(b, 2) // root dirID
		b = bp.AppendBE16(b, fileBitmapDataForkLen)
		b = append(b, PathTypeUTF8Names)
		return appendPascal(b, name)
	}

	// A file resolves.
	code, _ := sendCmd(t, svc, r, sessID, 4, req(cmdGetFileParms, "note.txt"))
	if code != afpNoErr {
		t.Fatalf("GetFileParms(file) result = %d, want 0", code)
	}
	// A directory addressed as a file is not-found.
	code, _ = sendCmd(t, svc, r, sessID, 5, req(cmdGetFileParms, "folder"))
	if code != afpErrObjectNotFnd {
		t.Fatalf("GetFileParms(dir) result = %d, want %d", code, afpErrObjectNotFnd)
	}
	// A directory addressed as a directory resolves via FPGetDirParms.
	code, _ = sendCmd(t, svc, r, sessID, 6, req(cmdGetDirParms, "folder"))
	if code != afpNoErr {
		t.Fatalf("GetDirParms(dir) result = %d, want 0", code)
	}
	// And a file addressed as a directory is not-found.
	code, _ = sendCmd(t, svc, r, sessID, 7, req(cmdGetDirParms, "note.txt"))
	if code != afpErrObjectNotFnd {
		t.Fatalf("GetDirParms(file) result = %d, want %d", code, afpErrObjectNotFnd)
	}
}

// TestMoveAndRename moves a file into a subdirectory and renames it, then proves
// the object is gone from the root and present (by CNID) at the destination.
func TestMoveAndRename(t *testing.T) {
	svc, r := newRunningService(t)
	vol := svc.Volumes()[0]
	mustCreate(t, vol, "src.txt")
	if err := vol.FS().CreateDir("dst"); err != nil {
		t.Fatalf("CreateDir: %v", err)
	}
	dstDID := vol.CNID("dst")
	sessID, volID := openVolForFork(t, svc, r)

	// cmd pad volID srcDirID dstDirID srcType srcName dstType dstDirName newType newName
	b := []byte{cmdMoveAndRename, 0}
	b = bp.AppendBE16(b, volID)
	b = bp.AppendBE32(b, 2)      // srcDirID = root
	b = bp.AppendBE32(b, dstDID) // dstDirID = "dst"
	b = append(b, PathTypeUTF8Names)
	b = appendPascal(b, "src.txt")
	b = append(b, 0)        // dstPathType 0 → use dstDirID directly
	b = appendPascal(b, "") // dstDirName empty
	b = append(b, PathTypeUTF8Names)
	b = appendPascal(b, "moved.txt")

	code, _ := sendCmd(t, svc, r, sessID, 4, b)
	if code != afpNoErr {
		t.Fatalf("MoveAndRename result = %d, want 0", code)
	}
	if _, err := vol.Stat("src.txt"); err == nil {
		t.Fatal("source still present after move")
	}
	if _, err := vol.Stat("dst/moved.txt"); err != nil {
		t.Fatalf("moved file not at destination: %v", err)
	}
}

// TestCopyFile copies a file and proves the copy exists alongside the original,
// and that a second copy onto the same name is kFPObjectExists.
func TestCopyFile(t *testing.T) {
	svc, r := newRunningService(t)
	vol := svc.Volumes()[0]
	mustCreate(t, vol, "orig.txt")
	sessID, volID := openVolForFork(t, svc, r)

	// cmd pad srcVolID srcDirID dstVolID dstDirID srcType srcName [pad] dstType dstDirName newType newName
	build := func(newName string) []byte {
		b := []byte{cmdCopyFile, 0}
		b = bp.AppendBE16(b, volID) // srcVolID
		b = bp.AppendBE32(b, 2)     // srcDirID root
		b = bp.AppendBE16(b, volID) // dstVolID
		b = bp.AppendBE32(b, 2)     // dstDirID root
		b = append(b, PathTypeUTF8Names)
		b = appendPascal(b, "orig.txt")
		if len("orig.txt")%2 != 0 {
			b = append(b, 0) // word-align the second path type
		}
		b = append(b, 0)        // dstPathType 0 → dstDirID directly
		b = appendPascal(b, "") // dstDirName
		b = append(b, PathTypeUTF8Names)
		b = appendPascal(b, newName)
		return b
	}

	code, _ := sendCmd(t, svc, r, sessID, 4, build("copy.txt"))
	if code != afpNoErr {
		t.Fatalf("CopyFile result = %d, want 0", code)
	}
	if _, err := vol.Stat("copy.txt"); err != nil {
		t.Fatalf("copy not created: %v", err)
	}
	if _, err := vol.Stat("orig.txt"); err != nil {
		t.Fatalf("original gone after copy: %v", err)
	}
	// A copy over an existing name is kFPObjectExists.
	code, _ = sendCmd(t, svc, r, sessID, 5, build("copy.txt"))
	if code != afpErrObjectExists {
		t.Fatalf("CopyFile over existing result = %d, want %d", code, afpErrObjectExists)
	}
}

// TestExchangeFiles swaps two files and proves each name now holds the other's
// data (the Finder safe-save primitive).
func TestExchangeFiles(t *testing.T) {
	svc, r := newRunningService(t)
	vol := svc.Volumes()[0]
	// Seed two files with distinguishable contents.
	writeFile := func(name, data string) {
		f, err := vol.FS().CreateFile(name)
		if err != nil {
			t.Fatalf("CreateFile %q: %v", name, err)
		}
		_, _ = f.WriteAt([]byte(data), 0)
		_ = f.Sync()
		_ = f.Close()
	}
	writeFile("a.txt", "AAAA")
	writeFile("b.txt", "BBBBBB")
	sessID, volID := openVolForFork(t, svc, r)

	b := []byte{cmdExchangeFiles, 0}
	b = bp.AppendBE16(b, volID)
	b = bp.AppendBE32(b, 2) // srcDirID root
	b = bp.AppendBE32(b, 2) // dstDirID root
	b = append(b, PathTypeUTF8Names)
	b = appendPascal(b, "a.txt")
	if len("a.txt")%2 != 0 {
		b = append(b, 0)
	}
	b = append(b, PathTypeUTF8Names)
	b = appendPascal(b, "b.txt")

	code, _ := sendCmd(t, svc, r, sessID, 4, b)
	if code != afpNoErr {
		t.Fatalf("ExchangeFiles result = %d, want 0", code)
	}
	// After the swap, a.txt holds b's data and vice versa.
	if got := readAll(t, vol, "a.txt"); got != "BBBBBB" {
		t.Fatalf("a.txt after exchange = %q, want %q", got, "BBBBBB")
	}
	if got := readAll(t, vol, "b.txt"); got != "AAAA" {
		t.Fatalf("b.txt after exchange = %q, want %q", got, "AAAA")
	}
}

// readAll reads a data fork's whole contents through the FS seam.
func readAll(t *testing.T, vol *Volume, path string) string {
	t.Helper()
	n, err := vol.ForkLen(path, fs.DataFork)
	if err != nil {
		t.Fatalf("ForkLen %q: %v", path, err)
	}
	f, err := vol.FS().OpenFile(path, 0)
	if err != nil {
		t.Fatalf("OpenFile %q: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, n)
	_, _ = f.ReadAt(buf, 0)
	return string(buf)
}
