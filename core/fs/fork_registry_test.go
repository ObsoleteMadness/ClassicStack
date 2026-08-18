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
		"appledouble", "auto", // AppleDouble default + alias
		"appledouble-default", "appledouble-osxzip", "appledouble-dir", // per-layout variants
		"ads", "xattr", // host-stream layouts (ads over memfs simulates streams as keys)
		"applesingle", "macbinary", // single-container backends
		"derez",                  // rdump/idump text sidecars (macresources)
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

	// "native" is a per-OS alias (windows→ads, darwin→hfs, linux→xattr — fork_native.go).
	// It must always RESOLVE (never "unknown fork backend"); whether it then succeeds or
	// errors over this memfs base is platform-dependent (xattr succeeds over any base;
	// ads/hfs need a host-backed NTFS/HFS volume), so we only assert it is registered.
	if _, err := forkAdapterByName("native", ShareSpec{}, base); err != nil && err.Error() == "fs: unknown fork backend" {
		t.Fatal("forkAdapterByName(native): not registered (unknown fork backend)")
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

func TestForkBackendsOmitsAliases(t *testing.T) {
	got := ForkBackends()
	if len(got) == 0 {
		t.Fatal("ForkBackends: empty")
	}
	hidden := map[string]struct{}{"auto": {}, "appledouble-default": {}, "null": {}, "none": {}}
	seen := map[string]bool{}
	for _, name := range got {
		if _, hide := hidden[name]; hide {
			t.Errorf("ForkBackends listed alias %q", name)
		}
		if seen[name] {
			t.Errorf("ForkBackends duplicate %q", name)
		}
		seen[name] = true
	}
	if !seen["appledouble"] {
		t.Error("ForkBackends missing canonical appledouble")
	}
	if !seen["nofork"] {
		t.Error("ForkBackends missing canonical nofork")
	}
}
