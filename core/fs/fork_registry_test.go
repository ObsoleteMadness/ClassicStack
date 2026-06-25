package fs

import (
	"errors"
	"testing"
)

// TestForkAdapterRegistry_BuiltinsResolve proves every built-in fork-adapter name the
// switch used to handle resolves to a non-nil engine through the registry — the
// no-behaviour-change guarantee of the switch→registry refactor — and that an unknown
// name is still a hard error.
func TestForkAdapterRegistry_BuiltinsResolve(t *testing.T) {
	base := newMemFS(ShareSpec{})
	for _, name := range []string{
		"appledouble", "auto", "native", // AppleDouble family (native aliases it pre-phase4)
		"ads", "xattr", // host-stream layouts
		"nofork", "null", "none", // explicit no-forks + legacy aliases
	} {
		eng, err := forkAdapterByName(name, ShareSpec{}, base)
		if err != nil {
			t.Fatalf("forkAdapterByName(%q): unexpected error %v", name, err)
		}
		if eng == nil {
			t.Fatalf("forkAdapterByName(%q): nil engine", name)
		}
	}

	// Case-insensitive (the registry lower-cases names).
	if _, err := forkAdapterByName("AppleDouble", ShareSpec{}, base); err != nil {
		t.Fatalf("forkAdapterByName is not case-insensitive: %v", err)
	}

	// Unknown name is a hard error, not a silent fallback.
	if _, err := forkAdapterByName("no-such-fork", ShareSpec{}, base); err == nil {
		t.Fatal("forkAdapterByName(unknown): expected error, got nil")
	}
}

// TestForkAdapterRegistry_RoundTrip proves a freshly registered adapter is resolvable by
// name and that the factory receives the base FS.
func TestForkAdapterRegistry_RoundTrip(t *testing.T) {
	const name = "test-fork-roundtrip"
	sentinel := errors.New("factory called")
	var gotBase FileSystem
	RegisterForkAdapter(name, func(spec ShareSpec, base FileSystem) (ForkEngine, error) {
		_ = spec
		gotBase = base
		return nil, sentinel
	})

	base := newMemFS(ShareSpec{})
	_, err := forkAdapterByName(name, ShareSpec{}, base)
	if !errors.Is(err, sentinel) {
		t.Fatalf("forkAdapterByName(%q) err = %v, want sentinel", name, err)
	}
	if gotBase != base {
		t.Fatal("factory did not receive the base FileSystem")
	}
}

// TestNoForkAdapterIsInert proves the "nofork" adapter carries no metadata: forks are
// absent, Finder info / comments read empty, and metadata move/delete are no-ops.
func TestNoForkAdapterIsInert(t *testing.T) {
	eng := NewNoForkAdapter()
	if _, _, err := eng.ReadFinderInfo("x"); err != nil {
		t.Fatalf("ReadFinderInfo: %v", err)
	}
	if info, ok, _ := eng.ReadFinderInfo("x"); ok || info != ([32]byte{}) {
		t.Fatalf("nofork ReadFinderInfo present: ok=%v info=%v", ok, info)
	}
	if c, ok := eng.ReadComment("x"); ok || c != nil {
		t.Fatalf("nofork ReadComment present: ok=%v c=%v", ok, c)
	}
	if err := eng.MoveMetadata("a", "b"); err != nil {
		t.Fatalf("nofork MoveMetadata: %v", err)
	}
	if err := eng.DeleteMetadata("a"); err != nil {
		t.Fatalf("nofork DeleteMetadata: %v", err)
	}

	// NewNullForkEngine is the deprecated alias and must still yield an inert engine.
	if _, _, err := NewNullForkEngine().ReadFinderInfo("x"); err != nil {
		t.Fatalf("NewNullForkEngine alias: %v", err)
	}
}
