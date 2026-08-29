package extmap

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSaveReadRoundTrip proves a valid extension map saves and reads back, and that a
// second save of a prior file leaves a numbered backup.
func TestSaveReadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "extmap.conf")

	first := ".txt \"TEXT\" \"ttxt\"\n"
	if backup, err := Save(path, []byte(first)); err != nil || backup != "" {
		t.Fatalf("first Save: backup=%q err=%v (want no backup)", backup, err)
	}
	got, err := Read(path)
	if err != nil || string(got) != first {
		t.Fatalf("Read = %q, %v", got, err)
	}

	// A second save backs up the prior file to path.1.
	second := ".gif \"GIFf\" \"ogle\"\n"
	backup, err := Save(path, []byte(second))
	if err != nil {
		t.Fatalf("second Save: %v", err)
	}
	if backup != path+".1" {
		t.Fatalf("backup = %q, want %q", backup, path+".1")
	}
	if b, _ := os.ReadFile(backup); string(b) != first {
		t.Fatalf("backup content = %q, want the prior file", b)
	}
}

// TestSaveRejectsInvalid proves Save validates: a malformed map is not written.
func TestSaveRejectsInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "extmap.conf")
	if _, err := Save(path, []byte("garbage no quotes")); err == nil {
		t.Fatal("expected a validation error")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("invalid content must not be written")
	}
}

// TestReadMissingFile proves a missing file reads as empty, no error (the UI shows an
// empty grid to fill in).
func TestReadMissingFile(t *testing.T) {
	data, err := Read(filepath.Join(t.TempDir(), "nope.conf"))
	if err != nil || data != nil {
		t.Fatalf("Read(missing) = %q, %v; want nil, nil", data, err)
	}
}
