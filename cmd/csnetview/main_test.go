package main

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/client/browse"
)

// csnetview is a thin consumer of client/browse: the solicit / find-master / NetServerEnum2
// sweep is tested in that SDK package (and in client/netbios). Here we only cover the
// tool-local rendering helpers.

func TestSourceLabel(t *testing.T) {
	for src, want := range map[browse.Source]string{
		browse.SourceBrowseList:   "list",
		browse.SourceMaster:       "master",
		browse.SourceAnnouncement: "announce",
	} {
		if got := sourceLabel(src); got != want {
			t.Errorf("sourceLabel(%v) = %q, want %q", src, got, want)
		}
	}
}

func TestRoleComment(t *testing.T) {
	cases := []struct {
		in   browse.Server
		want string
	}{
		{browse.Server{Role: "master browser", Comment: "the boss"}, "master browser — the boss"},
		{browse.Server{Role: "backup browser"}, "backup browser"},
		{browse.Server{Comment: "just a comment"}, "just a comment"},
		{browse.Server{}, ""},
	}
	for _, c := range cases {
		if got := roleComment(c.in); got != c.want {
			t.Errorf("roleComment(%+v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCarriersJoin(t *testing.T) {
	got := carriers([]browse.Protocol{browse.Protocol("nbf"), browse.Protocol("nbipx")})
	if got != "nbf+nbipx" {
		t.Errorf("carriers = %q, want nbf+nbipx", got)
	}
}
