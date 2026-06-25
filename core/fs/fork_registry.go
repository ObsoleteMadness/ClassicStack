package fs

import (
	"errors"
	"strings"
	"sync"
)

// fork_registry.go is the fork-adapter registry: the seam that lets a resource-fork
// backend self-register by name instead of being hardcoded in a switch. It mirrors the
// FileSystem factory registry (RegisterFS / fsFactories) so the storage seam is uniform
// across its swappable parts (spec/16-storage-seam.md §9). A fork adapter is MANDATORY
// for every share: BuildShare always resolves exactly one through forkAdapterByName, and
// the "nofork" adapter is the explicit "this share carries no resource forks" choice —
// there is no implicit null fallback. The built-in adapters register themselves from
// init() in their own files (appledouble/nofork here-adjacent in fork.go, ads in
// fork_ads.go, xattr in fork_xattr.go); a host-native adapter (Phase 4 "native") will
// self-register from the adapter/ ring under a build tag, exactly like an fs backend.

// ForkAdapterFactory builds a fork ForkEngine layered over a share's base FileSystem.
// The base is fork-unaware (plain bytes + paths); the adapter is the single place that
// knows resource forks / Finder metadata exist and where their container lives.
type ForkAdapterFactory func(base FileSystem) (ForkEngine, error)

var (
	forkAdapterMu sync.RWMutex
	forkAdapters  = map[string]ForkAdapterFactory{}
)

// RegisterForkAdapter registers a fork-adapter factory under name (case-insensitive).
// Called from init() in each adapter's file so the set of adapters is the set that is
// linked into the build — a build excluding an adapter excludes its name. A later
// registration of the same name overrides the earlier (the last init wins), matching
// RegisterFS.
func RegisterForkAdapter(name string, f ForkAdapterFactory) {
	forkAdapterMu.Lock()
	defer forkAdapterMu.Unlock()
	forkAdapters[strings.ToLower(name)] = f
}

// forkAdapterByName resolves the registered fork adapter for name over base, or an
// "unknown fork backend" error when no adapter registered under that name (so a
// mistyped or unlinked backend fails the share build loudly). An empty name is the
// caller's responsibility to default before calling (withDefaults sets "appledouble").
func forkAdapterByName(name string, base FileSystem) (ForkEngine, error) {
	forkAdapterMu.RLock()
	f, ok := forkAdapters[strings.ToLower(name)]
	forkAdapterMu.RUnlock()
	if !ok {
		return nil, errors.New("fs: unknown fork backend")
	}
	return f(base)
}
