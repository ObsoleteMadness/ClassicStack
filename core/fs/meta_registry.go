package fs

import (
	"errors"
	"strings"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
)

// meta_registry.go is the MetaEngine registry, mirroring fork_registry.go so the
// storage seam's two mandatory per-share engines (ForkEngine, MetaEngine) are
// resolved the same way. A MetaEngine backend is MANDATORY for every share:
// BuildShare always resolves exactly one through metaEngineByName, defaulted per
// host platform by withDefaults — there is no null/off state, since name
// derivation must always work for a DOS client. The built-in backends register
// themselves from init() in their own files (meta_store.go, meta_xattr.go,
// meta_ads.go).

// MetaEngineFactory builds a MetaEngine layered over a share's base FileSystem
// and its (already-opened) metastore.Store. A backend that needs no store
// (a platform-native one) ignores it; the metastore-backed fallback is the only
// built-in that reads it.
type MetaEngineFactory func(spec ShareSpec, base FileSystem, store metastore.Store) (MetaEngine, error)

var (
	metaEngineMu    sync.RWMutex
	metaEngineAdaps = map[string]MetaEngineFactory{}
)

// RegisterMetaEngine registers a MetaEngine factory under name (case-insensitive).
// Called from init() in each backend's file so the set of available backends is
// the set linked into the build. A later registration of the same name overrides
// the earlier (the last init wins), matching RegisterForkAdapter.
func RegisterMetaEngine(name string, f MetaEngineFactory) {
	metaEngineMu.Lock()
	defer metaEngineMu.Unlock()
	metaEngineAdaps[strings.ToLower(name)] = f
}

// metaEngineByName resolves the registered MetaEngine backend for name over
// base/store, or an "unknown meta backend" error when no backend is registered
// under that name. An empty name is the caller's responsibility to default
// first (withDefaults picks the per-platform default).
func metaEngineByName(name string, spec ShareSpec, base FileSystem, store metastore.Store) (MetaEngine, error) {
	metaEngineMu.RLock()
	f, ok := metaEngineAdaps[strings.ToLower(name)]
	metaEngineMu.RUnlock()
	if !ok {
		return nil, errors.New("fs: unknown meta backend")
	}
	return f(spec, base, store)
}
