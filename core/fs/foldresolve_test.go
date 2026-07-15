package fs

import (
	stdfs "io/fs"
	"testing"
)

// TestResolveFold proves case-insensitive store-path resolution against a real
// memfs tree with mixed-case names: an exact path is returned unchanged, a
// mis-cased path resolves to the stored casing, and a missing leaf keeps the
// requested casing (the create-target case) and reports not-fully-resolved.
func TestResolveFold(t *testing.T) {
	m := newMemFS(ShareSpec{}).(*memFS)
	if err := m.CreateDir("Reports"); err != nil {
		t.Fatalf("CreateDir: %v", err)
	}
	f, err := m.CreateFile("Reports/Q1.TXT")
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	_ = f.Close()

	cases := []struct {
		in       string
		want     string
		resolved bool
	}{
		{"Reports/Q1.TXT", "Reports/Q1.TXT", true},    // exact
		{"REPORTS/q1.txt", "Reports/Q1.TXT", true},    // both components folded
		{"reports/Q1.TXT", "Reports/Q1.TXT", true},    // parent folded
		{"Reports/new.txt", "Reports/new.txt", false}, // leaf missing → create casing kept
		{"MISSING/x", "MISSING/x", false},             // parent missing
		{"", "", true},                                // root
	}
	for _, c := range cases {
		got, ok := ResolveFold(m, c.in)
		if got != c.want || ok != c.resolved {
			t.Errorf("ResolveFold(%q) = (%q,%v), want (%q,%v)", c.in, got, ok, c.want, c.resolved)
		}
	}
}

// statPanicsFS wraps a FileSystem and panics if Stat is ever called — a probe
// proving ResolveFold/foldComponent resolve purely through ReadDir. This
// matters because a "does Stat(path) succeed" fast path is unsound on any
// case-insensitive host filesystem (Windows/NTFS, macOS/APFS): Stat succeeds
// for ANY casing that matches an existing entry there, but Go's os.Stat does
// not correct the returned FileInfo's name back to the real on-disk spelling —
// so a fast path built on "Stat succeeded, so the queried casing IS the stored
// casing" silently returns the CALLER's casing instead. Every metastore-backed
// lookup keyed by the resolved store path (EAs, DOS attributes, CNIDs — plain
// case-sensitive string keys, unlike case-insensitive file I/O) then silently
// misses: OS/2 WPS set a .ICON EA under "1516HBWT.cab", queried it back under
// "1516HBWT.CAB" moments later, and got an empty placeholder instead of the
// value it had just written (netbeui.pcap 2026-07-15 frames 513-522, a real
// Windows local_fs deployment). This probe fails loudly if that fast path is
// ever reintroduced, on any backend, not just a case-insensitive one.
type statPanicsFS struct{ FileSystem }

func (statPanicsFS) Stat(path string) (stdfs.FileInfo, error) {
	panic("ResolveFold/foldComponent must not call Stat — case-insensitive hosts make Stat's success unable to prove the queried casing matches the stored casing; resolve via ReadDir only")
}

// TestResolveFold_NeverCallsStat proves ResolveFold and foldComponent resolve
// every path purely by scanning ReadDir, never by probing Stat — the fix for
// the case-fold regression above.
func TestResolveFold_NeverCallsStat(t *testing.T) {
	m := newMemFS(ShareSpec{}).(*memFS)
	if err := m.CreateDir("Reports"); err != nil {
		t.Fatalf("CreateDir: %v", err)
	}
	f, err := m.CreateFile("Reports/Q1.TXT")
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	_ = f.Close()

	guarded := statPanicsFS{m}
	for _, in := range []string{"Reports/Q1.TXT", "REPORTS/q1.txt", "reports/Q1.TXT", "Reports/new.txt", "MISSING/x", ""} {
		ResolveFold(guarded, in) // must not panic
	}
}
