package registry

import (
	"sort"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

// Factory builds a component from its config section (and whatever deps it resolves from the
// model). Returns the component or an error; a disabled section yields (nil, nil).
type Factory func(m *config.Model) (component.Component, error)

var (
	mu        sync.RWMutex
	factories = map[string]Factory{}
)

// Register records a name->factory mapping. Call from a build-tagged init(): a component whose
// build tag is absent never registers, so the supervisor simply cannot Build it (the §8
// replacement for *_disabled.go). A later Register for the same name replaces the earlier one
// (last wins), allowing a build to override a default.
func Register(name string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	factories[name] = f
}

// Build constructs the named component from the model. ok=false means the name was never
// registered (a clean not-found, NOT an error — the caller logs "requested but not built").
// A registered factory that returns (nil, nil) for a disabled section yields (nil, true, nil).
func Build(name string, m *config.Model) (component.Component, bool, error) {
	mu.RLock()
	f, ok := factories[name]
	mu.RUnlock()
	if !ok {
		return nil, false, nil
	}
	c, err := f(m)
	return c, true, err
}

// Names returns the registered component names, sorted for deterministic iteration.
func Names() []string {
	mu.RLock()
	out := make([]string, 0, len(factories))
	for name := range factories {
		out = append(out, name)
	}
	mu.RUnlock()
	sort.Strings(out)
	return out
}
