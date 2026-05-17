package over_netbeui

import (
	"sync"

	protocol "github.com/ObsoleteMadness/ClassicStack/protocol/netbios"
)

// nameState tracks the lifecycle of a locally registered name.
type nameState uint8

const (
	// nameStateClaiming means we have broadcast ADD_NAME_QUERY but
	// have not yet confirmed uniqueness.
	nameStateClaiming nameState = iota
	// nameStateRegistered means the name is confirmed unique (or is
	// a group name that passed conflict checks).
	nameStateRegistered
	// nameStateConflict means a NAME_IN_CONFLICT was received.
	nameStateConflict
)

// nameEntry is a single name registered at this node.
type nameEntry struct {
	Name    protocol.Name
	IsGroup bool
	State   nameState
	// Number is the local name number (1-based) assigned at
	// registration, used to build NAME_NUMBER_1 when required by
	// the wire protocol. 0 means not yet assigned.
	Number uint8
}

// nameTable is a thread-safe registry of locally owned NetBIOS names.
type nameTable struct {
	mu      sync.RWMutex
	names   map[protocol.Name]*nameEntry
	nextNum uint8 // next name number to assign (1–254)
}

func newNameTable() *nameTable {
	return &nameTable{
		names:   make(map[protocol.Name]*nameEntry),
		nextNum: 1,
	}
}

// Add registers a name in the claiming state. Returns the entry so
// the caller can transition it to registered after the claim cycle.
// Returns nil if the name is already registered.
func (t *nameTable) Add(name protocol.Name, isGroup bool) *nameEntry {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.names[name]; ok {
		return nil
	}
	num := t.nextNum
	if t.nextNum < 254 {
		t.nextNum++
	}
	e := &nameEntry{
		Name:    name,
		IsGroup: isGroup,
		State:   nameStateClaiming,
		Number:  num,
	}
	t.names[name] = e
	return e
}

// Remove deletes a name from the table.
func (t *nameTable) Remove(name protocol.Name) {
	t.mu.Lock()
	delete(t.names, name)
	t.mu.Unlock()
}

// Lookup returns the entry for name, or nil if not found.
func (t *nameTable) Lookup(name protocol.Name) *nameEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.names[name]
}

// IsLocal returns true if name is registered locally.
func (t *nameTable) IsLocal(name protocol.Name) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	_, ok := t.names[name]
	return ok
}

// SetState updates the state of a registered name.
func (t *nameTable) SetState(name protocol.Name, state nameState) {
	t.mu.Lock()
	if e, ok := t.names[name]; ok {
		e.State = state
	}
	t.mu.Unlock()
}

// All returns a snapshot of all entries.
func (t *nameTable) All() []*nameEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]*nameEntry, 0, len(t.names))
	for _, e := range t.names {
		out = append(out, e)
	}
	return out
}

// Registered returns all names in the registered state.
func (t *nameTable) Registered() []*nameEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var out []*nameEntry
	for _, e := range t.names {
		if e.State == nameStateRegistered {
			out = append(out, e)
		}
	}
	return out
}

// nameNumber1 builds the NAME_NUMBER_1 encoding: 10 zero bytes
// followed by the 6-byte permanent adapter address (MAC).
func nameNumber1(mac [6]byte) protocol.Name {
	var n protocol.Name
	copy(n[10:], mac[:])
	return n
}
