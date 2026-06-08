package metastore

import (
	"errors"
	"sort"
	"strings"
	"sync"
)

// Store is a small persistent keyed map. Keys/values are opaque bytes; the caller (CNID,
// shortname, desktop) owns the schema. Range visits entries under prefix until fn returns false.
type Store interface {
	Get(key []byte) (val []byte, ok bool)
	Put(key, val []byte) error
	Delete(key []byte) error
	Range(prefix []byte, fn func(k, v []byte) bool) error
	Sync() error
	Close() error
}

// ErrUnknownKind is returned by Open for a store kind with no registered adapter.
var ErrUnknownKind = errors.New("metastore: unknown store kind")

// Open returns a store of the named kind at path (kind selects an adapter; "mem" is built-in).
// Adapters (e.g. sqlite) register additional kinds via Register; the default build only knows
// "mem".
func Open(kind, path string) (Store, error) {
	if f, ok := lookup(kind); ok {
		return f(path)
	}
	if kind == "mem" {
		return NewMem(path)
	}
	return nil, ErrUnknownKind
}

// --- adapter registration (sqlite etc. register here from a build-tagged init()). ---

var (
	regMu    sync.RWMutex
	registry = map[string]func(path string) (Store, error){}
)

// Register adds a store-kind factory. Called from a build-tagged adapter init().
func Register(kind string, f func(path string) (Store, error)) {
	regMu.Lock()
	defer regMu.Unlock()
	registry[kind] = f
}

func lookup(kind string) (func(path string) (Store, error), bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	f, ok := registry[kind]
	return f, ok
}

// --- mem: the default in-memory store, snapshotting to a file. ---

// memStore is an in-memory keyed map that snapshots to path on Sync/Close.
// It is safe for concurrent use.
type memStore struct {
	path string

	mu sync.RWMutex
	m  map[string][]byte
}

// NewMem returns the default in-memory store, snapshotting to path on Sync/Close (path ""
// = volatile). Reopening the same path reloads the snapshot.
func NewMem(path string) (Store, error) {
	s := &memStore{path: path, m: make(map[string][]byte)}
	if path != "" {
		if err := s.load(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *memStore) Get(key []byte) ([]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.m[string(key)]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), v...), true // copy: caller must not see our backing array
}

func (s *memStore) Put(key, val []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[string(key)] = append([]byte(nil), val...)
	return nil
}

func (s *memStore) Delete(key []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, string(key))
	return nil
}

// Range visits entries whose key begins with prefix, in sorted key order, until fn returns
// false. Iteration order is deterministic so callers (e.g. CNID enumeration) are stable.
func (s *memStore) Range(prefix []byte, fn func(k, v []byte) bool) error {
	s.mu.RLock()
	keys := make([]string, 0, len(s.m))
	for k := range s.m {
		if strings.HasPrefix(k, string(prefix)) {
			keys = append(keys, k)
		}
	}
	s.mu.RUnlock()

	sort.Strings(keys)
	for _, k := range keys {
		s.mu.RLock()
		v, ok := s.m[k]
		vc := append([]byte(nil), v...)
		s.mu.RUnlock()
		if !ok {
			continue
		}
		if !fn([]byte(k), vc) {
			return nil
		}
	}
	return nil
}

// Sync writes the current contents to path (a no-op for a volatile store).
func (s *memStore) Sync() error {
	if s.path == "" {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.save()
}

// Close syncs then drops the in-memory contents.
func (s *memStore) Close() error {
	if err := s.Sync(); err != nil {
		return err
	}
	s.mu.Lock()
	s.m = nil
	s.mu.Unlock()
	return nil
}
