package fuse

import (
	"bytes"
	"errors"
	iofs "io/fs"
	"os"
	"testing"
	"time"

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
	if _, err := a.Getxattr("/doc", xattrAppleFinderInfo); !errors.Is(err, errNoAttr) {
		t.Errorf("Getxattr: got %v, want errNoAttr", err)
	}
}

type readAtRecorder struct {
	fs.ForkFS
	lastN int
}

type recordingFile struct {
	fs.File
	rec *readAtRecorder
}

func (f recordingFile) ReadAt(p []byte, off int64) (int, error) {
	f.rec.lastN = len(p)
	return f.File.ReadAt(p, off)
}

func (r *readAtRecorder) OpenFile(path string, flag int) (fs.File, error) {
	f, err := r.ForkFS.OpenFile(path, flag)
	if err != nil {
		return nil, err
	}
	return recordingFile{File: f, rec: r}, nil
}

func TestReadCapsToKnownSize(t *testing.T) {
	base, err := fs.BuildShare(fs.ShareSpec{Name: "Mem", FSType: "memfs"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	wf, err := base.CreateFile("hello.txt")
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	payload := []byte("0123456789") // 10 bytes
	if _, err := wf.WriteAt(payload, 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	_ = wf.Close()

	rec := &readAtRecorder{ForkFS: base}
	a := New(rec, Options{VolumeLabel: "Test"})
	fh, err := a.Open("/hello.txt", os.O_RDONLY)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	buf := make([]byte, 4096)
	n, err := a.Read("/hello.txt", buf, 0, fh)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if n != len(payload) {
		t.Fatalf("n=%d, want %d", n, len(payload))
	}
	if rec.lastN != len(payload) {
		t.Fatalf("ReadAt asked for %d bytes, want %d (FUSE 4KiB must cap to file size)", rec.lastN, len(payload))
	}
	_ = a.Release("/hello.txt", fh)
}

type wireHintFS struct {
	fs.ForkFS
	finderCalls int
	forkLenN    int
	opens       int
	closes      int
}

type wireHintInfo struct {
	iofs.FileInfo
	finder [32]byte
	rsrc   int64
}

func (w wireHintInfo) Sys() any                     { return w }
func (w wireHintInfo) ResourceForkLen() int64       { return w.rsrc }
func (w wireHintInfo) FinderInfo() ([32]byte, bool) { return w.finder, true }
func (w wireHintInfo) DOSAttrs() uint16             { return 0 }

type countingCloseFile struct {
	fs.File
	onClose func()
}

func (f countingCloseFile) Close() error {
	f.onClose()
	return f.File.Close()
}

func (w *wireHintFS) Stat(path string) (iofs.FileInfo, error) {
	fi, err := w.ForkFS.Stat(path)
	if err != nil {
		return nil, err
	}
	var info [32]byte
	copy(info[:], []byte("TEXTttxt"))
	info[8], info[9] = 0x40, 0x00 // fdFlagsInvisible
	n, _ := w.ForkFS.ForkLen(path, fs.ResourceFork)
	return wireHintInfo{FileInfo: fi, finder: info, rsrc: n}, nil
}

func (w *wireHintFS) ReadFinderInfo(path string) ([32]byte, bool, error) {
	w.finderCalls++
	return w.ForkFS.ReadFinderInfo(path)
}

func (w *wireHintFS) ForkLen(path string, fork fs.ForkType) (int64, error) {
	w.forkLenN++
	return w.ForkFS.ForkLen(path, fork)
}

func (w *wireHintFS) OpenFork(path string, fork fs.ForkType, flag int) (fs.File, error) {
	w.opens++
	f, err := w.ForkFS.OpenFork(path, fork, flag)
	if err != nil {
		return nil, err
	}
	return countingCloseFile{File: f, onClose: func() { w.closes++ }}, nil
}

func TestGetattrUsesWireFinderInfo(t *testing.T) {
	base, err := fs.BuildShare(fs.ShareSpec{
		Name: "Mem", FSType: "memfs", ForkBackend: "appledouble",
	}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	wf, err := base.CreateFile("doc")
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	_ = wf.Close()
	hints := &wireHintFS{ForkFS: base}
	a := New(hints, Options{VolumeLabel: "Test", NativeForks: true, Layout: XattrLayoutApple})
	st, err := a.Getattr("/doc", 0)
	if err != nil {
		t.Fatalf("Getattr: %v", err)
	}
	if st.Flags&ufHidden == 0 {
		t.Fatal("expected UF_HIDDEN from wire FinderInfo")
	}
	if hints.finderCalls != 0 {
		t.Fatalf("ReadFinderInfo called %d times, want 0 (use Stat Sys())", hints.finderCalls)
	}
}

func TestListxattrUsesWireHints(t *testing.T) {
	base, err := fs.BuildShare(fs.ShareSpec{
		Name: "Mem", FSType: "memfs", ForkBackend: "appledouble",
	}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	wf, err := base.CreateFile("doc")
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	_ = wf.Close()
	var info [32]byte
	copy(info[:], []byte("TEXTttxt"))
	if err := base.WriteFinderInfo("doc", info); err != nil {
		t.Fatalf("WriteFinderInfo: %v", err)
	}
	rf, err := base.OpenFork("doc", fs.ResourceFork, os.O_RDWR|os.O_CREATE)
	if err != nil {
		t.Fatalf("OpenFork: %v", err)
	}
	if _, err := rf.WriteAt([]byte("rsrc"), 0); err != nil {
		t.Fatalf("WriteAt rsrc: %v", err)
	}
	_ = rf.Close()

	hints := &wireHintFS{ForkFS: base}
	a := New(hints, Options{VolumeLabel: "Test", NativeForks: true, Layout: XattrLayoutApple})
	names, err := a.Listxattr("/doc")
	if err != nil {
		t.Fatalf("Listxattr: %v", err)
	}
	seen := map[string]bool{}
	for _, n := range names {
		seen[n] = true
	}
	if !seen[xattrAppleFinderInfo] || !seen[xattrAppleResourceFork] {
		t.Fatalf("Listxattr = %v, want FinderInfo+ResourceFork", names)
	}
	if hints.finderCalls != 0 || hints.forkLenN != 0 {
		t.Fatalf("extra wire calls finder=%d forkLen=%d, want 0/0", hints.finderCalls, hints.forkLenN)
	}
}

func TestGetxattrResourceOpensForkOnce(t *testing.T) {
	base, err := fs.BuildShare(fs.ShareSpec{
		Name: "Mem", FSType: "memfs", ForkBackend: "appledouble",
	}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	wf, err := base.CreateFile("doc")
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	_ = wf.Close()
	rf, err := base.OpenFork("doc", fs.ResourceFork, os.O_RDWR|os.O_CREATE)
	if err != nil {
		t.Fatalf("OpenFork: %v", err)
	}
	payload := []byte("resource-bytes")
	if _, err := rf.WriteAt(payload, 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	_ = rf.Close()

	hints := &wireHintFS{ForkFS: base}
	a := New(hints, Options{VolumeLabel: "Test", NativeForks: true, Layout: XattrLayoutApple})
	got, err := a.Getxattr("/doc", xattrAppleResourceFork)
	if err != nil {
		t.Fatalf("Getxattr: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("got %q, want %q", got, payload)
	}
	if hints.opens != 1 || hints.closes != 0 {
		t.Fatalf("open/close = %d/%d, want 1/0 (fork ref cached for sequential reads)", hints.opens, hints.closes)
	}
	if hints.forkLenN != 0 {
		t.Fatalf("ForkLen called %d times, want 0 (open/read/close, no length probe)", hints.forkLenN)
	}
	if err := a.Removexattr("/doc", xattrAppleResourceFork); err != nil {
		t.Fatalf("Removexattr: %v", err)
	}
	if hints.closes < 1 {
		t.Fatalf("closes = %d after Removexattr, want cached fork closed", hints.closes)
	}
}

func TestXattrSizeDoesNotOpenFork(t *testing.T) {
	base, err := fs.BuildShare(fs.ShareSpec{
		Name: "Mem", FSType: "memfs", ForkBackend: "appledouble",
	}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	wf, err := base.CreateFile("doc")
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	_ = wf.Close()
	rf, err := base.OpenFork("doc", fs.ResourceFork, os.O_RDWR|os.O_CREATE)
	if err != nil {
		t.Fatalf("OpenFork: %v", err)
	}
	if _, err := rf.WriteAt(bytes.Repeat([]byte("R"), 4096), 0); err != nil {
		t.Fatalf("WriteAt rsrc: %v", err)
	}
	_ = rf.Close()

	hints := &wireHintFS{ForkFS: base}
	a := New(hints, Options{VolumeLabel: "Test", NativeForks: true, Layout: XattrLayoutApple})
	n, err := a.XattrSize("/doc", xattrAppleResourceFork)
	if err != nil {
		t.Fatalf("XattrSize: %v", err)
	}
	if n != 4096 {
		t.Fatalf("XattrSize = %d, want 4096", n)
	}
	if hints.opens != 0 {
		t.Fatalf("OpenFork called %d times, want 0 (size probe uses Stat)", hints.opens)
	}
}

type rangeReadRec struct {
	fs.ForkFS
	wantN int
	off   int64
	opens int
}

type rangeReadFile struct {
	fs.File
	rec *rangeReadRec
}

func (f rangeReadFile) ReadAt(p []byte, off int64) (int, error) {
	f.rec.wantN = len(p)
	f.rec.off = off
	return f.File.ReadAt(p, off)
}

func (r *rangeReadRec) OpenFork(path string, fork fs.ForkType, flag int) (fs.File, error) {
	r.opens++
	f, err := r.ForkFS.OpenFork(path, fork, flag)
	if err != nil {
		return nil, err
	}
	return rangeReadFile{File: f, rec: r}, nil
}

func TestGetxattrRangeIsOffsetAndLength(t *testing.T) {
	base, err := fs.BuildShare(fs.ShareSpec{
		Name: "Mem", FSType: "memfs", ForkBackend: "appledouble",
	}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	wf, err := base.CreateFile("doc")
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	_ = wf.Close()
	rf, err := base.OpenFork("doc", fs.ResourceFork, os.O_RDWR|os.O_CREATE)
	if err != nil {
		t.Fatalf("OpenFork: %v", err)
	}
	payload := []byte("0123456789abcdef")
	if _, err := rf.WriteAt(payload, 0); err != nil {
		t.Fatalf("WriteAt rsrc: %v", err)
	}
	_ = rf.Close()

	rec := &rangeReadRec{ForkFS: base}
	a := New(rec, Options{VolumeLabel: "Test", NativeForks: true, Layout: XattrLayoutApple})
	got, err := a.GetxattrRange("/doc", xattrAppleResourceFork, 4, 6)
	if err != nil {
		t.Fatalf("GetxattrRange: %v", err)
	}
	if string(got) != "456789" {
		t.Fatalf("got %q, want 456789", got)
	}
	if rec.opens != 1 {
		t.Fatalf("opens = %d, want 1", rec.opens)
	}
	if rec.off != 4 || rec.wantN != 6 {
		t.Fatalf("ReadAt off=%d n=%d, want off=4 n=6 (not the whole fork)", rec.off, rec.wantN)
	}
}

func TestXattrForkCacheReusesOpenFork(t *testing.T) {
	base, err := fs.BuildShare(fs.ShareSpec{
		Name: "Mem", FSType: "memfs", ForkBackend: "appledouble",
	}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	wf, err := base.CreateFile("doc")
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	_ = wf.Close()
	rf, err := base.OpenFork("doc", fs.ResourceFork, os.O_RDWR|os.O_CREATE)
	if err != nil {
		t.Fatalf("OpenFork: %v", err)
	}
	payload := make([]byte, 256*1024)
	for i := range payload {
		payload[i] = byte(i)
	}
	if _, err := rf.WriteAt(payload, 0); err != nil {
		t.Fatalf("WriteAt rsrc: %v", err)
	}
	_ = rf.Close()

	rec := &rangeReadRec{ForkFS: base}
	a := New(rec, Options{VolumeLabel: "Test", NativeForks: true, Layout: XattrLayoutApple})

	const chunk = 128 * 1024
	for off := int64(0); off < int64(len(payload)); off += chunk {
		got, err := a.GetxattrRange("/doc", xattrAppleResourceFork, off, chunk)
		if err != nil {
			t.Fatalf("GetxattrRange off=%d: %v", off, err)
		}
		if len(got) != chunk {
			t.Fatalf("off=%d len=%d, want %d", off, len(got), chunk)
		}
	}
	if rec.opens != 1 {
		t.Fatalf("OpenFork called %d times across sequential 128KiB chunks, want 1", rec.opens)
	}
}

type slowReadRec struct {
	fs.ForkFS
	opens int
}

type slowReadFile struct {
	fs.File
	rec *slowReadRec
}

func (f slowReadFile) ReadAt(p []byte, off int64) (int, error) {
	time.Sleep(20 * time.Millisecond)
	return f.File.ReadAt(p, off)
}

func (r *slowReadRec) OpenFork(path string, fork fs.ForkType, flag int) (fs.File, error) {
	r.opens++
	f, err := r.ForkFS.OpenFork(path, fork, flag)
	if err != nil {
		return nil, err
	}
	return slowReadFile{File: f, rec: r}, nil
}

// TestXattrForkCacheSurvivesSlowChunks verifies the idle timer is refreshed after
// each getxattr chunk completes, not when it starts — a remote AFP read of 128 KiB
// can take many seconds without forcing FPOpenFork on the next slice.
func TestXattrForkCacheSurvivesSlowChunks(t *testing.T) {
	base, err := fs.BuildShare(fs.ShareSpec{
		Name: "Mem", FSType: "memfs", ForkBackend: "appledouble",
	}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	wf, err := base.CreateFile("doc")
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	_ = wf.Close()
	rf, err := base.OpenFork("doc", fs.ResourceFork, os.O_RDWR|os.O_CREATE)
	if err != nil {
		t.Fatalf("OpenFork: %v", err)
	}
	payload := make([]byte, 512*1024)
	if _, err := rf.WriteAt(payload, 0); err != nil {
		t.Fatalf("WriteAt rsrc: %v", err)
	}
	_ = rf.Close()

	rec := &slowReadRec{ForkFS: base}
	a := New(rec, Options{VolumeLabel: "Test", NativeForks: true, Layout: XattrLayoutApple})

	const chunk = 128 * 1024
	for off := int64(0); off < int64(len(payload)); off += chunk {
		got, err := a.GetxattrRange("/doc", xattrAppleResourceFork, off, chunk)
		if err != nil {
			t.Fatalf("GetxattrRange off=%d: %v", off, err)
		}
		if len(got) != chunk {
			t.Fatalf("off=%d len=%d, want %d", off, len(got), chunk)
		}
	}
	if rec.opens != 1 {
		t.Fatalf("OpenFork called %d times across slow sequential chunks, want 1", rec.opens)
	}
}
