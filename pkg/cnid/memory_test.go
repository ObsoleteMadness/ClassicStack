package cnid

import (
	"path/filepath"
	"testing"
)

func TestMemoryStoreEnsureAndLookup(t *testing.T) {
	t.Parallel()
	s := NewMemoryStore()
	if s.RootID() != Root {
		t.Fatalf("RootID = %d, want %d", s.RootID(), Root)
	}

	a := s.Ensure("dir/foo")
	if a < firstDynamic {
		t.Fatalf("Ensure returned reserved CNID %d", a)
	}
	if got := s.Ensure("dir/foo"); got != a {
		t.Fatalf("Ensure not idempotent: %d vs %d", got, a)
	}
	if got, ok := s.CNID("dir/foo"); !ok || got != a {
		t.Fatalf("CNID lookup: got=%d ok=%v, want %d", got, ok, a)
	}
	want := filepath.Clean("dir/foo")
	if got, ok := s.Path(a); !ok || got != want {
		t.Fatalf("Path lookup: got=%q want=%q ok=%v", got, want, ok)
	}
}

func TestMemoryStoreRebindPrefix(t *testing.T) {
	t.Parallel()
	s := NewMemoryStore()
	root := s.Ensure("a")
	child := s.Ensure("a/b/c")

	s.Rebind("a", "x")

	if got, ok := s.Path(root); !ok || got != "x" {
		t.Fatalf("root path after rebind: got=%q ok=%v", got, ok)
	}
	wantChild := filepath.Clean("x/b/c")
	if got, ok := s.Path(child); !ok || got != wantChild {
		t.Fatalf("child path after rebind: got=%q want=%q ok=%v", got, wantChild, ok)
	}
	if _, ok := s.CNID("a/b/c"); ok {
		t.Fatal("old path still resolvable after rebind")
	}
}

func TestMemoryStoreRemoveSubtree(t *testing.T) {
	t.Parallel()
	s := NewMemoryStore()
	keep := s.Ensure("keep")
	s.Ensure("drop")
	s.Ensure("drop/child")

	s.Remove("drop")

	if _, ok := s.CNID("drop"); ok {
		t.Error("drop not removed")
	}
	if _, ok := s.CNID("drop/child"); ok {
		t.Error("drop/child not removed")
	}
	if _, ok := s.Path(keep); !ok {
		t.Error("keep was incorrectly removed")
	}
}

func TestMemoryStoreEnsureReserved(t *testing.T) {
	t.Parallel()
	s := NewMemoryStore()
	got := s.EnsureReserved("foo", 100)
	if got != 100 {
		t.Fatalf("EnsureReserved = %d, want 100", got)
	}
	if path, ok := s.Path(100); !ok || path != "foo" {
		t.Fatalf("Path(100) = %q %v", path, ok)
	}
	// Subsequent Ensure should skip 100.
	next := s.Ensure("bar")
	if next == 100 {
		t.Fatal("Ensure collided with reserved CNID")
	}
}
