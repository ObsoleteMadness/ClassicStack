package fs

import (
	"testing"
)

// seedMem builds a memfs with a small tree for the walk tests.
func seedMem(t *testing.T) *memFS {
	t.Helper()
	m := newMemFS(ShareSpec{}).(*memFS)
	_ = m.CreateDir("sub")
	for _, p := range []string{"report-jan.txt", "sub/report-feb.txt", "sub/notes.txt"} {
		f, err := m.CreateFile(p)
		if err != nil {
			t.Fatalf("CreateFile %q: %v", p, err)
		}
		_, _ = f.WriteAt([]byte("x"), 0)
		_ = f.Close()
	}
	return m
}

// TestWalkCatSearch_PartialAcrossTree proves the default walk descends into
// subdirectories and substring-matches names case-insensitively.
func TestWalkCatSearch_PartialAcrossTree(t *testing.T) {
	m := seedMem(t)
	res, next, err := WalkCatSearch(m, CatSearchCriteria{MatchName: true, Partial: true, Name: "REPORT", Max: 50}, nil)
	if err != nil {
		t.Fatalf("WalkCatSearch: %v", err)
	}
	if len(next) != 0 {
		t.Fatalf("next cursor = %v, want empty (last page)", next)
	}
	got := map[string]bool{}
	for _, r := range res {
		got[r.Path] = true
	}
	if !got["report-jan.txt"] || !got["sub/report-feb.txt"] {
		t.Fatalf("results = %v, want report-jan.txt + sub/report-feb.txt", res)
	}
	if got["sub/notes.txt"] {
		t.Fatalf("results = %v, must not include non-matching notes.txt", res)
	}
}

// TestWalkCatSearch_Paged proves the flat-index cursor pages without repeats.
func TestWalkCatSearch_Paged(t *testing.T) {
	m := newMemFS(ShareSpec{}).(*memFS)
	for _, p := range []string{"hit-a", "hit-b", "hit-c"} {
		f, _ := m.CreateFile(p)
		_ = f.Close()
	}
	crit := CatSearchCriteria{MatchName: true, Partial: true, Name: "hit-", Max: 2}

	page1, next, _ := WalkCatSearch(m, crit, nil)
	if len(page1) != 2 || len(next) == 0 {
		t.Fatalf("page 1 = %d results, next=%v; want 2 + a cursor", len(page1), next)
	}
	page2, next2, _ := WalkCatSearch(m, crit, next)
	if len(next2) != 0 {
		t.Fatalf("page 2 next = %v, want empty (last page)", next2)
	}
	seen := map[string]bool{}
	for _, r := range append(page1, page2...) {
		if seen[r.Path] {
			t.Fatalf("path %q repeated across pages", r.Path)
		}
		seen[r.Path] = true
	}
	for _, want := range []string{"hit-a", "hit-b", "hit-c"} {
		if !seen[want] {
			t.Fatalf("paged walk missing %q (got %v)", want, seen)
		}
	}
}

// TestWalkCatSearch_ParentScope proves the parent predicate restricts matches to
// direct children of one directory.
func TestWalkCatSearch_ParentScope(t *testing.T) {
	m := seedMem(t)
	res, _, _ := WalkCatSearch(m, CatSearchCriteria{MatchParent: true, ParentPath: "sub", Max: 50}, nil)
	for _, r := range res {
		if parentStorePath(r.Path) != "sub" {
			t.Fatalf("result %q is not a direct child of sub", r.Path)
		}
	}
	// Both children of sub (report-feb.txt, notes.txt) must appear; the root file
	// must not.
	got := map[string]bool{}
	for _, r := range res {
		got[r.Path] = true
	}
	if !got["sub/report-feb.txt"] || !got["sub/notes.txt"] {
		t.Fatalf("parent-scoped results = %v, want both children of sub", res)
	}
	if got["report-jan.txt"] {
		t.Fatalf("parent-scoped results = %v, must not include the root file", res)
	}
}

// TestShareFS_CatSearchUnsupported proves a built share whose base FileSystem does
// not implement CatSearcher reports ErrCatSearchUnsupported, so the file service
// can answer "not supported" rather than emulate a search.
func TestShareFS_CatSearchUnsupported(t *testing.T) {
	s := &shareFS{FileSystem: nonSearchingFS{}, ForkEngine: NewNullForkEngine()}
	_, _, err := s.CatSearch(CatSearchCriteria{}, nil)
	if err != ErrCatSearchUnsupported {
		t.Fatalf("CatSearch err = %v, want ErrCatSearchUnsupported", err)
	}
}

// nonSearchingFS is a minimal FileSystem that does NOT implement CatSearcher.
type nonSearchingFS struct{ FileSystem }

func (nonSearchingFS) Capabilities() Capabilities { return Capabilities{} }
