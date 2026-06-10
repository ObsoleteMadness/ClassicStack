// Routing table with the RTMP aging state machine, re-expressed for the core
// ring: it indexes routes by network, ages learned routes Good→Suspect→Bad→
// Worst→removed, and withdraws a port's routes immediately on Detach (§3). It
// holds RoutedPort (not the legacy port.Port) and logs through core/log; the key
// is hand-built (no fmt) to stay reflection-free (§1).
package router

import (
	"strconv"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

// RoutingTableEntry is one route: a network range reachable via Port at Distance
// hops, with the next-hop address for non-directly-connected (Distance>0) routes.
// A Distance-0 entry is directly connected (the Port's own network).
type RoutingTableEntry struct {
	ExtendedNetwork bool
	NetworkMin      uint16
	NetworkMax      uint16
	Distance        uint8
	Port            RoutedPort
	NextNetwork     uint16
	NextNode        uint8
}

// RTMP aging states. An RTMP router ages a learned entry through Good → Suspect →
// Bad → Worst → removed on successive aging ticks; receiving the route again
// resets it to Good. This validity state is RTMP's notion of an entry's "age" —
// there is no wall-clock timestamp. Directly-connected (Distance 0) entries stay
// Good and are removed by membership (Detach), not aging.
const (
	stateGood  = 1
	stateSus   = 2
	stateBad   = 3
	stateWorst = 4
)

// RoutingTable is the router's network→route index plus per-entry aging state.
type RoutingTable struct {
	zit            *ZoneInformationTable
	logger         log.Logger
	mu             sync.RWMutex
	entryByNetwork map[uint16]*RoutingTableEntry
	stateByKey     map[string]int
	entryByKey     map[string]*RoutingTableEntry
}

// NewRoutingTable builds an empty routing table bound to a zone information table
// (whose network→zone associations are withdrawn alongside routes) and a logger.
func NewRoutingTable(zit *ZoneInformationTable, logger log.Logger) *RoutingTable {
	return &RoutingTable{
		zit:            zit,
		logger:         logger,
		entryByNetwork: map[uint16]*RoutingTableEntry{},
		stateByKey:     map[string]int{},
		entryByKey:     map[string]*RoutingTableEntry{},
	}
}

// entryKey is the stable identity of an entry (port name + range + distance +
// next hop). Built by hand rather than fmt.Sprintf to stay reflection-free (§1).
func entryKey(e *RoutingTableEntry) string {
	var b []byte
	if e.Port != nil {
		b = append(b, e.Port.Name()...)
	}
	b = append(b, '|')
	b = strconv.AppendUint(b, uint64(e.NetworkMin), 10)
	b = append(b, '|')
	b = strconv.AppendUint(b, uint64(e.NetworkMax), 10)
	b = append(b, '|')
	b = strconv.AppendUint(b, uint64(e.Distance), 10)
	b = append(b, '|')
	b = strconv.AppendUint(b, uint64(e.NextNetwork), 10)
	b = append(b, '|')
	b = strconv.AppendUint(b, uint64(e.NextNode), 10)
	return string(b)
}

// GetByNetwork returns the entry serving network n and whether it is currently
// bad (Bad/Worst aging state). A nil entry means the network is unknown.
func (t *RoutingTable) GetByNetwork(n uint16) (*RoutingTableEntry, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	e := t.entryByNetwork[n]
	if e == nil {
		return nil, false
	}
	s := t.stateByKey[entryKey(e)]
	return e, s == stateBad || s == stateWorst
}

// SetPortRange installs (or replaces) p's directly-connected route for the given
// network range. Any prior Distance-0 entry for p is withdrawn first (range may
// have changed after a node-claim), dropping its zone associations.
func (t *RoutingTable) SetPortRange(p RoutedPort, networkMin, networkMax uint16) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for n, e := range t.entryByNetwork {
		if e.Port == p && e.Distance == 0 {
			k := entryKey(e)
			delete(t.stateByKey, k)
			delete(t.entryByKey, k)
			delete(t.entryByNetwork, n)
			nmax := e.NetworkMax
			t.removeZoneNetworks(e.NetworkMin, nmax)
		}
	}
	e := &RoutingTableEntry{
		ExtendedNetwork: networkMin != networkMax,
		NetworkMin:      networkMin,
		NetworkMax:      networkMax,
		Distance:        0,
		Port:            p,
	}
	for n := networkMin; n <= networkMax; n++ {
		t.entryByNetwork[n] = e
	}
	k := entryKey(e)
	t.stateByKey[k] = stateGood
	t.entryByKey[k] = e
}

// Consider folds a learned (Distance>0) route into the table per RTMP rules: an
// identical entry is refreshed to Good; a better/compatible one replaces the
// current entry for its range; a worse one is rejected. Returns true if accepted.
func (t *RoutingTable) Consider(e *RoutingTableEntry) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	k := entryKey(e)
	if _, ok := t.stateByKey[k]; ok {
		t.stateByKey[k] = stateGood
		return true
	}
	var cur *RoutingTableEntry
	for n := e.NetworkMin; n <= e.NetworkMax; n++ {
		x := t.entryByNetwork[n]
		if cur == nil {
			cur = x
		} else if x != cur {
			return false
		}
	}
	if cur != nil {
		ck := entryKey(cur)
		cs := t.stateByKey[ck]
		if cur.Distance < e.Distance && cs != stateBad && cs != stateWorst &&
			(cur.NextNetwork != e.NextNetwork || cur.NextNode != e.NextNode || cur.Port != e.Port) {
			return false
		}
		delete(t.stateByKey, ck)
		delete(t.entryByKey, ck)
	}
	for n := e.NetworkMin; n <= e.NetworkMax; n++ {
		t.entryByNetwork[n] = e
	}
	t.stateByKey[k] = stateGood
	t.entryByKey[k] = e
	return true
}

// MarkBad forces the entry covering [networkMin,networkMax] to Bad (an RTMP
// neighbour advertised the network unreachable). Returns false if the range is
// not covered by a single entry.
func (t *RoutingTable) MarkBad(networkMin, networkMax uint16) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	var cur *RoutingTableEntry
	for n := networkMin; n <= networkMax; n++ {
		e := t.entryByNetwork[n]
		if cur == nil {
			cur = e
		} else if e != cur {
			return false
		}
	}
	if cur == nil {
		return false
	}
	k := entryKey(cur)
	if t.stateByKey[k] != stateWorst {
		t.stateByKey[k] = stateBad
	}
	return true
}

// RemoveEntriesForPort withdraws every route reachable via p — both p's
// directly-connected networks and any remote networks learned through it — and
// drops their zone associations. This is the §3 event-driven membership
// withdrawal: it runs immediately on Detach, with no aging delay.
func (t *RoutingTable) RemoveEntriesForPort(p RoutedPort) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var removed []*RoutingTableEntry
	for k, e := range t.entryByKey {
		if e.Port != p {
			continue
		}
		delete(t.stateByKey, k)
		delete(t.entryByKey, k)
		removed = append(removed, e)
	}
	for n, e := range t.entryByNetwork {
		if e.Port == p {
			delete(t.entryByNetwork, n)
		}
	}
	for _, e := range removed {
		t.removeZoneNetworks(e.NetworkMin, e.NetworkMax)
	}
}

// Age advances the RTMP aging machine by one tick. Learned entries walk
// Good→Suspect→Bad→Worst→removed; a Worst entry is dropped (and its zones
// withdrawn). Directly-connected (Distance 0) entries never age.
func (t *RoutingTable) Age() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for k, e := range t.entryByKey {
		switch t.stateByKey[k] {
		case stateWorst:
			delete(t.stateByKey, k)
			delete(t.entryByKey, k)
			for n := range t.entryByNetwork {
				if t.entryByNetwork[n] == e {
					delete(t.entryByNetwork, n)
				}
			}
			t.removeZoneNetworks(e.NetworkMin, e.NetworkMax)
		case stateBad:
			t.stateByKey[k] = stateWorst
		case stateSus:
			t.stateByKey[k] = stateBad
		case stateGood:
			if e.Distance != 0 {
				t.stateByKey[k] = stateSus
			}
		}
	}
}

// removeZoneNetworks drops the zone associations for a network range, logging a
// warning if the zone information table rejects the removal. Caller holds t.mu.
func (t *RoutingTable) removeZoneNetworks(networkMin, networkMax uint16) {
	nmax := networkMax
	if err := t.zit.RemoveNetworks(networkMin, &nmax); err != nil {
		if t.logger != nil && t.logger.Enabled(log.Warn) {
			t.logger.Log2(log.Warn, "couldn't remove networks from zone information table",
				log.Int("network_min", int64(networkMin)), log.Str("err", err.Error()))
		}
	}
}

// stateName maps an internal RTMP aging state to a human label.
func stateName(s int) string {
	switch s {
	case stateGood:
		return "good"
	case stateSus:
		return "suspect"
	case stateBad:
		return "bad"
	case stateWorst:
		return "worst"
	default:
		return "unknown"
	}
}

// RouteSnapshot is one routing-table entry plus its RTMP aging state, for
// read-only diagnostics (the management UI's RTMP table view).
type RouteSnapshot struct {
	Entry *RoutingTableEntry
	State string // good | suspect | bad | worst
}

// Snapshot returns every distinct routing-table entry with its RTMP aging state.
// Directly-connected entries (Distance 0) are always "good".
func (t *RoutingTable) Snapshot() []RouteSnapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]RouteSnapshot, 0, len(t.entryByKey))
	for k, e := range t.entryByKey {
		out = append(out, RouteSnapshot{Entry: e, State: stateName(t.stateByKey[k])})
	}
	return out
}

// RouteEntry is one entry plus whether it is currently bad, for the RTMP/ZIP
// sending paths that iterate the table.
type RouteEntry struct {
	Entry *RoutingTableEntry
	Bad   bool
}

// Entries returns every distinct routing-table entry with its bad flag.
func (t *RoutingTable) Entries() []RouteEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]RouteEntry, 0, len(t.entryByKey))
	for k, e := range t.entryByKey {
		s := t.stateByKey[k]
		out = append(out, RouteEntry{Entry: e, Bad: s == stateBad || s == stateWorst})
	}
	return out
}
