package afp

import "testing"

func TestChildPath(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ dir, name, want string }{
		{"", "Spectre 1.1", "Spectre 1.1"},
		{"/", "Spectre 1.1", "Spectre 1.1"},
		{"StuffIt", "Expander", "StuffIt/Expander"},
		{"/StuffIt/", "Expander", "StuffIt/Expander"},
	} {
		if got := childPath(tc.dir, tc.name); got != tc.want {
			t.Errorf("childPath(%q, %q) = %q, want %q", tc.dir, tc.name, got, tc.want)
		}
	}
}

func TestForkReadWant(t *testing.T) {
	t.Parallel()
	// FUSE 4 KiB read of a 10-byte remaining fork must ask AFP for 10, not a quantum.
	if got := forkReadWant(4096, 0, 10, true); got != 10 {
		t.Errorf("forkReadWant(4096, 0, 10) = %d, want 10", got)
	}
	if got := forkReadWant(4096, 8, 10, true); got != 2 {
		t.Errorf("forkReadWant remaining = %d, want 2", got)
	}
	if got := forkReadWant(4096, 10, 10, true); got != 0 {
		t.Errorf("forkReadWant at EOF = %d, want 0", got)
	}
	if got := forkReadWant(100, 0, 1<<20, true); got != 100 {
		t.Errorf("forkReadWant buffer-limited = %d, want 100", got)
	}
	if got := forkReadWant(maxForkIO+1, 0, 1<<20, true); got != maxForkIO {
		t.Errorf("forkReadWant quantum cap = %d, want %d", got, maxForkIO)
	}
}
