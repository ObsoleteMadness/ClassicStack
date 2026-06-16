package registry

import (
	"sort"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

// BuildContext carries everything a factory needs to build a FULLY-WIRED component:
// the config model plus the shared collaborators a component binds to (§14). It
// replaces the bare *config.Model the factory used to receive — that signature
// could only build inert/unrouted components, which is why ports came up with a nil
// router and the macip factory returned a placeholder. The compose runtime root
// populates the collaborators (building the shared Router first) and hands one
// BuildContext to every factory, so a port/service is born already bound to the
// router rather than wired up afterwards through setters.
//
// A field is nil when its collaborator is not available in this build/config (e.g.
// Router is nil if the router component is not registered, or for a unit test that
// builds one component in isolation). A factory must tolerate a nil collaborator by
// building the inert/standalone form — the same graceful degradation the model-only
// path had.
type BuildContext struct {
	// Model is the shared, editable config model. Always set.
	Model *config.Model
	// Router is the shared AppleTalk router instance every DDP port and service
	// binds to (ports via router.Router, services via router.ServiceRouter +
	// RegisterService). nil when the router is not in this build. The runtime root
	// builds it before any dependent factory so this is populated for them.
	Router *router.RouterImpl
	// Telemetry is the bus stats/state/log are published on. May be nil.
	Telemetry bus.Bus
}

// Factory builds a fully-wired component from its BuildContext. Returns the
// component or an error; a disabled section yields (nil, nil).
type Factory func(*BuildContext) (component.Component, error)

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

// Build constructs the named component from the context. ok=false means the name was never
// registered (a clean not-found, NOT an error — the caller logs "requested but not built").
// A registered factory that returns (nil, nil) for a disabled section yields (nil, true, nil).
func Build(name string, ctx *BuildContext) (component.Component, bool, error) {
	mu.RLock()
	f, ok := factories[name]
	mu.RUnlock()
	if !ok {
		return nil, false, nil
	}
	c, err := f(ctx)
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
