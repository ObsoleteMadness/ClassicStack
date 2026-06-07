package router

import "testing"

// TestRoutingTableSnapshot verifies Snapshot reports each entry with its RTMP
// aging state: a directly-connected route is "good", and the aging machine
// advances a learned route good → suspect → bad → worst on successive ticks.
func TestRoutingTableSnapshot(t *testing.T) {
	r := newTestRouter()
	p := &fakePort{name: "fake", netMin: 10, netMax: 12}

	// Directly-connected route (Distance 0) is always good.
	r.RoutingTable.SetPortRange(p, 10, 12)
	// A learned route (Distance > 0) ages.
	if !r.RoutingTable.Consider(&RoutingTableEntry{
		NetworkMin: 20, NetworkMax: 20, Distance: 1, Port: p, NextNetwork: 10, NextNode: 2,
	}) {
		t.Fatal("Consider rejected the learned route")
	}

	stateFor := func(netMin uint16) string {
		t.Helper()
		for _, e := range r.RoutingTable.Snapshot() {
			if e.Entry != nil && e.Entry.NetworkMin == netMin {
				return e.State
			}
		}
		t.Fatalf("no snapshot entry for network %d", netMin)
		return ""
	}

	if got := stateFor(10); got != "good" {
		t.Errorf("connected route state = %q, want good", got)
	}
	if got := stateFor(20); got != "good" {
		t.Errorf("fresh learned route state = %q, want good", got)
	}

	// One aging tick demotes a good learned route to suspect; the connected
	// route stays good.
	r.RoutingTable.Age()
	if got := stateFor(20); got != "suspect" {
		t.Errorf("after 1 Age: learned route = %q, want suspect", got)
	}
	if got := stateFor(10); got != "good" {
		t.Errorf("after 1 Age: connected route = %q, want good", got)
	}

	// Further ticks walk suspect → bad → worst.
	r.RoutingTable.Age()
	if got := stateFor(20); got != "bad" {
		t.Errorf("after 2 Age: learned route = %q, want bad", got)
	}
	r.RoutingTable.Age()
	if got := stateFor(20); got != "worst" {
		t.Errorf("after 3 Age: learned route = %q, want worst", got)
	}
}
