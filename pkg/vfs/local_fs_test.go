package vfs

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestLocalFSRoundTrip(t *testing.T) {
	dir := t.TempDir()

	fsBackend, err := New(LocalFSName, Params{Name: "Test", Path: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	target := filepath.Join(dir, "hello.txt")
	f, err := fsBackend.CreateFile(target)
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	if _, err := f.WriteAt([]byte("hi"), 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	info, err := fsBackend.Stat(target)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Size() != 2 {
		t.Fatalf("size: got %d want 2", info.Size())
	}

	entries, err := fsBackend.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "hello.txt" {
		t.Fatalf("ReadDir: %v", entries)
	}

	renamed := filepath.Join(dir, "bye.txt")
	if err := fsBackend.Rename(target, renamed); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if _, err := os.Stat(renamed); err != nil {
		t.Fatalf("renamed file missing: %v", err)
	}

	if err := fsBackend.Remove(renamed); err != nil {
		t.Fatalf("Remove: %v", err)
	}
}

func TestLocalFSCapabilities(t *testing.T) {
	caps := NewLocalFileSystem().Capabilities()
	if !caps.ChildCount || !caps.DirAttributes || !caps.ReadOnlyState {
		t.Fatalf("expected universal caps; got %+v", caps)
	}
	if caps.CatSearch {
		t.Fatal("CatSearch is AFP-specific and must not be claimed by the generic local backend")
	}
}

func TestLocalFSRegistration(t *testing.T) {
	names := RegisteredNames()
	if !slices.Contains(names, LocalFSName) {
		t.Fatalf("local_fs not in registry: %v", names)
	}
}
