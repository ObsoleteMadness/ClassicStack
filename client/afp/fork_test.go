package afp

import "testing"

func TestForkReadWantCapsToKnownSize(t *testing.T) {
	// FUSE typically asks for 4096; a 100-byte fork must request 100 so the
	// ATP bitmap is one packet, not a full quantum.
	if got := forkReadWant(4096, 0, 100, true); got != 100 {
		t.Fatalf("short file: got %d, want 100", got)
	}
	if got := forkReadWant(4096, 100, 100, true); got != 0 {
		t.Fatalf("at EOF: got %d, want 0", got)
	}
	if got := forkReadWant(4096, 0, 0, false); got != 4096 {
		t.Fatalf("unknown size: got %d, want 4096", got)
	}
	if got := forkReadWant(50, 0, 10_000, true); got != 50 {
		t.Fatalf("small buffer: got %d, want 50", got)
	}
	if got := forkReadWant(maxForkIO+1, 0, 10_000, true); got != maxForkIO {
		t.Fatalf("quantum cap: got %d, want %d", got, maxForkIO)
	}
}
