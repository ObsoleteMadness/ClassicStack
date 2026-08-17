package finder

import (
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

func TestLocalIDParse(t *testing.T) {
	id := localID("afp", "Mac HD")
	proto, name, ok := parseLocalID(id)
	if !ok || proto != "afp" || name != "Mac HD" {
		t.Fatalf("parse %q → %q %q %v", id, proto, name, ok)
	}
}
