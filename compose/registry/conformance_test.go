package registry

import (
	"context"
	"errors"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
)

// TestComponentConformance implements E1: Component Conformance Harness.
// It verifies that all registered components honour lifecycle contract rules.
func TestComponentConformance(t *testing.T) {
	ctx := context.Background()

	// Get all registered component names
	names := Names()
	if len(names) == 0 {
		t.Skip("No registered components to test")
	}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			if name == "stub-tagged" || name == "stub-a" || name == "stub-disabled" {
				// Skip test-specific stubs from registry_test.go
				return
			}

			// 1. Prepare model
			m := config.NewModel()
			// Ensure it's enabled in the model if it's a port-based component
			m.Set(&port.Section{
				SKey:      name,
				Iface:     "eth0",
				IsEnabled: true,
			})

			// 2. Build component
			c, ok, err := Build(name, &BuildContext{Model: m})
			if err != nil {
				t.Fatalf("Build(%s) returned error: %v", name, err)
			}
			if !ok {
				t.Fatalf("Build(%s) ok = false, want true", name)
			}
			if c == nil {
				// Disabled components are built as nil, but since we enabled it,
				// it should be non-nil.
				t.Fatalf("Build(%s) returned nil component", name)
			}

			// 3. Verify name
			if c.Name() != name {
				t.Errorf("Component Name() = %q, want %q", c.Name(), name)
			}

			// 4. Verify Start -> Stop -> Start idempotency (§3)
			// Start
			if err := c.Start(ctx); err != nil {
				t.Fatalf("Start: %v", err)
			}
			// Start again (must be idempotent, returning nil)
			if err := c.Start(ctx); err != nil {
				t.Errorf("Idempotent Start: %v", err)
			}
			// Stop
			if err := c.Stop(ctx); err != nil {
				t.Fatalf("Stop: %v", err)
			}
			// Stop again (must be safe/idempotent)
			if err := c.Stop(ctx); err != nil {
				t.Errorf("Repeat Stop: %v", err)
			}
			// Start after Stop
			if err := c.Start(ctx); err != nil {
				t.Fatalf("Start after Stop: %v", err)
			}
			// Clean up and stop it
			if err := c.Stop(ctx); err != nil {
				t.Fatalf("Cleanup Stop: %v", err)
			}

			// 5. Verify Stop after partial/failed Start (§3)
			// Stop must be safe to call on stopped/unstarted component
			if err := c.Stop(ctx); err != nil {
				t.Errorf("Stop on unstarted component: %v", err)
			}

			// 6. Test optional capabilities if implemented
			if sf, ok := c.(component.Statful); ok {
				// Stats() must return without panic
				stats := sf.Stats()
				if stats.Counters == nil {
					t.Errorf("Statful returned nil Counters map")
				}
			}

			if en, ok := c.(component.Enableable); ok {
				_ = en.Enabled()
			}

			if bd, ok := c.(component.Bindable); ok {
				_ = bd.Binding()
			}

			if mt, ok := c.(component.Metered); ok {
				// SetTrafficObserver must accept standard observer without panicking
				mt.SetTrafficObserver(func(rx, tx int) {})
			}

			if cf, ok := c.(component.Configurable); ok {
				// Test Configurable hot-apply and restart paths
				// If it's a port, changing binding needs restart, enabled applies live.
				if name == "EtherTalk" || name == "LocalTalk" || name == "IPX" || name == "NetBEUI" {
					// Hot-apply check (same iface, IsEnabled changes)
					secLive := &port.Section{SKey: name, Iface: "eth0", IsEnabled: false}
					if err := cf.ApplyConfig(secLive); err != nil {
						t.Errorf("ApplyConfig live change returned error: %v", err)
					}

					// Restart check (different Iface)
					secRestart := &port.Section{SKey: name, Iface: "eth1", IsEnabled: true}
					err := cf.ApplyConfig(secRestart)
					if !errors.Is(err, component.ErrNeedsRestart) {
						t.Errorf("ApplyConfig binding change error = %v, want component.ErrNeedsRestart", err)
					}
				} else {
					// For other configurable components, verify it accepts nil or its section
					_ = cf.ApplyConfig(nil)
				}
			}
		})
	}
}
