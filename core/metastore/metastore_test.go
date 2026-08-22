package metastore

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMemRoundTripAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.db")

	s, err := NewMem(path)
	if err != nil {
		t.Fatalf("NewMem: %v", err)
	}
	if err := s.Put([]byte("cnid:1"), []byte("alpha")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put([]byte("cnid:2"), []byte("beta")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Close(); err != nil { // Close syncs
		t.Fatalf("Close: %v", err)
	}

	// Reopen the same path: the snapshot must reload.
	s2, err := NewMem(path)
	if err != nil {
		t.Fatalf("reopen NewMem: %v", err)
	}
	defer s2.Close()

	if v, ok := s2.Get([]byte("cnid:1")); !ok || !bytes.Equal(v, []byte("alpha")) {
		t.Fatalf("cnid:1 = %q,%v after reopen", v, ok)
	}
	if v, ok := s2.Get([]byte("cnid:2")); !ok || !bytes.Equal(v, []byte("beta")) {
		t.Fatalf("cnid:2 = %q,%v after reopen", v, ok)
	}
}

func TestDelete(t *testing.T) {
	s, _ := NewMem("")
	s.Put([]byte("k"), []byte("v"))
	s.Delete([]byte("k"))
	if _, ok := s.Get([]byte("k")); ok {
		t.Fatal("key should be gone after Delete")
	}
}

func TestRangePrefixAndEarlyExit(t *testing.T) {
	s, _ := NewMem("")
	s.Put([]byte("a:1"), []byte("1"))
	s.Put([]byte("a:2"), []byte("2"))
	s.Put([]byte("a:3"), []byte("3"))
	s.Put([]byte("b:1"), []byte("x"))

	// Prefix scoping: only a: keys.
	var seen []string
	s.Range([]byte("a:"), func(k, v []byte) bool {
		seen = append(seen, string(k))
		return true
	})
	if len(seen) != 3 {
		t.Fatalf("prefix a: should visit 3 keys, got %v", seen)
	}

	// Early exit: stop after the first key (sorted order → a:1).
	var first []string
	s.Range([]byte("a:"), func(k, v []byte) bool {
		first = append(first, string(k))
		return false
	})
	if len(first) != 1 || first[0] != "a:1" {
		t.Fatalf("early-exit should visit exactly a:1, got %v", first)
	}
}

func TestGetReturnsCopy(t *testing.T) {
	s, _ := NewMem("")
	s.Put([]byte("k"), []byte("orig"))
	v, _ := s.Get([]byte("k"))
	v[0] = 'X' // mutate the returned slice
	again, _ := s.Get([]byte("k"))
	if !bytes.Equal(again, []byte("orig")) {
		t.Fatalf("Get must return a copy; store was mutated to %q", again)
	}
}

func TestOpenUnknownKind(t *testing.T) {
	if _, err := Open("nope", ""); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("Open unknown kind: want ErrUnknownKind, got %v", err)
	}
	if _, err := Open("mem", ""); err != nil {
		t.Fatalf("Open mem: %v", err)
	}
}

func TestCorruptSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.db")
	if err := os.WriteFile(path, []byte("not-a-snapshot"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewMem(path); !errors.Is(err, ErrCorruptSnapshot) {
		t.Fatalf("want ErrCorruptSnapshot, got %v", err)
	}
}
