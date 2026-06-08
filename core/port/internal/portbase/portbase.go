package portbase

import (
	"context"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

// Section is the typed config one placeholder port carries. It satisfies
// config.Section (§4) so the model can stage/round-trip it and the supervisor
// can hand it back via ApplyConfig. Real ports get richer sections in Phase 2;
// this one carries only the fields the harness exercises.
type Section struct {
	// SKey is the section/component key ("EtherTalk", "LocalTalk", …). It is the
	// value Key() returns, set when the section is constructed so one Section type
	// serves every placeholder port. It is not serialised — the section's table
	// name in config IS the key, and the factory re-sets it on build.
	SKey string `toml:"-"`
	// Iface is the bound interface ("eth0", "ipx:0550", …). Changing it is a
	// structural change (needs restart); it is what Binding() reports.
	Iface string `toml:"iface"`
	// IsEnabled mirrors the configured-enabled flag (≠ running).
	IsEnabled bool `toml:"enabled"`
}

// Key returns the section key (matches the component/registry name).
func (s *Section) Key() string { return s.SKey }

// Clone returns a deep copy so staging never mutates the live section.
func (s *Section) Clone() config.Section {
	cp := *s
	return &cp
}

// Validate checks the section in isolation. A placeholder accepts anything.
func (s *Section) Validate() error { return nil }

// compile-time assertion: *Section satisfies config.Section.
var _ config.Section = (*Section)(nil)

// SectionFromModel resolves the placeholder Section registered under key, falling
// back to a fresh default (with that key) when the model has none. Per-transport
// port packages use it to build from the shared model.
func SectionFromModel(m *config.Model, key string) *Section {
	if m != nil {
		if s, ok := m.Get(key); ok {
			if ps, ok := s.(*Section); ok {
				return ps
			}
		}
	}
	return &Section{SKey: key}
}

// Port is the shared Phase 1 placeholder port. It holds a link (which it never
// reads/writes — the data path is a no-op) and satisfies the lifecycle plus the
// capabilities the supervisor and UI type-assert. Embed it in a per-transport
// package to give that transport a runnable-but-inert component.
type Port struct {
	mu      sync.Mutex
	sec     *Section
	frame   link.FrameLink // optional; nil for kernel-socket-style ports
	logger  log.Logger
	running bool
	observe func(rxBytes, txBytes int)
}

// New builds a placeholder port from a section, an optional frame link, and a
// logger. Either link may be nil in Phase 1 — the data path is inert regardless.
func New(sec *Section, frame link.FrameLink, logger log.Logger) *Port {
	return &Port{sec: sec, frame: frame, logger: logger}
}

// Name returns the component name (the section key).
func (p *Port) Name() string { return p.sec.SKey }

// Start brings the placeholder up. Idempotent (§3): starting a started port is a
// no-op returning nil. The data path stays inert — Phase 2 wires the read loop.
func (p *Port) Start(ctx context.Context) error {
	_ = ctx
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running {
		return nil
	}
	p.running = true
	p.logf("port placeholder started (data path not implemented)")
	return nil
}

// Stop brings the placeholder down. Safe after a failed/partial Start (§3).
func (p *Port) Stop(ctx context.Context) error {
	_ = ctx
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.running {
		return nil
	}
	p.running = false
	p.logf("port placeholder stopped")
	return nil
}

// Enabled reports the configured-enabled flag (≠ running). Capability: Enableable.
func (p *Port) Enabled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sec.IsEnabled
}

// Binding reports the bound interface for the dashboard. Capability: Bindable.
func (p *Port) Binding() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sec.Iface
}

// Stats returns a point-in-time snapshot. Capability: Statful (§5). A placeholder
// reports zeroed counters/gauges so the rate subscriber has something to read.
func (p *Port) Stats() component.Stats {
	return component.Stats{
		Counters: map[string]uint64{
			"frames_rx":     0,
			"frames_tx":     0,
			"decode_errors": 0,
		},
		Gauges: map[string]float64{},
	}
}

// SetTrafficObserver installs the rx/tx byte observer. Capability: Metered (§5).
// A placeholder never invokes it (no data path), but stores it so the contract holds.
func (p *Port) SetTrafficObserver(fn func(rxBytes, txBytes int)) {
	p.mu.Lock()
	p.observe = fn
	p.mu.Unlock()
}

// ApplyConfig hot-applies a new section when it can. Capability: Configurable (§11).
// A placeholder can absorb an enabled-flag change live, but treats an interface
// (binding) change as structural and returns ErrNeedsRestart so the supervisor
// restarts it — exercising both reconfigure paths in the harness.
func (p *Port) ApplyConfig(section any) error {
	sec, ok := section.(*Section)
	if !ok || sec == nil {
		// Nothing typed to apply (e.g. notify pass with nil); absorb it live.
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if sec.Iface != p.sec.Iface {
		return component.ErrNeedsRestart // binding change is structural
	}
	p.sec = sec // enabled-flag change applies live
	p.logf("port placeholder reconfigured live")
	return nil
}

// logf emits one info line through the scoped logger, if present.
func (p *Port) logf(msg string) {
	if p.logger == nil || !p.logger.Enabled(log.Info) {
		return
	}
	p.logger.Log1(log.Info, msg, log.Str("port", p.sec.SKey))
}

// compile-time capability assertions.
var (
	_ component.Component    = (*Port)(nil)
	_ component.Enableable   = (*Port)(nil)
	_ component.Bindable     = (*Port)(nil)
	_ component.Statful      = (*Port)(nil)
	_ component.Metered      = (*Port)(nil)
	_ component.Configurable = (*Port)(nil)
)
