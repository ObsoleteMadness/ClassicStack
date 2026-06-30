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

// TestAMTEntries proves Entries snapshots every live mapping with its address, hardware
// address and confirm time, and returns a copy decoupled from the table.
func TestAMTEntries(t *testing.T) {
	amt := NewAMT(0, 0)
	if got := amt.Entries(); len(got) != 0 {
		t.Fatalf("empty table Entries = %d, want 0", len(got))
	}
	a := ProtoAddr{Network: 1, Node: 2}
	b := ProtoAddr{Network: 3, Node: 4}
	amt.Glean(a, mac(0xAA), 5*sec)
	amt.Glean(b, mac(0xBB), 6*sec)

	got := amt.Entries()
	if len(got) != 2 {
		t.Fatalf("Entries = %d, want 2", len(got))
	}
	byAddr := map[ProtoAddr]Entry{}
	for _, e := range got {
		byAddr[e.Addr] = e
	}
	if e := byAddr[a]; e.HW != mac(0xAA) || e.Seen != 5*sec {
		t.Fatalf("entry a = %+v, want HW=%v Seen=%d", e, mac(0xAA), 5*sec)
	}
	if e := byAddr[b]; e.HW != mac(0xBB) || e.Seen != 6*sec {
		t.Fatalf("entry b = %+v, want HW=%v Seen=%d", e, mac(0xBB), 6*sec)
	}
	// Snapshot is a copy: deleting from the table leaves the slice intact.
	amt.Delete(a)
	if len(got) != 2 {
		t.Fatal("Entries snapshot mutated by a later Delete")
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
