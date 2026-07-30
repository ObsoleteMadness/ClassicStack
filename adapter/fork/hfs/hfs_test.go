//go:build darwin

package hfs

import (
	"errors"
	"os"
	"testing"

	corefs "github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// TestHFS_RequiresHostPather proves the "hfs" adapter rejects a non-host-backed base
// FileSystem: building a memfs share with fork_backend="hfs" fails with ErrNoHostPath.
func TestHFS_RequiresHostPather(t *testing.T) {
	_, err := corefs.BuildShare(corefs.ShareSpec{FSType: "memfs", ForkBackend: "hfs"}, nil)
	if err == nil {
		t.Fatal("hfs over memfs: expected error, got nil")
	}
	if !errors.Is(err, ErrNoHostPath) {
		t.Fatalf("hfs over memfs err = %v, want ErrNoHostPath", err)
	}
}

// TestHFS_OverLocalFS builds an hfs share over a real host directory (local_fs is a
// HostPather) and exercises the resource fork. On darwin the "<file>/..namedfork/rsrc"
// stream is a real HFS+/APFS facility, so a data-only file cleanly reports an absent
// resource fork (len 0, no error).
func TestHFS_OverLocalFS(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(root+"/doc", []byte("data fork via host"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ffs, err := corefs.BuildShare(corefs.ShareSpec{
		FSType:      "local_fs",
		Path:        root,
		ForkBackend: "hfs",
	}, nil)
	if err != nil {
		t.Fatalf("BuildShare local_fs+hfs: %v", err)
	}

	// Data fork is the plain host file.
	n, err := ffs.ForkLen("doc", corefs.DataFork)
	if err != nil {
		t.Fatalf("ForkLen(data): %v", err)
	}
	if n != int64(len("data fork via host")) {
		t.Fatalf("data fork len = %d, want %d", n, len("data fork via host"))
	}

	// A data-only file reports an absent resource fork (len 0, no error) on HFS+/APFS.
	if _, err := ffs.ForkLen("doc", corefs.ResourceFork); err != nil {
		t.Fatalf("ForkLen(resource) on data-only file: %v", err)
	}

	// MetadataPaths is nil: hfs forks ride with the host file.
	if fc, ok := ffs.(corefs.ForkContainers); ok {
		if mp := fc.MetadataPaths("doc"); mp != nil {
			t.Fatalf("hfs MetadataPaths = %v, want nil", mp)
		}
	}
}
