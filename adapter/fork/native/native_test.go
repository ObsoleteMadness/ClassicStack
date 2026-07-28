//go:build forknative || all

package native

import (
	"errors"
	"os"
	"testing"

	corefs "github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// TestNative_RequiresHostPather proves the real "native" adapter is registered (it
// replaces the core stub under -tags forknative) and rejects a non-host-backed base
// FileSystem: building a memfs share with fork_backend="native" fails with ErrNoHostPath
// — NOT the core stub's "rebuild with -tags forknative" error.
func TestNative_RequiresHostPather(t *testing.T) {
	_, err := corefs.BuildShare(corefs.ShareSpec{FSType: "memfs", ForkBackend: "native"}, nil)
	if err == nil {
		t.Fatal("native over memfs: expected error, got nil")
	}
	if !errors.Is(err, ErrNoHostPath) {
		t.Fatalf("native over memfs err = %v, want ErrNoHostPath (is the stub still linked?)", err)
	}
}

// TestNative_OverLocalFS builds a native share over a real host directory (local_fs is a
// HostPather) and exercises the resource fork. On a host without native fork support the
// resource stream simply does not exist, so the fork reads back empty — both outcomes are
// valid; the test asserts the engine assembles and a data-only file round-trips.
func TestNative_OverLocalFS(t *testing.T) {
	root := t.TempDir()
	// Seed a plain data file in the share root.
	if err := os.WriteFile(root+"/doc", []byte("data fork via host"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ffs, err := corefs.BuildShare(corefs.ShareSpec{
		FSType:      "local_fs",
		Path:        root,
		ForkBackend: "native",
	}, nil)
	if err != nil {
		t.Fatalf("BuildShare local_fs+native: %v", err)
	}

	// Data fork is the plain host file.
	n, err := ffs.ForkLen("doc", corefs.DataFork)
	if err != nil {
		t.Fatalf("ForkLen(data): %v", err)
	}
	if n != int64(len("data fork via host")) {
		t.Fatalf("data fork len = %d, want %d", n, len("data fork via host"))
	}

	// Resource fork: absent on a host without native forks (len 0, no error) — the
	// engine must not fail just because the host has no resource stream.
	if _, err := ffs.ForkLen("doc", corefs.ResourceFork); err != nil {
		t.Fatalf("ForkLen(resource) on data-only file: %v", err)
	}

	// MetadataPaths is nil: native forks ride with the host file.
	if fc, ok := ffs.(corefs.ForkContainers); ok {
		if mp := fc.MetadataPaths("doc"); mp != nil {
			t.Fatalf("native MetadataPaths = %v, want nil", mp)
		}
	}
}
