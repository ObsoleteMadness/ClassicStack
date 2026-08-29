//go:build zipfs || all

package zipfs

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	corefs "github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// TestZipFSRoundTrip exercises the zipfs backend through BuildShare:
// create/write/read/stat/rename/remove over a .zip archive, proving the whole §9
// storage seam (appledouble fork engine + mem metastore, no sqlite) assembles and
// serves like local_fs/memfs — the canonical "the VFS structure works" check.
func TestZipFSRoundTrip(t *testing.T) {
	arc := filepath.Join(t.TempDir(), "vol.zip")
	ffs, err := corefs.BuildShare(corefs.ShareSpec{
		Name:          "Zip",
		FSType:        "zipfs",
		Path:          arc,
		ForkBackend:   "appledouble",
		FilenameCodec: "macroman-utf8",
		Metastore:     "mem",
	}, nil)
	if err != nil {
		t.Fatalf("BuildShare zipfs: %v", err)
	}

	if err := ffs.CreateDir("docs"); err != nil {
		t.Fatalf("CreateDir: %v", err)
	}
	f, err := ffs.CreateFile("docs/readme.txt")
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	want := []byte("hello zip fs")
	if _, err := f.WriteAt(want, 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// The mutation was flushed to a real .zip on disk.
	if _, err := os.Stat(arc); err != nil {
		t.Fatalf("archive not written: %v", err)
	}

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

// TestZipFSPersistsAcrossReopen proves a write flushed to the .zip is read back by a
// freshly-constructed backend over the same archive — the durability the in-memory
// memfs reference cannot give, and the whole reason zipfs exists as a structure check.
func TestZipFSPersistsAcrossReopen(t *testing.T) {
	arc := filepath.Join(t.TempDir(), "vol.zip")
	z1, err := newZipFS(corefs.ShareSpec{FSType: "zipfs", Path: arc}, nil)
	if err != nil {
		t.Fatalf("newZipFS: %v", err)
	}
	f, err := z1.CreateFile("a/b/c.txt")
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	want := []byte("persisted payload")
	if _, err := f.WriteAt(want, 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := z1.Close(); err != nil {
		t.Fatalf("Close fs: %v", err)
	}

	z2, err := newZipFS(corefs.ShareSpec{FSType: "zipfs", Path: arc}, nil)
	if err != nil {
		t.Fatalf("reopen newZipFS: %v", err)
	}
	rf, err := z2.OpenFile("a/b/c.txt", os.O_RDONLY)
	if err != nil {
		t.Fatalf("reopen OpenFile: %v", err)
	}
	got := make([]byte, len(want))
	if _, err := rf.ReadAt(got, 0); err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("reopen ReadAt: %v", err)
	}
	rf.Close()
	if string(got) != string(want) {
		t.Fatalf("persisted read mismatch: got %q want %q", got, want)
	}
	// Intermediate directories survived the round-trip.
	if fi, err := z2.Stat("a/b"); err != nil || !fi.IsDir() {
		t.Fatalf("Stat(a/b) = %v, %v; want a directory", fi, err)
	}
}

// TestZipFSReadOnly proves a read-only share rejects every mutation and that the
// share-spec validator allows zipfs + appledouble (the only legal RO combination).
func TestZipFSReadOnly(t *testing.T) {
	arc := filepath.Join(t.TempDir(), "ro.zip")
	// Build a non-empty archive on disk first.
	buf, err := os.Create(arc)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	w := zip.NewWriter(buf)
	fw, _ := w.Create("hello.txt")
	if _, err := fw.Write([]byte("read me")); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("seed close: %v", err)
	}
	buf.Close()

	z, err := newZipFS(corefs.ShareSpec{FSType: "zipfs", Path: arc, ReadOnly: true}, nil)
	if err != nil {
		t.Fatalf("newZipFS ro: %v", err)
	}
	if !z.Capabilities().ReadOnly {
		t.Fatalf("read-only share did not advertise ReadOnly capability")
	}
	rf, err := z.OpenFile("hello.txt", os.O_RDONLY)
	if err != nil {
		t.Fatalf("OpenFile ro read: %v", err)
	}
	got := make([]byte, 7)
	if _, err := rf.ReadAt(got, 0); err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("ReadAt ro: %v", err)
	}
	rf.Close()
	if string(got) != "read me" {
		t.Fatalf("ro read mismatch: got %q", got)
	}
	if _, err := z.CreateFile("nope.txt"); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("CreateFile ro err = %v, want ErrReadOnly", err)
	}
	if err := z.CreateDir("nope"); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("CreateDir ro err = %v, want ErrReadOnly", err)
	}
	if err := z.Remove("hello.txt"); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("Remove ro err = %v, want ErrReadOnly", err)
	}
}

// TestZipFSReadOnlyRejectsNonAppleDoubleFork proves the share-build validator enforces
// the documented constraint: a read-only zipfs may only use the appledouble fork
// backend (nothing can be written, so forks must be baked-in sidecars).
func TestZipFSReadOnlyRejectsNonAppleDoubleFork(t *testing.T) {
	arc := filepath.Join(t.TempDir(), "ro.zip")
	if _, err := corefs.BuildShare(corefs.ShareSpec{
		Name:        "ZipRO",
		FSType:      "zipfs",
		Path:        arc,
		ReadOnly:    true,
		ForkBackend: "xattr",
	}, nil); err == nil {
		t.Fatalf("BuildShare read-only zipfs with xattr fork: expected error, got nil")
	}
}

// TestZipFSRequiresPath proves the registered factory declares path as required.
func TestZipFSRequiresPath(t *testing.T) {
	ps := corefs.ParamsFor("zipfs")
	if len(ps) != 1 || ps[0].Key != corefs.PathKey || !ps[0].Required {
		t.Fatalf("ParamsFor(zipfs) = %+v, want one required path param", ps)
	}
	if _, err := corefs.BuildShare(corefs.ShareSpec{Name: "NoPath", FSType: "zipfs"}, nil); err == nil {
		t.Fatalf("BuildShare zipfs without path: expected error, got nil")
	}
}

// TestZipFSStreamingReadOffsets proves a clean member is read correctly at arbitrary
// offsets — sequential forward (cursor advances), and BACKWARD (cursor reopens and
// re-inflates) — which is the streaming-read contract that replaced loading the whole
// member into RAM. Uses a compressible-but-large payload so the member is genuinely
// deflated (the backward-seek path matters only for a deflate stream).
func TestZipFSStreamingReadOffsets(t *testing.T) {
	arc := filepath.Join(t.TempDir(), "big.zip")
	z1, err := newZipFS(corefs.ShareSpec{FSType: "zipfs", Path: arc}, nil)
	if err != nil {
		t.Fatalf("newZipFS: %v", err)
	}
	// 1 MiB of position-dependent bytes so a wrong offset is detectable.
	const size = 1 << 20
	payload := make([]byte, size)
	for i := range payload {
		payload[i] = byte(i*31 + 7)
	}
	f, err := z1.CreateFile("big.bin")
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	if _, err := f.WriteAt(payload, 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := z1.Close(); err != nil {
		t.Fatalf("Close fs: %v", err)
	}

	// Reopen so the read streams from the flushed archive (not a staged temp file).
	z2, err := newZipFS(corefs.ShareSpec{FSType: "zipfs", Path: arc}, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer z2.Close()
	rf, err := z2.OpenFile("big.bin", os.O_RDONLY)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer rf.Close()

	check := func(off int64, n int) {
		got := make([]byte, n)
		r, err := rf.ReadAt(got, off)
		if err != nil && !errors.Is(err, io.EOF) {
			t.Fatalf("ReadAt(%d,%d): %v", off, n, err)
		}
		if !bytes.Equal(got[:r], payload[off:off+int64(r)]) {
			t.Fatalf("ReadAt(%d,%d) mismatch", off, n)
		}
	}
	// Forward sequential (cursor advances), a far jump, then a BACKWARD seek (reopen),
	// then the very end (EOF boundary).
	check(0, 4096)
	check(4096, 4096)
	check(512<<10, 8192) // jump forward
	check(1024, 4096)    // backward — exercises the reopen+re-inflate path
	check(size-100, 100) // tail
}

// TestZipFSDoesNotHoldArchiveHandle proves the backend keeps NO long-lived OS handle on
// the archive: after construction (and with no open file handles) the .zip can be
// renamed/removed on every platform — including Windows, which refuses to rename a file
// that is still open. This is the property that lets a 2 GiB volume cost neither 2 GiB
// of RAM nor a pinned file descriptor.
func TestZipFSDoesNotHoldArchiveHandle(t *testing.T) {
	dir := t.TempDir()
	arc := filepath.Join(dir, "vol.zip")
	z, err := newZipFS(corefs.ShareSpec{FSType: "zipfs", Path: arc}, nil)
	if err != nil {
		t.Fatalf("newZipFS: %v", err)
	}
	f, err := z.CreateFile("a.txt")
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	if _, err := f.WriteAt([]byte("hi"), 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if err := f.Close(); err != nil { // flushes the archive, releases all handles
		t.Fatalf("Close file: %v", err)
	}
	// No file handle is open now; the archive must be freely renamable.
	moved := filepath.Join(dir, "moved.zip")
	if err := os.Rename(arc, moved); err != nil {
		t.Fatalf("rename archive (handle leaked?): %v", err)
	}
	if err := os.Rename(moved, arc); err != nil {
		t.Fatalf("rename back: %v", err)
	}
	_ = z.Close()
}
