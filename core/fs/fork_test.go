package fs

import (
	"bytes"
	"os"
	"testing"
)

// newForkTestShare builds a memfs share with the real appledouble fork engine.
func newForkTestShare(t *testing.T) (ForkFS, FileSystem) {
	t.Helper()
	base := newMemFS(ShareSpec{})
	eng := newAppleDoubleForkEngine(base, netatalkSidecarPath)
	return &shareFS{
		FileSystem: base,
		ForkEngine: eng,
		codec:      NewMacRomanUTF8FilenameCodec(),
		names:      NewPassthroughNameEngine(),
	}, base
}

func TestForkEngine_FinderInfoRoundTrip(t *testing.T) {
	share, _ := newForkTestShare(t)
	if _, err := share.CreateFile("doc"); err != nil {
		t.Fatalf("CreateFile: %v", err)
	}

	var fi [32]byte
	copy(fi[:], []byte("TEXTttxt")) // type 'TEXT', creator 'ttxt'
	if err := share.WriteFinderInfo("doc", fi); err != nil {
		t.Fatalf("WriteFinderInfo: %v", err)
	}

	got, ok, err := share.ReadFinderInfo("doc")
	if err != nil || !ok {
		t.Fatalf("ReadFinderInfo ok=%v err=%v", ok, err)
	}
	if got != fi {
		t.Fatalf("FinderInfo = %x, want %x", got, fi)
	}
}

func TestForkEngine_ResourceForkRoundTrip(t *testing.T) {
	share, _ := newForkTestShare(t)
	if _, err := share.CreateFile("app"); err != nil {
		t.Fatalf("CreateFile: %v", err)
	}

	payload := []byte("RESOURCE-FORK-BYTES")
	rf, err := share.OpenFork("app", ResourceFork, os.O_RDWR|os.O_CREATE)
	if err != nil {
		t.Fatalf("OpenFork(create): %v", err)
	}
	if _, err := rf.WriteAt(payload, 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if err := rf.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	n, err := share.ForkLen("app", ResourceFork)
	if err != nil {
		t.Fatalf("ForkLen: %v", err)
	}
	if n != int64(len(payload)) {
		t.Fatalf("ForkLen = %d, want %d", n, len(payload))
	}

	rf2, err := share.OpenFork("app", ResourceFork, os.O_RDONLY)
	if err != nil {
		t.Fatalf("OpenFork(read): %v", err)
	}
	defer rf2.Close()
	buf := make([]byte, len(payload))
	if _, err := rf2.ReadAt(buf, 0); err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	if !bytes.Equal(buf, payload) {
		t.Fatalf("resource fork = %q, want %q", buf, payload)
	}
}

func TestForkEngine_FinderInfoAndResourceCoexist(t *testing.T) {
	share, _ := newForkTestShare(t)
	share.CreateFile("both")

	var fi [32]byte
	copy(fi[:], []byte("APPLmdrp"))
	if err := share.WriteFinderInfo("both", fi); err != nil {
		t.Fatalf("WriteFinderInfo: %v", err)
	}
	rf, _ := share.OpenFork("both", ResourceFork, os.O_RDWR|os.O_CREATE)
	rf.WriteAt([]byte("rsrc"), 0)
	rf.Close()

	// FinderInfo must survive the resource-fork write (same sidecar).
	got, ok, _ := share.ReadFinderInfo("both")
	if !ok || got != fi {
		t.Fatalf("FinderInfo lost after resource write: ok=%v got=%x", ok, got)
	}
}

func TestForkEngine_CommentRoundTrip(t *testing.T) {
	share, _ := newForkTestShare(t)
	share.CreateFile("noted")
	if err := share.WriteComment("noted", []byte("hello world")); err != nil {
		t.Fatalf("WriteComment: %v", err)
	}
	c, ok := share.ReadComment("noted")
	if !ok || string(c) != "hello world" {
		t.Fatalf("ReadComment = %q ok=%v", c, ok)
	}
}

func TestForkEngine_DeleteAndMoveMetadata(t *testing.T) {
	share, base := newForkTestShare(t)
	share.CreateFile("orig")
	share.WriteComment("orig", []byte("x"))

	if err := share.MoveMetadata("orig", "renamed"); err != nil {
		t.Fatalf("MoveMetadata: %v", err)
	}
	if _, err := base.Stat("._orig"); err == nil {
		t.Fatal("old sidecar still present after move")
	}
	if c, ok := share.ReadComment("renamed"); !ok || string(c) != "x" {
		t.Fatalf("comment lost after move: %q ok=%v", c, ok)
	}

	if err := share.DeleteMetadata("renamed"); err != nil {
		t.Fatalf("DeleteMetadata: %v", err)
	}
	if _, ok := share.ReadComment("renamed"); ok {
		t.Fatal("comment present after delete")
	}
}
