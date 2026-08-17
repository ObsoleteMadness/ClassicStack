package fuse

import (
	"bytes"
	"os"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

func newTestAdapter(t *testing.T, native bool, layout XattrLayout) *Adapter {
	t.Helper()
	forkFS, err := fs.BuildShare(fs.ShareSpec{
		Name:        "Test",
		FSType:      "memfs",
		ForkBackend: "appledouble",
	}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	return New(forkFS, Options{VolumeLabel: "Test", NativeForks: native, Layout: layout})
}

func TestCreateWriteReadStat(t *testing.T) {
	a := newTestAdapter(t, false, XattrLayoutApple)

	fh, err := a.Create("/hello.txt", os.O_RDWR, 0o644)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	payload := []byte("Hello, ClassicStack!")
	n, err := a.Write("/hello.txt", payload, 0, fh)
	if err != nil || n != len(payload) {
		t.Fatalf("Write n=%d err=%v", n, err)
	}
	buf := make([]byte, len(payload))
	rn, err := a.Read("/hello.txt", buf, 0, fh)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(buf[:rn], payload) {
		t.Errorf("Read got %q, want %q", buf[:rn], payload)
	}
	if err := a.Release("/hello.txt", fh); err != nil {
		t.Fatalf("Release: %v", err)
	}

	st, err := a.Getattr("/hello.txt", 0)
	if err != nil {
		t.Fatalf("Getattr: %v", err)
	}
	if st.IsDir {
		t.Error("file marked as directory")
	}
	if st.Size != int64(len(payload)) {
		t.Errorf("size=%d, want %d", st.Size, len(payload))
	}
}

func TestMkdirReaddirRenameUnlink(t *testing.T) {
	a := newTestAdapter(t, false, XattrLayoutApple)

	if err := a.Mkdir("/dir", 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	fh, err := a.Create("/dir/a.txt", os.O_RDWR, 0o644)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_ = a.Release("/dir/a.txt", fh)

	ents, err := a.Readdir("/dir", 0)
	if err != nil {
		t.Fatalf("Readdir: %v", err)
	}
	seen := map[string]bool{}
	for _, e := range ents {
		seen[e.Name] = true
	}
	if !seen["."] || !seen[".."] || !seen["a.txt"] {
		t.Errorf("listing missing entries: %v", seen)
	}

	if err := a.Rename("/dir/a.txt", "/dir/b.txt"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if _, err := a.fsys.Stat("dir/a.txt"); err == nil {
		t.Error("a.txt still present after rename")
	}
	if err := a.Unlink("/dir/b.txt"); err != nil {
		t.Fatalf("Unlink: %v", err)
	}
	if _, err := a.fsys.Stat("dir/b.txt"); err == nil {
		t.Error("b.txt still present after unlink")
	}
}

func TestSidecarModeHidesNativeXattrs(t *testing.T) {
	a := newTestAdapter(t, false, XattrLayoutApple)
	fh, err := a.Create("/doc", os.O_RDWR, 0o644)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_ = a.Release("/doc", fh)

	var info [32]byte
	copy(info[:], []byte("TEXTttxt"))
	if err := a.fsys.WriteFinderInfo("doc", info); err != nil {
		t.Fatalf("WriteFinderInfo: %v", err)
	}
	names, err := a.Listxattr("/doc")
	if err != nil {
		t.Fatalf("Listxattr: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("sidecar mode advertised xattrs: %v", names)
	}
	if _, err := a.Getxattr("/doc", xattrAppleFinderInfo); err != errNoAttr {
		t.Errorf("Getxattr: got %v, want errNoAttr", err)
	}
}
