package component

import (
	"context"
	"errors"
	"testing"
)

// noopComponent is a minimal Component that also implements a few optional
// capabilities, used to prove the interfaces are satisfiable as written.
type noopComponent struct{ started bool }

func (c *noopComponent) Name() string                { return "noop" }
func (c *noopComponent) Start(context.Context) error { c.started = true; return nil }
func (c *noopComponent) Stop(context.Context) error  { c.started = false; return nil }
func (c *noopComponent) Enabled() bool               { return true }
func (c *noopComponent) Binding() string             { return ":0" }
func (c *noopComponent) Stats() Stats                { return Stats{} }
func (c *noopComponent) ApplyConfig(any) error       { return ErrNeedsRestart }

// Compile-time interface assertions (the core of the B1 acceptance check).
var (
	_ Component    = (*noopComponent)(nil)
	_ Enableable   = (*noopComponent)(nil)
	_ Bindable     = (*noopComponent)(nil)
	_ Statful      = (*noopComponent)(nil)
	_ Configurable = (*noopComponent)(nil)
)

func TestCapabilityAssertion(t *testing.T) {
	var c Component = &noopComponent{}

	// A caller discovers an optional capability via type assertion (the §3 pattern).
	if b, ok := c.(Bindable); !ok || b.Binding() != ":0" {
		t.Fatalf("expected noopComponent to be Bindable with binding :0")
	}
	if _, ok := c.(Metered); ok {
		t.Fatalf("noopComponent does not implement Metered; assertion must fail")
	}
}

func TestApplyConfigNeedsRestartSentinel(t *testing.T) {
	var c Configurable = &noopComponent{}
	if err := c.ApplyConfig(nil); !errors.Is(err, ErrNeedsRestart) {
		t.Fatalf("ApplyConfig should return ErrNeedsRestart, got %v", err)
	}
}

func TestStartStopIdempotentShape(t *testing.T) {
	c := &noopComponent{}
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Stop after Start, then Stop again must be safe.
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop must be safe: %v", err)
	}
}
