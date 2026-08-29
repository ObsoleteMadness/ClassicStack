package smb

import "testing"

func TestWildcard_DOS83QuestionMarks(t *testing.T) {
	cases := []struct {
		name, pat string
		want      bool
	}{
		{"SUBA", "????????.???", true}, // dotless dir, WfW browse pattern
		{"FILE1.TXT", "????????.???", true},
		{"README", "????????.???", true},
		{"A.B", "????????.???", true},
		{"FILE1.TXT", "*.*", true},
		{"SUBA", "*.*", true},
		{"FILE1.TXT", "*.TXT", true},
		{"FILE1.DOC", "*.TXT", false},
	}
	for _, c := range cases {
		if got := wildcardMatch(c.name, c.pat); got != c.want {
			t.Errorf("wildcardMatch(%q, %q) = %v, want %v", c.name, c.pat, got, c.want)
		}
	}
}
