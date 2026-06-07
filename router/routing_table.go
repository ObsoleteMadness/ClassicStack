package router

import (
	"fmt"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/port"
)

type RoutingTableEntry struct {
	ExtendedNetwork bool
	NetworkMin      uint16
	NetworkMax      uint16
	Distance        uint8
	Port            port.Port
	NextNetwork     uint16
	NextNode        uint8
}

const (
	stateGood  = 1
	stateSus   = 2
	stateBad   = 3
	stateWorst = 4
)

type RoutingTable struct {
	router         *Router
	entryByNetwork map[uint16]*RoutingTableEntry
	stateByKey     map[string]int
	entryByKey     map[string]*RoutingTableEntry
	mu             sync.RWMutex
}

func entryKey(e *RoutingTableEntry) string {
	return fmt.Sprintf("%s|%d|%d|%d|%d|%d", e.Port.ShortString(), e.NetworkMin, e.NetworkMax, e.Distance, e.NextNetwork, e.NextNode)
}

func NewRoutingTable(router *Router) *RoutingTable {
	return &RoutingTable{
		router:         router,
		entryByNetwork: map[uint16]*RoutingTableEntry{},
		stateByKey:     map[string]int{},
		entryByKey:     map[string]*RoutingTableEntry{},
	}
}

func (t *RoutingTable) GetByNetwork(network uint16) (*RoutingTableEntry, *bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	e := t.entryByNetwork[network]
	if e == nil {
		return nil, nil
	}
	bad := t.stateByKey[entryKey(e)] == stateBad || t.stateByKey[entryKey(e)] == stateWorst
	return e, &bad
}

func (t *RoutingTable) SetPortRange(p port.Port, networkMin, networkMax uint16) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for n, e := range t.entryByNetwork {
		if e.Port == p && e.Distance == 0 {
			netlog.Debug("%s deleting: %+v", t.router.ShortString(), *e)
			delete(t.stateByKey, entryKey(e))
			delete(t.entryByKey, entryKey(e))
			delete(t.entryByNetwork, n)
			nmax := e.NetworkMax
			if err := t.router.ZoneInformationTable.RemoveNetworks(e.NetworkMin, &nmax); err != nil {
				netlog.Warn("%s couldn't remove networks from zone information table: %v",
					t.router.ShortString(), err)
			}
		}
	}
	e := &RoutingTableEntry{
		ExtendedNetwork: p.ExtendedNetwork(),
		NetworkMin:      networkMin,
		NetworkMax:      networkMax,
		Distance:        0,
		Port:            p,
	}
	for n := networkMin; n <= networkMax; n++ {
		t.entryByNetwork[n] = e
	}
	netlog.Debug("%s adding: %+v", t.router.ShortString(), *e)
	t.stateByKey[entryKey(e)] = stateGood
	t.entryByKey[entryKey(e)] = e
}

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
		if cur.Distance < e.Distance && t.stateByKey[ck] != stateBad && t.stateByKey[ck] != stateWorst &&
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
	netlog.Debug("%s adding: %+v", t.router.ShortString(), *e)
	return true
}

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

// RemoveEntriesForPort withdraws every routing-table entry reachable via p
// — both the port's directly-connected networks and any remote networks
// learned through it — and drops their zone associations. It is called when
// a port is removed at runtime (e.g. the operator disables LToUDP) so the
// router stops advertising and routing to networks that no longer have a
// backing interface. It mirrors the cleanup SetPortRange/Age already do for
// a single entry, applied across all of p's entries at once.
func (t *RoutingTable) RemoveEntriesForPort(p port.Port) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Collect the distinct entries owned by p first; entryByKey is the
	// authoritative set and dedupes the per-network fan-out.
	var removed []*RoutingTableEntry
	for k, e := range t.entryByKey {
		if e.Port != p {
			continue
		}
		netlog.Debug("%s removing entry for port %s: %+v", t.router.ShortString(), p.ShortString(), *e)
		delete(t.stateByKey, k)
		delete(t.entryByKey, k)
		removed = append(removed, e)
	}

	// Drop the per-network index entries pointing at any removed entry.
	for n, e := range t.entryByNetwork {
		if e.Port == p {
			delete(t.entryByNetwork, n)
		}
	}

	// Withdraw the corresponding zone associations.
	for _, e := range removed {
		nmax := e.NetworkMax
		if err := t.router.ZoneInformationTable.RemoveNetworks(e.NetworkMin, &nmax); err != nil {
			netlog.Warn("%s couldn't remove networks from zone information table: %v",
				t.router.ShortString(), err)
		}
	}
}

func (t *RoutingTable) Age() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for k, e := range t.entryByKey {
		switch t.stateByKey[k] {
		case stateWorst:
			netlog.Debug("%s aging out: %+v", t.router.ShortString(), *e)
			delete(t.stateByKey, k)
			delete(t.entryByKey, k)
			for n := range t.entryByNetwork {
				if t.entryByNetwork[n] == e {
					delete(t.entryByNetwork, n)
				}
			}
			nmax := e.NetworkMax
			if err := t.router.ZoneInformationTable.RemoveNetworks(e.NetworkMin, &nmax); err != nil {
				netlog.Warn("%s couldn't remove networks from zone information table: %v",
					t.router.ShortString(), err)
			}
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

// stateName maps an internal RTMP aging state to a human label. RTMP routers
// age entries through Good → Suspect → Bad → (removed) on successive aging
// ticks; receiving the route again resets it to Good. This validity state is
// RTMP's notion of an entry's "age" — there is no wall-clock timestamp.
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

// RoutingTableSnapshotEntry is one routing-table entry plus its RTMP aging
// state, for read-only diagnostics.
type RoutingTableSnapshotEntry struct {
	Entry *RoutingTableEntry
	State string // RTMP aging state: good | suspect | bad | worst
}

// Snapshot returns every distinct routing-table entry with its RTMP aging
// state. Directly-connected entries (Distance 0) are always "good"; learned
// entries carry the state the aging machine has reached.
func (t *RoutingTable) Snapshot() []RoutingTableSnapshotEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]RoutingTableSnapshotEntry, 0, len(t.entryByKey))
	for k, e := range t.entryByKey {
		out = append(out, RoutingTableSnapshotEntry{Entry: e, State: stateName(t.stateByKey[k])})
	}
	return out
}

func (t *RoutingTable) Entries() []struct {
	Entry *RoutingTableEntry
	Bad   bool
} {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]struct {
		Entry *RoutingTableEntry
		Bad   bool
	}, 0, len(t.entryByKey))
	for k, e := range t.entryByKey {
		s := t.stateByKey[k]
		out = append(out, struct {
			Entry *RoutingTableEntry
			Bad   bool
		}{Entry: e, Bad: s == stateBad || s == stateWorst})
	}
	return out
}
