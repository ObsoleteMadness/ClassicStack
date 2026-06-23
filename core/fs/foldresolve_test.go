package fs

import "testing"

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
