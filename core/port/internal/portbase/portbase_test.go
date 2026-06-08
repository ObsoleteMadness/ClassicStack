package portbase

import (
	"context"
	"errors"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
)

func TestLifecycleIdempotent(t *testing.T) {
	p := New(&Section{SKey: "T", IsEnabled: true, Iface: "eth0"}, nil, nil)
	ctx := context.Background()

	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start (idempotent): %v", err)
	}
	if err := p.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	// Stop after stop is safe.
	if err := p.Stop(ctx); err != nil {
		t.Fatalf("Stop (repeat): %v", err)
	}
}

func TestCapabilities(t *testing.T) {
	p := New(&Section{SKey: "T", IsEnabled: true, Iface: ":548"}, nil, nil)

	if !p.Enabled() {
		t.Error("Enabled() = false, want true")
	}
	if got := p.Binding(); got != ":548" {
		t.Errorf("Binding() = %q, want %q", got, ":548")
	}
	if _, ok := p.Stats().Counters["frames_rx"]; !ok {
		t.Error("Stats() missing frames_rx counter")
	}
	// Metered: storing an observer must not panic; placeholder never invokes it.
	p.SetTrafficObserver(func(rx, tx int) {})
}

func TestApplyConfigLive(t *testing.T) {
	p := New(&Section{SKey: "T", IsEnabled: false, Iface: "eth0"}, nil, nil)
	// Same iface, enabled flag flipped → hot-apply, no restart needed.
	err := p.ApplyConfig(&Section{SKey: "T", IsEnabled: true, Iface: "eth0"})
	if err != nil {
		t.Fatalf("ApplyConfig (live): %v", err)
	}
	if !p.Enabled() {
		t.Error("enabled flag not applied live")
	}
}

func TestApplyConfigNeedsRestart(t *testing.T) {
	p := New(&Section{SKey: "T", Iface: "eth0"}, nil, nil)
	// Binding change is structural → ErrNeedsRestart.
	err := p.ApplyConfig(&Section{SKey: "T", Iface: "eth1"})
	if !errors.Is(err, component.ErrNeedsRestart) {
		t.Fatalf("ApplyConfig binding change err = %v, want ErrNeedsRestart", err)
	}
}

func TestSectionRoundTripsModelKey(t *testing.T) {
	s := &Section{SKey: "EtherTalk", Iface: "eth0"}
	cp := s.Clone()
	if cp.Key() != "EtherTalk" {
		t.Errorf("Clone Key() = %q", cp.Key())
	}
	// Clone is independent.
	cp.(*Section).Iface = "eth9"
	if s.Iface == "eth9" {
		t.Error("Clone did not deep-copy")
	}
}
