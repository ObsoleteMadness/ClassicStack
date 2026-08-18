package finder

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

func TestCatalogMkdirListRename(t *testing.T) {
	ffs, err := fs.BuildShare(fs.ShareSpec{Name: "Mem", FSType: "memfs"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	svc := New(nil, nil)
	root := ffs.Meta().EnsureCNID("")
	svc.put(&Session{
		ID:      "t",
		Kind:    "local",
		Volume:  "Mem",
		FS:      ffs,
		local:   true,
		touched: time.Now(),
	})

	dir, err := svc.Mkdir("t", root, "Folder")
	if err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if !dir.IsDir || dir.Name != "Folder" {
		t.Fatalf("mkdir node = %+v", dir)
	}

	file, err := svc.CreateFile("t", dir.ID, "hello.txt", []byte("hi"), nil, nil)
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	if file.IsDir || file.DataBytes != 2 {
		t.Fatalf("file node = %+v", file)
	}

	kids, err := svc.Children("t", dir.ID)
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	if len(kids) != 1 || kids[0].Name != "hello.txt" {
		t.Fatalf("children = %+v", kids)
	}

	got, err := svc.Lookup("t", dir.ID, "hello.txt")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.ID != file.ID {
		t.Fatalf("lookup id %d want %d", got.ID, file.ID)
	}

	data, err := svc.ReadFork("t", file.ID, false, 0, 0)
	if err != nil {
		t.Fatalf("ReadFork: %v", err)
	}
	if string(data) != "hi" {
		t.Fatalf("data %q", data)
	}

	if err := svc.Rename("t", file.ID, "bye.txt"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if _, err := svc.Lookup("t", dir.ID, "bye.txt"); err != nil {
		t.Fatalf("lookup after rename: %v", err)
	}

	n, err := svc.GetNode("t", dir.ID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if n.Name != "Folder" {
		t.Fatalf("dir name %q", n.Name)
	}

	if err := svc.Remove("t", file.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	kids, err = svc.Children("t", dir.ID)
	if err != nil {
		t.Fatalf("Children after remove: %v", err)
	}
	if len(kids) != 0 {
		t.Fatalf("expected empty, got %+v", kids)
	}
}

type forkIOCounter struct {
	fs.ForkFS
	opens  int
	closes int
}

type countingForkFile struct {
	fs.File
	onClose func()
}

func (f countingForkFile) Close() error {
	f.onClose()
	return f.File.Close()
}

func (c *forkIOCounter) OpenFork(path string, fork fs.ForkType, flag int) (fs.File, error) {
	c.opens++
	f, err := c.ForkFS.OpenFork(path, fork, flag)
	if err != nil {
		return nil, err
	}
	return countingForkFile{File: f, onClose: func() { c.closes++ }}, nil
}

func TestReadForkOpensAndCloses(t *testing.T) {
	base, err := fs.BuildShare(fs.ShareSpec{Name: "Mem", FSType: "memfs"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	wf, err := base.CreateFile("hello.txt")
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	if _, err := wf.WriteAt([]byte("abcdef"), 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	_ = wf.Close()

	counted := &forkIOCounter{ForkFS: base}
	svc := New(nil, nil)
	root := counted.Meta().EnsureCNID("")
	svc.put(&Session{ID: "t", Kind: "local", Volume: "Mem", FS: counted, local: true, touched: time.Now()})
	file, err := svc.Lookup("t", root, "hello.txt")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	got, err := svc.ReadFork("t", file.ID, false, 1, 3)
	if err != nil {
		t.Fatalf("ReadFork range: %v", err)
	}
	if string(got) != "bcd" {
		t.Fatalf("range = %q, want bcd", got)
	}
	if counted.opens != 1 || counted.closes != 1 {
		t.Fatalf("range open/close = %d/%d, want 1/1", counted.opens, counted.closes)
	}

	all, err := svc.ReadFork("t", file.ID, false, 0, 0)
	if err != nil {
		t.Fatalf("ReadFork all: %v", err)
	}
	if string(all) != "abcdef" {
		t.Fatalf("all = %q, want abcdef", all)
	}
	if counted.opens != 2 || counted.closes != 2 {
		t.Fatalf("all open/close = %d/%d, want 2/2", counted.opens, counted.closes)
	}
}

type readDirCounter struct {
	fs.ForkFS
	n int
}

func (c *readDirCounter) ReadDir(path string) ([]os.DirEntry, error) {
	c.n++
	return c.ForkFS.ReadDir(path)
}

func TestLookupDoesNotEnumerateDirectory(t *testing.T) {
	base, err := fs.BuildShare(fs.ShareSpec{Name: "Mem", FSType: "memfs"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	if err := base.CreateDir("Folder"); err != nil {
		t.Fatalf("CreateDir: %v", err)
	}
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		f, err := base.CreateFile("Folder/" + name)
		if err != nil {
			t.Fatalf("CreateFile %s: %v", name, err)
		}
		_ = f.Close()
	}
	counted := &readDirCounter{ForkFS: base}
	svc := New(nil, nil)
	root := counted.Meta().EnsureCNID("")
	svc.put(&Session{ID: "t", Kind: "local", Volume: "Mem", FS: counted, local: true, touched: time.Now()})

	folder, err := svc.Lookup("t", root, "Folder")
	if err != nil {
		t.Fatalf("Lookup Folder: %v", err)
	}
	if counted.n != 0 {
		t.Fatalf("Lookup(Folder) ReadDir count = %d, want 0", counted.n)
	}

	got, err := svc.Lookup("t", folder.ID, "c")
	if err != nil {
		t.Fatalf("Lookup c: %v", err)
	}
	if got.Name != "c" || got.IsDir {
		t.Fatalf("lookup c = %+v", got)
	}
	if counted.n != 0 {
		t.Fatalf("Lookup(c) ReadDir count = %d, want 0 (must Stat, not enumerate)", counted.n)
	}

	if _, err := svc.Lookup("t", folder.ID, "Icon\r"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Lookup Icon\\r err = %v, want ErrNotFound", err)
	}
	if counted.n != 0 {
		t.Fatalf("Lookup(missing) ReadDir count = %d, want 0", counted.n)
	}

	kids, err := svc.Children("t", folder.ID)
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	if len(kids) != 5 {
		t.Fatalf("Children count = %d, want 5", len(kids))
	}
	if counted.n != 1 {
		t.Fatalf("Children ReadDir count = %d, want 1", counted.n)
	}
}

func TestLocalIDParse(t *testing.T) {
	id := localID("afp", "Mac HD")
	proto, name, ok := parseLocalID(id)
	if !ok || proto != "afp" || name != "Mac HD" {
		t.Fatalf("parse %q → %q %q %v", id, proto, name, ok)
	}
}

func TestChildrenHidesAppleDoubleSidecars(t *testing.T) {
	ffs, err := fs.BuildShare(fs.ShareSpec{Name: "Mem", FSType: "memfs"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	if _, err := ffs.CreateFile("doc"); err != nil {
		t.Fatalf("CreateFile doc: %v", err)
	}
	if _, err := ffs.OpenFile("._doc", os.O_CREATE|os.O_RDWR); err != nil {
		t.Fatalf("OpenFile sidecar: %v", err)
	}
	if err := ffs.CreateDir(".AppleDouble"); err != nil {
		t.Fatalf("CreateDir: %v", err)
	}

	svc := New(nil, nil)
	root := ffs.Meta().EnsureCNID("")
	svc.put(&Session{ID: "t", Kind: "local", Volume: "Mem", FS: ffs, local: true, touched: time.Now()})
	kids, err := svc.Children("t", root)
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	if len(kids) != 1 || kids[0].Name != "doc" {
		t.Fatalf("children = %+v, want [doc] (sidecars hidden)", kids)
	}
}

func TestChildrenShowsSidecarsOnNoFork(t *testing.T) {
	ffs, err := fs.BuildShare(fs.ShareSpec{Name: "Mem", FSType: "memfs", ForkBackend: "nofork"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	if _, err := ffs.CreateFile("doc"); err != nil {
		t.Fatalf("CreateFile doc: %v", err)
	}
	if _, err := ffs.OpenFile("._doc", os.O_CREATE|os.O_RDWR); err != nil {
		t.Fatalf("OpenFile sidecar: %v", err)
	}

	svc := New(nil, nil)
	root := ffs.Meta().EnsureCNID("")
	svc.put(&Session{ID: "t", Kind: "local", Volume: "Mem", FS: ffs, local: true, touched: time.Now()})
	kids, err := svc.Children("t", root)
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	got := map[string]bool{}
	for _, k := range kids {
		got[k.Name] = true
	}
	if !got["doc"] || !got["._doc"] {
		t.Fatalf("nofork children = %+v, want doc and ._doc", kids)
	}
}

func TestOpenLocalReusesSession(t *testing.T) {
	ffs, err := fs.BuildShare(fs.ShareSpec{Name: "Mem", FSType: "memfs"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	svc := New(nil, nil)
	svc.put(&Session{
		ID:        "s1",
		Kind:      "local",
		Volume:    "Mem",
		FS:        ffs,
		local:     true,
		remoteURI: "local:afp:Mem",
		touched:   time.Now(),
	})
	info, err := svc.OpenLocal("local:afp:Mem")
	if err != nil {
		t.Fatalf("OpenLocal: %v", err)
	}
	if info.SessionID != "s1" {
		t.Fatalf("session %q, want reused s1", info.SessionID)
	}
}
