// Package runtime is the compose runtime root: the single assembly the
// interactive binary, the Windows service wrapper, and the Unix daemon all share
// (§14, M9/M10). It re-expresses what the D5 skeleton main did inline — load the
// config model, build every registered component, wire them, and supervise — as a
// reusable Runtime so the three entry points stop each hand-rolling the loop.
//
// Ring: COMPOSE. It imports core/ and may import adapter/, but it does NOT pick a
// config Store or Codec itself: those are injected (Options.Store/Codec) so a
// TOML/file build, a UCI/ubus build, and an in-memory test each choose their own
// adapters at the cmd edge without this package pulling all of them in. The
// registry's build-tagged init()s decide which components exist; this root only
// assembles whatever registered.
//
// What this slice does NOT do (kept for the later M10 cutover, per .refactor/TODO):
// inject REAL device links (the ports still build inert until pcap/framing is wired
// here), flag parsing, and retiring the legacy internal/app. The cross-wiring of
// the runtime data path (service↔router, transport↔service) is the M-ng work; this
// root provides the place that wiring will live (Build → wire) and does the part
// that needs no new seam yet: load, build, dependency-ordered supervise.
package runtime

import (
	"context"
	"fmt"

	"github.com/ObsoleteMadness/ClassicStack/compose/registry"
	"github.com/ObsoleteMadness/ClassicStack/compose/supervisor"
	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

// stubNames are registry entries that are test/skeleton scaffolding, never part of
// a real stack. Build skips them (the D5 main special-cased the same set inline).
var stubNames = map[string]bool{
	"stub-tagged":   true,
	"stub-a":        true,
	"stub-disabled": true,
}

// hardDeps declares the start-order edges between built components (a name must be
// running before the names that list it). Soft transport bindings (IPX/NetBEUI →
// NetBIOS) are component.Attachable side-effects, NOT edges (§11d), so they are not
// here. This static map re-expresses the D5 main's hardcoded edges; a declarative
// per-component dependency capability is a deliberate follow-on (noted in TODO),
// kept out of this slice so the root stays pure assembly.
//
// Only edges whose BOTH ends are actually built take effect — Build filters against
// the components it constructed, so a minimal build (no NetBEUI) simply drops the
// SMB→NetBEUI edge rather than failing.
var hardDeps = map[string][]string{
	"AFP": {"Router"},
	"SMB": {"NetBEUI"},
}

// componentSource enumerates and builds the components a Runtime assembles. The
// production source is the global compose/registry; tests inject their own so they
// neither depend on whatever build-tagged components registered nor pollute the
// global registry singleton.
type componentSource interface {
	Names() []string
	Build(name string, m *config.Model) (component.Component, bool, error)
}

// registrySource adapts the package-level compose/registry to componentSource.
type registrySource struct{}

func (registrySource) Names() []string { return registry.Names() }
func (registrySource) Build(name string, m *config.Model) (component.Component, bool, error) {
	return registry.Build(name, m)
}

// Options configures a Runtime build.
type Options struct {
	// Model is the starting config model. Required. Load() fills one from a
	// Store+Codec; a caller may also hand-build one (tests, flag-derived).
	Model *config.Model
	// Telemetry is the bus state/stats/log are published on. A nil bus disables
	// publication (the supervisor and stats subscriber both tolerate nil).
	Telemetry bus.Bus
	// source enumerates/builds components. nil → the global compose/registry. Set
	// only by tests (kept unexported so the production API is the registry path).
	source componentSource
}

// Runtime is the assembled, not-yet-started stack: the supervisor owning every
// built component in dependency order, plus the shared model and telemetry bus the
// control plane binds to. The entry points call Start/Stop and, for the control
// plane, reach Supervisor()/Model().
type Runtime struct {
	sup       *supervisor.Supervisor
	model     *config.Model
	telemetry bus.Bus
	built     []string // names actually constructed (diagnostics)
}

// Load builds a config.Model from a Store + Codec. A missing store file yields the
// default model (Store.Load returns (nil,nil)); a present one is decoded through the
// codec. It is the read half of the config path the control plane's Save mirrors.
func Load(store config.Store, codec config.Codec) (*config.Model, error) {
	m := config.NewModel()
	data, err := store.Load()
	if err != nil {
		return nil, fmt.Errorf("runtime: load config: %w", err)
	}
	if len(data) == 0 {
		return m, nil // no stored config yet — defaults
	}
	if err := codec.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("runtime: decode config: %w", err)
	}
	return m, nil
}

// Build constructs every registered (non-stub) component from the model, registers
// each with the supervisor under its filtered dependency edges, and returns the
// assembled Runtime. A factory error aborts the whole build (a misconfigured
// component must not be silently dropped). A name that is registered but returns
// (nil, true) for a disabled section is skipped.
func Build(opts Options) (*Runtime, error) {
	if opts.Model == nil {
		return nil, fmt.Errorf("runtime: Build requires a Model")
	}
	src := opts.source
	if src == nil {
		src = registrySource{}
	}
	sup := supervisor.New(opts.Model, opts.Telemetry)

	// First pass: build the components, recording which names actually exist so the
	// dependency edges can be filtered to built-both-ends pairs.
	comps := make(map[string]component.Component)
	var order []string
	for _, name := range src.Names() {
		if stubNames[name] {
			continue
		}
		c, ok, err := src.Build(name, opts.Model)
		if err != nil {
			return nil, fmt.Errorf("runtime: build %q: %w", name, err)
		}
		if !ok || c == nil {
			continue // not in this build, or disabled section
		}
		comps[name] = c
		order = append(order, name)
	}

	// Second pass: register with the supervisor under filtered edges (only edges
	// whose dependency is also built).
	for _, name := range order {
		deps := builtDeps(name, comps)
		sup.Add(comps[name], deps)
	}

	return &Runtime{
		sup:       sup,
		model:     opts.Model,
		telemetry: opts.Telemetry,
		built:     order,
	}, nil
}

// builtDeps returns name's hard dependencies, dropping any whose target was not
// built in this configuration (so a minimal build omits the edge instead of
// failing the topo sort on a missing node).
func builtDeps(name string, comps map[string]component.Component) []string {
	want := hardDeps[name]
	if len(want) == 0 {
		return nil
	}
	out := make([]string, 0, len(want))
	for _, d := range want {
		if _, ok := comps[d]; ok {
			out = append(out, d)
		}
	}
	return out
}

// Start brings the whole stack up in dependency order.
func (r *Runtime) Start(ctx context.Context) error { return r.sup.StartAll(ctx) }

// Stop brings the whole stack down in reverse dependency order.
func (r *Runtime) Stop(ctx context.Context) error { return r.sup.StopAll(ctx) }

// Supervisor returns the supervisor (the control.Supervisor surface the control
// plane drives: Status/Start/Stop/Restart/Reconfigure/Users).
func (r *Runtime) Supervisor() *supervisor.Supervisor { return r.sup }

// Model returns the shared config model (the control plane reads/edits it).
func (r *Runtime) Model() *config.Model { return r.model }

// Built returns the names of the components actually constructed, in build order
// (diagnostics / startup logging).
func (r *Runtime) Built() []string { return append([]string(nil), r.built...) }
