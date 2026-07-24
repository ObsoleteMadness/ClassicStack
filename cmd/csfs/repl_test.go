package main

import (
	"reflect"
	"testing"
)

// TestSplitArgs proves the REPL tokeniser honours quoted spans so a path with spaces is
// one argument (the bug: strings.Fields split cd "My Folder" into two args).
func TestSplitArgs(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{`ls`, []string{"ls"}},
		{`cd Docs`, []string{"cd", "Docs"}},
		{`cd "My Folder"`, []string{"cd", "My Folder"}},
		{`cp "a b" "c d"`, []string{"cp", "a b", "c d"}},
		{`cd 'single quoted'`, []string{"cd", "single quoted"}},
		{`cd My\ Folder`, []string{"cd", "My Folder"}},
		{`get "/Vol/a b.txt" ./out`, []string{"get", "/Vol/a b.txt", "./out"}},
		{`  cd   Docs  `, []string{"cd", "Docs"}}, // collapses runs of whitespace
		{`cd ""`, []string{"cd", ""}},             // an empty quoted arg is preserved
	}
	for _, c := range cases {
		got, err := splitArgs(c.in)
		if err != nil {
			t.Errorf("splitArgs(%q) error: %v", c.in, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("splitArgs(%q) = %#v, want %#v", c.in, got, c.want)
		}
	}
}

// TestSplitArgsUnterminated rejects an unterminated quote rather than truncating.
func TestSplitArgsUnterminated(t *testing.T) {
	if _, err := splitArgs(`cd "My Folder`); err == nil {
		t.Error(`splitArgs("cd \"My Folder") = nil error, want an unterminated-quote error`)
	}
}

// TestResolveREPLPath checks cwd-relative, absolute, and ".." resolution — a quoted path
// with spaces flows through here after splitArgs, so a space must survive intact.
func TestResolveREPLPath(t *testing.T) {
	cases := []struct{ cwd, arg, want string }{
		{"", "Docs", "Docs"},
		{"Docs", "Sub", "Docs/Sub"},
		{"Docs/Sub", "..", "Docs"},
		{"Docs", "/Other", "Other"},
		{"Docs", "My Folder", "Docs/My Folder"},
		{"A B", "C D", "A B/C D"},
	}
	for _, c := range cases {
		if got := resolveREPLPath(c.cwd, c.arg); got != c.want {
			t.Errorf("resolveREPLPath(%q,%q) = %q, want %q", c.cwd, c.arg, got, c.want)
		}
	}
}
