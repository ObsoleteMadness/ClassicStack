package fs

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// TestLocalFSRoundTrip exercises the real local_fs backend through BuildShare:
// create/write/read/stat/rename/remove over a host temp directory, proving the
// first real registry backend assembles and serves like memfs.
func TestLocalFSRoundTrip(t *testing.T) {
	root := t.TempDir()
	ffs, err := BuildShare(ShareSpec{
		Name:          "Local",
		FSType:        "local_fs",
		Path:          root,
		ForkBackend:   "appledouble",
		FilenameCodec: "macroman-utf8",
	}, nil)
	if err != nil {
		t.Fatalf("BuildShare local_fs: %v", err)
	}

	// Create a directory and a file inside the share.
	if err := ffs.CreateDir("docs"); err != nil {
		t.Fatalf("CreateDir: %v", err)
	}
	f, err := ffs.CreateFile("docs/readme.txt")
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	want := []byte("hello local fs")
	if _, err := f.WriteAt(want, 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// File really exists on the host under root.
	if _, err := os.Stat(filepath.Join(root, "docs", "readme.txt")); err != nil {
		t.Fatalf("host file missing: %v", err)
	}

	// Read it back through the FS.
	rf, err := ffs.OpenFile("docs/readme.txt", os.O_RDONLY)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	got := make([]byte, len(want))
	if _, err := rf.ReadAt(got, 0); err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("ReadAt: %v", err)
	}
	rf.Close()
	if string(got) != string(want) {
		t.Fatalf("read mismatch: got %q want %q", got, want)
	}

	// Stat + ReadDir.
	fi, err := ffs.Stat("docs/readme.txt")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if fi.Size() != int64(len(want)) {
		t.Fatalf("Stat size = %d, want %d", fi.Size(), len(want))
	}
	ents, err := ffs.ReadDir("docs")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(ents) == 0 {
		t.Fatalf("ReadDir returned no entries")
	}

	// Rename then remove.
	if err := ffs.Rename("docs/readme.txt", "docs/notes.txt"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if _, err := ffs.Stat("docs/readme.txt"); err == nil {
		t.Fatalf("old name still present after rename")
	}
	if err := ffs.Remove("docs/notes.txt"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := ffs.Stat("docs/notes.txt"); err == nil {
		t.Fatalf("file still present after remove")
	}
}

// TestLocalFSDiskUsage proves the real per-OS DiskUsage reports a non-zero,
// self-consistent total/free for the host volume backing the share root. On a
// platform with no build-tagged statfs/GetDiskFreeSpaceEx query (the "other"
// fallback / TinyGo) it returns 0/0 (unknown); the test skips the magnitude
// assertions there rather than failing, since 0/0 is the documented contract.
func TestLocalFSDiskUsage(t *testing.T) {
	root := t.TempDir()
	l, err := newLocalFS(ShareSpec{FSType: "local_fs", Path: root}, nil)
	if err != nil {
		t.Fatalf("newLocalFS: %v", err)
	}
	total, free, err := l.DiskUsage("")
	if err != nil {
		t.Fatalf("DiskUsage: %v", err)
	}
	if total == 0 && free == 0 {
		t.Skip("DiskUsage unsupported on this platform (0/0 fallback) — nothing to assert")
	}
	if total == 0 {
		t.Fatalf("DiskUsage total = 0 with non-zero free %d", free)
	}
	if free > total {
		t.Fatalf("DiskUsage free %d exceeds total %d", free, total)
	}
}

// TestLocalFSDiskUsageRejectsTraversal proves a per-subtree DiskUsage query is
// resolved under the root and cannot escape the share.
func TestLocalFSDiskUsageRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	l, err := newLocalFS(ShareSpec{FSType: "local_fs", Path: root}, nil)
	if err != nil {
		t.Fatalf("newLocalFS: %v", err)
	}
	if _, _, err := l.DiskUsage("../escape"); !errors.Is(err, ErrPathEscape) {
		t.Fatalf("DiskUsage(../escape) err = %v, want ErrPathEscape", err)
	}
}

// TestLocalFSRejectsTraversal proves a '..' path cannot escape the share root.
func TestLocalFSRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	l, err := newLocalFS(ShareSpec{FSType: "local_fs", Path: root}, nil)
	if err != nil {
		t.Fatalf("newLocalFS: %v", err)
	}
	if _, err := l.host("../escape"); !errors.Is(err, ErrPathEscape) {
		t.Fatalf("host(../escape) err = %v, want ErrPathEscape", err)
	}
	if _, err := l.host("docs/../../escape"); !errors.Is(err, ErrPathEscape) {
		t.Fatalf("host(docs/../../escape) err = %v, want ErrPathEscape", err)
	}
	// A path that climbs then returns inside the root is fine.
	if _, err := l.host("docs/../keep"); err != nil {
		t.Fatalf("host(docs/../keep) err = %v, want nil", err)
	}
}

// TestLocalFSRequiresPath proves BuildShare rejects a local_fs share with no
// path via the declared required Param (M6a param validation).
func TestLocalFSRequiresPath(t *testing.T) {
	if _, err := BuildShare(ShareSpec{Name: "NoPath", FSType: "local_fs"}, nil); err == nil {
		t.Fatalf("BuildShare local_fs without path: expected error, got nil")
	}
	// ParamsFor advertises the path param for the UI/config layer.
	ps := ParamsFor("local_fs")
	if len(ps) != 1 || ps[0].Key != PathKey || !ps[0].Required {
		t.Fatalf("ParamsFor(local_fs) = %+v, want one required path param", ps)
	}
}
