package fuse

import (
	"fmt"
	"runtime"
	"testing"
)

func TestEscapeFuseOpt(t *testing.T) {
	if got := escapeFuseOpt("Classic"); got != "Classic" {
		t.Fatalf("plain = %q", got)
	}
	if got := escapeFuseOpt("OpenRetroSCSI 7.5.3"); got != `OpenRetroSCSI\0407.5.3` {
		t.Fatalf("space = %q", got)
	}
	if got := escapeFuseOpt("a,b"); got != `a\,b` {
		t.Fatalf("comma = %q", got)
	}
}

func TestFuseHostArgsEscapesVolname(t *testing.T) {
	got := fuseHostArgs("OpenRetroSCSI 7.5.3")
	if len(got) < 2 || got[0] != "-ofsname=ClassicStack" {
		t.Fatalf("fsname = %q", got)
	}
	if got[1] != `-ovolname=OpenRetroSCSI\0407.5.3` {
		t.Fatalf("volname = %q", got[1])
	}
	if n := fuseIOSize(); n > 0 {
		want := fmt.Sprintf("-oiosize=%d", n)
		if len(got) != 3 || got[2] != want {
			t.Fatalf("got %q, want iosize %q", got, want)
		}
	} else if len(got) != 2 {
		t.Fatalf("got extra args %q", got)
	}
}

func TestFuseIOSizeIsPlatformMinimum(t *testing.T) {
	n := fuseIOSize()
	switch runtime.GOOS {
	case "darwin":
		want := fuseIOSizeDarwinIntel
		if runtime.GOARCH == "arm64" {
			want = fuseIOSizeDarwinARM64
		}
		if n != want {
			t.Fatalf("iosize = %d, want %d", n, want)
		}
	default:
		if n != 0 {
			t.Fatalf("non-darwin iosize = %d, want 0", n)
		}
	}
}
