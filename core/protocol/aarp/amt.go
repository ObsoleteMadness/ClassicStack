package aarp

// amt.go is the Address Mapping Table: AARP's cache of protocol-address → hardware-
// address mappings (Inside AppleTalk ch.2). It is pure and timer-free — the adapter
// drives aging by calling Age(now) on a ticker, passing UnixNano. Two aging methods the
// spec allows are both implemented: timer-based eviction (Age) and probe-triggered
// deletion (Delete, called when an inbound Probe is seen for a mapped address). Mappings
// are gleaned ONLY from Request/Reply traffic, never from a Probe (whose source address
// is tentative and unreliable).

// DefaultTTL is how long an AMT entry survives without being confirmed/updated before
// Age evicts it. The spec leaves the value to the implementation; a minute is the
// conventional AppleTalk choice and is long enough that a chatty peer keeps its entry
// fresh by gleaning.
const DefaultTTL int64 = 60 * 1_000_000_000 // 60s in nanoseconds

// DefaultMaxEntries bounds the table; on overflow the least-recently-confirmed entry is
// purged (the spec's "some type of least-recently-used algorithm").
const DefaultMaxEntries = 256

type amtEntry struct {
	hw   [6]byte
	seen int64 // UnixNano of the last confirm/update; the LRU + TTL key
}

// AMT maps an AppleTalk protocol address to a hardware address. The zero value is not
// usable; build one with NewAMT.
type AMT struct {
	entries    map[ProtoAddr]amtEntry
	ttl        int64
	maxEntries int
}

// NewAMT builds an empty table with the default TTL and capacity. Pass ttl<=0 or
// maxEntries<=0 to take the defaults.
func NewAMT(ttl int64, maxEntries int) *AMT {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	if maxEntries <= 0 {
		maxEntries = DefaultMaxEntries
	}
	return &AMT{entries: make(map[ProtoAddr]amtEntry), ttl: ttl, maxEntries: maxEntries}
}

// Lookup returns the hardware address mapped to addr, or ok=false on a miss.
func (t *AMT) Lookup(addr ProtoAddr) (hw [6]byte, ok bool) {
	e, ok := t.entries[addr]
	if !ok {
		return [6]byte{}, false
	}
	return e.hw, true
}

// Glean records (or refreshes) addr→hw at time now. It is the gleaning + confirmation
// path: call it for the SOURCE of every inbound Request/Reply (NOT a Probe). A changed
// mapping overwrites; an unchanged one refreshes the timer. On overflow the
// least-recently-confirmed entry is evicted first.
func (t *AMT) Glean(addr ProtoAddr, hw [6]byte, now int64) {
	if _, exists := t.entries[addr]; !exists && len(t.entries) >= t.maxEntries {
		t.evictLRU()
	}
	t.entries[addr] = amtEntry{hw: hw, seen: now}
}

// Delete removes addr's mapping if present (the probe-triggered aging method: AARP
// deletes an entry when it sees a Probe for that protocol address, since the address may
// be changing owners).
func (t *AMT) Delete(addr ProtoAddr) { delete(t.entries, addr) }

// Age evicts every entry not confirmed within the TTL window ending at now. The adapter
// calls it periodically.
func (t *AMT) Age(now int64) {
	for addr, e := range t.entries {
		if now-e.seen >= t.ttl {
			delete(t.entries, addr)
		}
	}
}

// Len reports the number of live entries (diagnostics/tests).
func (t *AMT) Len() int { return len(t.entries) }

// Entry is one AMT mapping in snapshot form: the AppleTalk protocol address, its
// hardware address, and the UnixNano of the last confirm/glean (so a diagnostic can show
// freshness). It is the unit Entries returns.
type Entry struct {
	Addr ProtoAddr
	HW   [6]byte
	Seen int64 // UnixNano of the last confirm/update
}

// Entries returns a snapshot of every live mapping (diagnostics). The order is
// unspecified (map iteration) — the caller sorts for display. It copies, so the returned
// slice is safe to retain while the table mutates.
func (t *AMT) Entries() []Entry {
	out := make([]Entry, 0, len(t.entries))
	for addr, e := range t.entries {
		out = append(out, Entry{Addr: addr, HW: e.hw, Seen: e.seen})
	}
	return out
}

// evictLRU removes the single least-recently-confirmed entry.
func (t *AMT) evictLRU() {
	var oldest ProtoAddr
	var oldestSeen int64
	first := true
	for addr, e := range t.entries {
		if first || e.seen < oldestSeen {
			oldest, oldestSeen, first = addr, e.seen, false
		}
	}
	if !first {
		delete(t.entries, oldest)
	}
}
