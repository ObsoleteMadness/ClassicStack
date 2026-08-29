package metastore

import "testing"

func newCNID(t *testing.T) *CNIDStore {
	t.Helper()
	s, err := NewMem("")
	if err != nil {
		t.Fatalf("NewMem: %v", err)
	}
	return NewCNIDStore(s)
}

func TestCNID_EnsureStableAndReverse(t *testing.T) {
	c := newCNID(t)
	a := c.Ensure("dir/file.txt")
	if a < cnidFirstDynamic {
		t.Fatalf("first dynamic cnid = %d, want >= %d", a, cnidFirstDynamic)
	}
	if again := c.Ensure("dir/file.txt"); again != a {
		t.Fatalf("Ensure not stable: %d vs %d", a, again)
	}
	if p, ok := c.Path(a); !ok || p != "dir/file.txt" {
		t.Fatalf("Path(%d) = %q ok=%v", a, p, ok)
	}
	if id, ok := c.CNID("dir/file.txt"); !ok || id != a {
		t.Fatalf("CNID = %d ok=%v, want %d", id, ok, a)
	}
}

func TestCNID_UniquePerPath(t *testing.T) {
	c := newCNID(t)
	a := c.Ensure("a")
	b := c.Ensure("b")
	if a == b {
		t.Fatalf("distinct paths share cnid %d", a)
	}
}

func TestCNID_RebindSubtree(t *testing.T) {
	c := newCNID(t)
	dir := c.Ensure("old")
	child := c.Ensure("old/child.txt")

	c.Rebind("old", "new")

	if p, _ := c.Path(dir); p != "new" {
		t.Fatalf("dir path after rebind = %q", p)
	}
	if p, _ := c.Path(child); p != "new/child.txt" {
		t.Fatalf("child path after rebind = %q", p)
	}
	if _, ok := c.CNID("old"); ok {
		t.Fatal("old path still resolves after rebind")
	}
}

func TestCNID_RemoveSubtree(t *testing.T) {
	c := newCNID(t)
	c.Ensure("d")
	c.Ensure("d/x")
	c.Remove("d")
	if _, ok := c.CNID("d"); ok {
		t.Fatal("d present after remove")
	}
	if _, ok := c.CNID("d/x"); ok {
		t.Fatal("d/x present after remove")
	}
}

func TestCNID_PersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/cnid.mst"

	s1, _ := NewMem(path)
	c1 := NewCNIDStore(s1)
	id := c1.Ensure("keep/me.txt")
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, _ := NewMem(path)
	c2 := NewCNIDStore(s2)
	if got, ok := c2.CNID("keep/me.txt"); !ok || got != id {
		t.Fatalf("after reopen CNID = %d ok=%v, want %d", got, ok, id)
	}
	// A fresh Ensure must not reuse the persisted id.
	if next := c2.Ensure("another.txt"); next == id {
		t.Fatalf("reused persisted cnid %d", id)
	}
}

func TestCNID_EnsureReservedAdvancesSeq(t *testing.T) {
	c := newCNID(t)
	c.EnsureReserved("root", 100)
	if next := c.Ensure("x"); next <= 100 {
		t.Fatalf("Ensure after reserve(100) = %d, want > 100", next)
	}
}
