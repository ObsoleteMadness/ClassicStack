package metastore

import (
	"strconv"
	"strings"
	"sync"
)

// CNID well-known node IDs (AFP Catalog Node IDs).
const (
	// CNIDInvalid is the "no CNID" / error sentinel.
	CNIDInvalid uint32 = 0
	// CNIDParentOfRoot is the synthetic parent of the root directory.
	CNIDParentOfRoot uint32 = 1
	// CNIDRoot identifies a volume's root directory.
	CNIDRoot uint32 = 2
	// cnidFirstDynamic is the first CNID assignable to non-root objects.
	cnidFirstDynamic uint32 = 3
)

// CNIDStore tracks the CNID <-> path mapping for one volume on top of a keyed
// metastore.Store, so the binding persists (or not) according to the store kind
// — mem by default, sqlite behind a build tag — without the CNID logic knowing
// which. This is the §9 inversion: the AFP-specific CNID registry re-expressed
// over the shared store seam.
type CNIDStore struct {
	store Store

	mu   sync.Mutex
	next uint32
}

// metastore key layout (one volume per CNIDStore; callers scope by store):
//
//	"c/p/<cnid>"  -> path        (cnid -> path)
//	"c/i/<path>"  -> <cnid>      (path -> cnid, decimal)
//	"c/seq"       -> <next>      (next dynamic cnid, decimal)
func cnidPathKey(cnid uint32) []byte { return []byte("c/p/" + strconv.FormatUint(uint64(cnid), 10)) }
func cnidIDKey(path string) []byte   { return []byte("c/i/" + cleanPath(path)) }

var cnidSeqKey = []byte("c/seq")

// NewCNIDStore returns a CNID store over store, recovering the next-id sequence
// from a prior snapshot when present.
func NewCNIDStore(store Store) *CNIDStore {
	c := &CNIDStore{store: store, next: cnidFirstDynamic}
	if v, ok := store.Get(cnidSeqKey); ok {
		if n, err := strconv.ParseUint(string(v), 10, 32); err == nil && uint32(n) >= cnidFirstDynamic {
			c.next = uint32(n)
		}
	}
	return c
}

// RootID returns the volume root CNID.
func (c *CNIDStore) RootID() uint32 { return CNIDRoot }

// Path returns the path bound to cnid.
func (c *CNIDStore) Path(cnid uint32) (string, bool) {
	v, ok := c.store.Get(cnidPathKey(cnid))
	return string(v), ok
}

// CNID returns the CNID bound to path.
func (c *CNIDStore) CNID(path string) (uint32, bool) {
	v, ok := c.store.Get(cnidIDKey(path))
	if !ok {
		return CNIDInvalid, false
	}
	n, err := strconv.ParseUint(string(v), 10, 32)
	if err != nil {
		return CNIDInvalid, false
	}
	return uint32(n), true
}

// Ensure returns the CNID for path, allocating a fresh one on first sight.
func (c *CNIDStore) Ensure(path string) uint32 {
	path = cleanPath(path)
	c.mu.Lock()
	defer c.mu.Unlock()
	if cnid, ok := c.CNID(path); ok {
		return cnid
	}
	cnid := c.nextAvailableLocked()
	c.bindLocked(cnid, path)
	return cnid
}

// EnsureReserved binds path to a specific cnid (e.g. a recovered desktop entry),
// advancing the sequence past it.
func (c *CNIDStore) EnsureReserved(path string, cnid uint32) uint32 {
	path = cleanPath(path)
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.CNID(path); ok {
		return existing
	}
	if existingPath, ok := c.Path(cnid); ok && existingPath != path {
		_ = c.store.Delete(cnidIDKey(existingPath))
	}
	c.bindLocked(cnid, path)
	if cnid >= c.next {
		c.next = max(cnid+1, cnidFirstDynamic)
		c.persistSeqLocked()
	}
	return cnid
}

// Rebind moves path (and its subtree) from oldPath to newPath, preserving CNIDs.
func (c *CNIDStore) Rebind(oldPath, newPath string) {
	oldPath = cleanPath(oldPath)
	newPath = cleanPath(newPath)
	prefix := oldPath + "/"

	c.mu.Lock()
	defer c.mu.Unlock()

	type move struct {
		cnid uint32
		oldP string
		newP string
	}
	var moves []move
	_ = c.store.Range([]byte("c/i/"), func(k, v []byte) bool {
		p := strings.TrimPrefix(string(k), "c/i/")
		if p != oldPath && !strings.HasPrefix(p, prefix) {
			return true
		}
		n, err := strconv.ParseUint(string(v), 10, 32)
		if err != nil {
			return true
		}
		mapped := cleanPath(newPath + strings.TrimPrefix(p, oldPath))
		moves = append(moves, move{cnid: uint32(n), oldP: p, newP: mapped})
		return true
	})
	for _, m := range moves {
		_ = c.store.Delete(cnidIDKey(m.oldP))
		c.bindLocked(m.cnid, m.newP)
	}
}

// Remove deletes path and its subtree from the mapping.
func (c *CNIDStore) Remove(path string) {
	path = cleanPath(path)
	prefix := path + "/"

	c.mu.Lock()
	defer c.mu.Unlock()

	var victims []struct {
		cnid uint32
		path string
	}
	_ = c.store.Range([]byte("c/i/"), func(k, v []byte) bool {
		p := strings.TrimPrefix(string(k), "c/i/")
		if p != path && !strings.HasPrefix(p, prefix) {
			return true
		}
		n, _ := strconv.ParseUint(string(v), 10, 32)
		victims = append(victims, struct {
			cnid uint32
			path string
		}{uint32(n), p})
		return true
	})
	for _, vct := range victims {
		_ = c.store.Delete(cnidIDKey(vct.path))
		_ = c.store.Delete(cnidPathKey(vct.cnid))
	}
}

func (c *CNIDStore) bindLocked(cnid uint32, path string) {
	_ = c.store.Put(cnidPathKey(cnid), []byte(path))
	_ = c.store.Put(cnidIDKey(path), []byte(strconv.FormatUint(uint64(cnid), 10)))
}

func (c *CNIDStore) nextAvailableLocked() uint32 {
	for {
		cnid := c.next
		c.next++
		if cnid < cnidFirstDynamic {
			continue
		}
		if _, exists := c.Path(cnid); !exists {
			c.persistSeqLocked()
			return cnid
		}
	}
}

func (c *CNIDStore) persistSeqLocked() {
	_ = c.store.Put(cnidSeqKey, []byte(strconv.FormatUint(uint64(c.next), 10)))
}

// cleanPath normalises a slash-separated path: collapses repeated separators and
// trims a trailing slash, without importing path/filepath (store paths are
// always '/'-separated regardless of host).
func cleanPath(p string) string {
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	if len(p) > 1 {
		p = strings.TrimSuffix(p, "/")
	}
	return p
}
