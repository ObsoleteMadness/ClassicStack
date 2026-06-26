package aarp

import "testing"

const sec = int64(1_000_000_000)

// TestAMTGleanAndLookup proves Glean records a mapping and Lookup returns it; an unmapped
// address misses.
func TestAMTGleanAndLookup(t *testing.T) {
	amt := NewAMT(0, 0)
	addr := ProtoAddr{Network: 0x0001, Node: 0x20}
	hw := mac(0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF)

	if _, ok := amt.Lookup(addr); ok {
		t.Fatal("empty AMT returned a hit")
	}
	amt.Glean(addr, hw, 0)
	got, ok := amt.Lookup(addr)
	if !ok || got != hw {
		t.Fatalf("Lookup = %v ok=%v, want %v", got, ok, hw)
	}
}

// TestAMTAgeEvicts proves an entry past the TTL is aged out and a refreshed one survives.
func TestAMTAgeEvicts(t *testing.T) {
	amt := NewAMT(10*sec, 0)
	addr := ProtoAddr{Network: 1, Node: 2}
	amt.Glean(addr, mac(1, 2, 3, 4, 5, 6), 0)

	amt.Age(5 * sec) // within TTL — survives
	if _, ok := amt.Lookup(addr); !ok {
		t.Fatal("entry aged out early")
	}
	// Confirm/refresh at t=8s, then age at t=15s: 15-8=7 < 10, still alive.
	amt.Glean(addr, mac(1, 2, 3, 4, 5, 6), 8*sec)
	amt.Age(15 * sec)
	if _, ok := amt.Lookup(addr); !ok {
		t.Fatal("refreshed entry aged out")
	}
	// Now let it lapse: 30-8=22 >= 10 → evicted.
	amt.Age(30 * sec)
	if _, ok := amt.Lookup(addr); ok {
		t.Fatal("stale entry not aged out")
	}
}

// TestAMTDelete proves the probe-triggered delete removes a mapping.
func TestAMTDelete(t *testing.T) {
	amt := NewAMT(0, 0)
	addr := ProtoAddr{Network: 1, Node: 2}
	amt.Glean(addr, mac(1, 2, 3, 4, 5, 6), 0)
	amt.Delete(addr)
	if _, ok := amt.Lookup(addr); ok {
		t.Fatal("entry present after Delete")
	}
}

// TestAMTLRUEviction proves a full table evicts the least-recently-confirmed entry when a
// new mapping arrives.
func TestAMTLRUEviction(t *testing.T) {
	amt := NewAMT(0, 2) // capacity 2
	a := ProtoAddr{Network: 1, Node: 1}
	b := ProtoAddr{Network: 1, Node: 2}
	c := ProtoAddr{Network: 1, Node: 3}

	amt.Glean(a, mac(1), 1*sec) // oldest
	amt.Glean(b, mac(2), 2*sec)
	amt.Glean(c, mac(3), 3*sec) // overflow → evicts a (LRU)

	if _, ok := amt.Lookup(a); ok {
		t.Fatal("LRU entry a should have been evicted")
	}
	if _, ok := amt.Lookup(b); !ok {
		t.Fatal("b should survive")
	}
	if _, ok := amt.Lookup(c); !ok {
		t.Fatal("c should be present")
	}
	if amt.Len() != 2 {
		t.Fatalf("Len = %d, want 2", amt.Len())
	}
}
