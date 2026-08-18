package xfer

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// buildShare makes an in-memory ForkFS with the default AppleDouble fork adapter, so
// resource forks and Finder info are carried as sidecars — exactly the layering
// client.Connect gives an SMB/NCP/EtherDFS remote.
func buildShare(t *testing.T) fs.ForkFS {
	t.Helper()
	sh, err := fs.BuildShare(fs.ShareSpec{FSType: "memfs"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	return sh
}

func writeData(t *testing.T, sh fs.ForkFS, path string, data []byte) {
	t.Helper()
	f, err := sh.CreateFile(path)
	if err != nil {
		t.Fatalf("CreateFile %s: %v", path, err)
	}
	if _, err := f.WriteAt(data, 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func writeRsrc(t *testing.T, sh fs.ForkFS, path string, data []byte) {
	t.Helper()
	f, err := sh.OpenFork(path, fs.ResourceFork, os.O_RDWR|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		t.Fatalf("OpenFork rsrc %s: %v", path, err)
	}
	if _, err := f.WriteAt(data, 0); err != nil {
		t.Fatalf("WriteAt rsrc: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close rsrc: %v", err)
	}
}

func readAll(t *testing.T, sh fs.ForkFS, path string) []byte {
	t.Helper()
	f, err := sh.OpenFile(path, os.O_RDONLY)
	if err != nil {
		t.Fatalf("OpenFile %s: %v", path, err)
	}
	defer f.Close()
	info, _ := f.Stat()
	buf := make([]byte, info.Size())
	if len(buf) > 0 {
		if _, err := f.ReadAt(buf, 0); err != nil && err.Error() != "EOF" {
			// io.EOF at the exact end is fine
		}
	}
	return buf
}

func readFork(t *testing.T, sh fs.ForkFS, path string, fork fs.ForkType) []byte {
	t.Helper()
	f, err := sh.OpenFork(path, fork, os.O_RDONLY)
	if err != nil {
		t.Fatalf("OpenFork %s: %v", path, err)
	}
	defer f.Close()
	n, _ := sh.ForkLen(path, fork)
	buf := make([]byte, n)
	if n > 0 {
		_, _ = f.ReadAt(buf, 0)
	}
	return buf
}

// TestCopyFilePreservesForksAndMeta is the core guarantee: a file's data fork,
// resource fork, Finder type/creator, and DOS attributes all survive a Copy between
// two independent shares — the same generic path used remote↔host.
func TestCopyFilePreservesForksAndMeta(t *testing.T) {
	src := buildShare(t)
	dst := buildShare(t)

	data := []byte("hello data fork")
	rsrc := []byte("RESOURCE FORK BYTES")
	writeData(t, src, "file.txt", data)
	writeRsrc(t, src, "file.txt", rsrc)

	var fi [32]byte
	copy(fi[0:4], "TEXT")
	copy(fi[4:8], "ttxt")
	if err := src.WriteFinderInfo("file.txt", fi); err != nil {
		t.Fatalf("WriteFinderInfo: %v", err)
	}
	if err := src.Meta().SetAttrs("file.txt", fs.DOSAttr{Attrs: fs.DOSReadOnly | fs.DOSHidden}); err != nil {
		t.Fatalf("SetAttrs: %v", err)
	}

	if err := Copy(src, dst, "file.txt", "copied.txt"); err != nil {
		t.Fatalf("Copy: %v", err)
	}

	if got := readAll(t, dst, "copied.txt"); !bytes.Equal(got, data) {
		t.Errorf("data fork = %q, want %q", got, data)
	}
	if got := readFork(t, dst, "copied.txt", fs.ResourceFork); !bytes.Equal(got, rsrc) {
		t.Errorf("resource fork = %q, want %q", got, rsrc)
	}
	gotFI, ok, err := dst.ReadFinderInfo("copied.txt")
	if err != nil || !ok {
		t.Fatalf("ReadFinderInfo dst: ok=%v err=%v", ok, err)
	}
	if !bytes.Equal(gotFI[0:8], fi[0:8]) {
		t.Errorf("Finder info type/creator = %q, want %q", gotFI[0:8], fi[0:8])
	}
	attr, ok := dst.Meta().Attrs("copied.txt")
	if !ok {
		t.Fatalf("dst Attrs missing")
	}
	if attr.Attrs&(fs.DOSReadOnly|fs.DOSHidden) != (fs.DOSReadOnly | fs.DOSHidden) {
		t.Errorf("DOS attrs = %#x, want RO|HID set", attr.Attrs)
	}
}

func TestCopyDirRecursive(t *testing.T) {
	src := buildShare(t)
	dst := buildShare(t)

	if err := src.CreateDir("d"); err != nil {
		t.Fatal(err)
	}
	if err := src.CreateDir("d/sub"); err != nil {
		t.Fatal(err)
	}
	writeData(t, src, "d/a.txt", []byte("A"))
	writeData(t, src, "d/sub/b.txt", []byte("BB"))

	if err := Copy(src, dst, "d", "d2"); err != nil {
		t.Fatalf("Copy dir: %v", err)
	}
	if got := readAll(t, dst, "d2/a.txt"); string(got) != "A" {
		t.Errorf("d2/a.txt = %q", got)
	}
	if got := readAll(t, dst, "d2/sub/b.txt"); string(got) != "BB" {
		t.Errorf("d2/sub/b.txt = %q", got)
	}
}

func TestListReportsTypeCreator(t *testing.T) {
	sh := buildShare(t)
	writeData(t, sh, "doc", []byte("x"))
	var fi [32]byte
	copy(fi[0:4], "APPL")
	copy(fi[4:8], "MACS")
	_ = sh.WriteFinderInfo("doc", fi)

	entries, err := List(sh, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var found bool
	for _, e := range entries {
		if e.Name == "doc" {
			found = true
			if e.Type != "APPL" || e.Creator != "MACS" {
				t.Errorf("type/creator = %q/%q, want APPL/MACS", e.Type, e.Creator)
			}
		}
	}
	if !found {
		t.Errorf("doc not listed; entries=%+v", entries)
	}
}

func TestSetAttrToggles(t *testing.T) {
	sh := buildShare(t)
	writeData(t, sh, "f", []byte("x"))
	if err := SetAttr(sh, "f", fs.DOSReadOnly, 0); err != nil {
		t.Fatal(err)
	}
	if attr, _ := sh.Meta().Attrs("f"); attr.Attrs&fs.DOSReadOnly == 0 {
		t.Errorf("RO not set")
	}
	if err := SetAttr(sh, "f", 0, fs.DOSReadOnly); err != nil {
		t.Fatal(err)
	}
	if attr, _ := sh.Meta().Attrs("f"); attr.Attrs&fs.DOSReadOnly != 0 {
		t.Errorf("RO not cleared")
	}
}

func TestCopyCtxReportsProgressAndCancel(t *testing.T) {
	src := buildShare(t)
	dst := buildShare(t)
	payload := bytes.Repeat([]byte("x"), 200_000)
	writeData(t, src, "big.bin", payload)

	var last Progress
	if err := CopyCtx(context.Background(), src, dst, "big.bin", "out.bin", func(p Progress) {
		last = p
	}); err != nil {
		t.Fatalf("CopyCtx: %v", err)
	}
	if last.BytesDone < int64(len(payload)) {
		t.Fatalf("bytesDone %d want >= %d", last.BytesDone, len(payload))
	}
	if last.BytesTotal < int64(len(payload)) {
		t.Fatalf("bytesTotal %d", last.BytesTotal)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := CopyCtx(ctx, src, dst, "big.bin", "cancelled.bin", nil); err == nil {
		t.Fatal("expected canceled copy")
	}
}
