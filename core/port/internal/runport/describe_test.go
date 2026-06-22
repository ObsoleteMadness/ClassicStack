package runport

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/port"
)

// TestPortDescribable verifies the dashboard Describable surface: Kind is "port" and
// Props reports the AppleTalk seed range + zone (so the dashboard groups the component
// under Transports and shows what it seeds without opening config).
func TestPortDescribable(t *testing.T) {
	// A seeded extended-network EtherTalk port.
	p := New(&port.Section{SKey: "EtherTalk", SeedNetwork: 100, SeedNetworkEnd: 110, SeedZone: "Engineering"}, nil, nil, nil)
	if p.Kind() != "port" {
		t.Errorf("Kind() = %q, want port", p.Kind())
	}
	props := p.Props()
	if props["seed network"] != "100–110" {
		t.Errorf("seed network = %q, want 100–110", props["seed network"])
	}
	if props["zone"] != "Engineering" {
		t.Errorf("zone = %q, want Engineering", props["zone"])
	}

	// A single-number seed reports just the number (no range).
	single := New(&port.Section{SKey: "LToUDP", SeedNetwork: 7}, nil, nil, nil)
	if got := single.Props()["seed network"]; got != "7" {
		t.Errorf("single seed = %q, want 7", got)
	}

	// A non-seed port reports "non-seed" and no zone.
	non := New(&port.Section{SKey: "TashTalk"}, nil, nil, nil)
	np := non.Props()
	if _, ok := np["seed network"]; ok {
		t.Errorf("non-seed port should not report a seed network: %v", np)
	}
	if np["seed"] == "" {
		t.Errorf("non-seed port should report a seed=non-seed note: %v", np)
	}
}
