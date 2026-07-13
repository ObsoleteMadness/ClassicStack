package share

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// buildCoordShare assembles a real appledouble+memfs ForkFS with a deriving name engine,
// so the reactor's coordination (shortname re-derive + container paths) has something to
// act on.
func buildCoordShare(t *testing.T) fs.ForkFS {
	t.Helper()
	ffs, err := fs.BuildShare(fs.ShareSpec{
		FSType:      "memfs",
		ForkBackend: fs.ForkAppleDoubleDefault,
		MetaBackend: "metastore",
	}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	return ffs
}

// TestMetadataPathsFor proves the reactor surfaces the fork adapter's sidecar container
// for a host path under the share root (host→store conversion + ForkContainers), and
// nil for a path outside the share or a share without an FS.
func TestMetadataPathsFor(t *testing.T) {
	ffs := buildCoordShare(t)
	np := NamedPath{Name: "Vol", Root: "/srv/vol", FS: ffs}

	got := MetadataPathsFor(np, "/srv/vol/dir/report")
	if len(got) != 1 || got[0] != "dir/._report" {
		t.Fatalf("MetadataPathsFor = %v, want [dir/._report]", got)
	}

	// Path outside the share root -> nil.
	if got := MetadataPathsFor(np, "/other/place/file"); got != nil {
		t.Fatalf("MetadataPathsFor(outside) = %v, want nil", got)
	}

	// No FS -> nil (still safe).
	if got := MetadataPathsFor(NamedPath{Name: "Vol", Root: "/srv/vol"}, "/srv/vol/x"); got != nil {
		t.Fatalf("MetadataPathsFor(no FS) = %v, want nil", got)
	}
}

// TestReactorCoordinate_ReDerivesShortnameOnForeignRename proves a foreign rename under a
// shared root makes the peer's NameEngine produce a stable shortname for the NEW name —
// the coordination the §10d reactor performs (wire push still deferred). It calls
// coordinate directly (deterministic) rather than racing the async loop.
func TestReactorCoordinate_ReDerivesShortnameOnForeignRename(t *testing.T) {
	ffs := buildCoordShare(t)
	np := NamedPath{Name: "Vol", Root: "/srv/vol", FS: ffs}
	r := NewReactor("afp", func() []NamedPath { return []NamedPath{np} }, nil)

	// A long name with no prior mapping: before coordination the engine has not bound it.
	me := ffs.Meta()

	// Simulate SMB renaming "dir/old.txt" -> "dir/a-very-long-new-name.txt" on the shared
	// host path; the AFP reactor coordinates.
	ev := fs.Event{
		Op:       fs.OpRename,
		OldPath:  "/srv/vol/dir/old.txt",
		HostPath: "/srv/vol/dir/a-very-long-new-name.txt",
		Origin:   "smb",
	}
	r.coordinate(np, ev)

	// The new name now has a derived shortname bound (idempotent + stable on re-lookup).
	first := me.ShortName("dir", "a-very-long-new-name.txt")
	second := me.ShortName("dir", "a-very-long-new-name.txt")
	if first == "" || first != second {
		t.Fatalf("shortname not stable after coordinate: %q vs %q", first, second)
	}
	// A DOS 8.3 shortname is at most 12 chars (8 + dot + 3) — proving it derived, not
	// passed the long name through.
	if len(first) > 12 {
		t.Fatalf("shortname %q not 8.3-derived (len %d)", first, len(first))
	}
}

// TestReactorCoordinate_NilFSIsNoOp proves coordination is safe when a NamedPath carries
// no FS (path-only matching still works elsewhere).
func TestReactorCoordinate_NilFSIsNoOp(t *testing.T) {
	r := NewReactor("afp", func() []NamedPath { return nil }, nil)
	// Must not panic.
	r.coordinate(NamedPath{Name: "Vol", Root: "/srv/vol"}, fs.Event{Op: fs.OpRename, HostPath: "/srv/vol/x"})
}
