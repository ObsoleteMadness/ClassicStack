// Package shortname is the shared 8.3 ("short name") mapping service
// used by SMB 1.0 (which must serve 8.3 to legacy DOS/Windows clients)
// and, optionally, by AFP (whose PathTypeShortNames code path is today
// only a wire flag).
//
// The package is a stub: NewMapper returns a Mapper that produces a
// deterministic naive 8.3 form without persisting collision suffixes.
// Full per-directory uniqueness with a backing store lands when the
// SMB enumeration path actually needs it.
package shortname

import (
	"path/filepath"
	"strings"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/pkg/vfs"
)

// Mapper maps long names to 8.3 short names and back. Implementations
// must be safe for concurrent use.
type Mapper interface {
	// LongToShort returns the 8.3 form for a long file name. The result
	// is the registered short name when one already exists, or a freshly
	// allocated one otherwise.
	LongToShort(long string) string
	// ShortToLong returns the long name previously registered for a
	// short name. The second return is false when no mapping exists.
	ShortToLong(short string) (string, bool)
	// Bind registers (or returns) the short name for long within the
	// given parent directory key, applying ~N collision suffixes.
	Bind(dir, long string) string
}

// Store persists short<->long bindings. The in-memory implementation
// is the default; a sqlite-backed store will land later.
type Store interface {
	Get(short string) (long string, ok bool)
	Put(dir, long, short string) error
	LookupShort(dir, long string) (short string, ok bool)
}

// NewMapper returns a Mapper backed by store. When store is nil, an
// in-memory store is used.
func NewMapper(store Store) Mapper {
	if store == nil {
		store = NewMemoryStore()
	}
	return &mapper{store: store}
}

type mapper struct {
	store Store
}

func (m *mapper) LongToShort(long string) string {
	return m.Bind("", long)
}

func (m *mapper) ShortToLong(short string) (string, bool) {
	return m.store.Get(strings.ToUpper(short))
}

func (m *mapper) Bind(dir, long string) string {
	if existing, ok := m.store.LookupShort(dir, long); ok {
		return existing
	}
	short := derive83(long, 1)
	_ = m.store.Put(dir, long, short)
	return short
}

// derive83 produces a deterministic 8.3 candidate from long with the
// given collision counter N (encoded as ~N). It does not check for
// uniqueness; the caller is responsible for collision handling.
func derive83(long string, n int) string {
	base, ext := splitExt(long)
	base = sanitizeFAT(strings.ToUpper(base))
	ext = sanitizeFAT(strings.ToUpper(ext))
	if len(ext) > 3 {
		ext = ext[:3]
	}
	suffix := "~" + itoa(n)
	keep := max(8-len(suffix), 1)
	if len(base) > keep {
		base = base[:keep]
	}
	if base == "" {
		base = "FILE"
		if len(base) > keep {
			base = base[:keep]
		}
	}
	out := base + suffix
	if ext != "" {
		out += "." + ext
	}
	return out
}

func splitExt(name string) (base, ext string) {
	idx := strings.LastIndex(name, ".")
	if idx <= 0 || idx == len(name)-1 {
		return name, ""
	}
	return name[:idx], name[idx+1:]
}

// sanitizeFAT strips characters that are illegal in FAT short names.
// It is intentionally simple — the canonical Windows mapping is more
// elaborate and lands when the real algorithm replaces this stub.
func sanitizeFAT(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '-' || r == '$' || r == '#' || r == '&' || r == '@' || r == '!' || r == '(' || r == ')' || r == '{' || r == '}' || r == '\'' || r == '`':
			b.WriteRune(r)
		default:
			// Drop spaces, dots (already handled), and anything else.
		}
	}
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// MemoryStore is a non-persistent Store implementation. It is the
// default backing store when callers pass nil to NewMapper.
type MemoryStore struct {
	mu      sync.RWMutex
	byShort map[string]string            // SHORT -> long
	byLong  map[string]map[string]string // dir -> long -> SHORT
}

// NewMemoryStore returns an empty in-memory store and subscribes it to the VFS bus.
func NewMemoryStore() *MemoryStore {
	s := &MemoryStore{
		byShort: map[string]string{},
		byLong:  map[string]map[string]string{},
	}
	vfs.DefaultBus.Subscribe(s)
	return s
}

// Get implements Store.
func (s *MemoryStore) Get(short string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	long, ok := s.byShort[strings.ToUpper(short)]
	return long, ok
}

// LookupShort implements Store.
func (s *MemoryStore) LookupShort(dir, long string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	dirMap, ok := s.byLong[dir]
	if !ok {
		return "", false
	}
	short, ok := dirMap[long]
	return short, ok
}

// Put implements Store. It is intentionally last-writer-wins; the
// real implementation will reject collisions with a different long
// name and return an error so the caller can pick a fresh ~N suffix.
func (s *MemoryStore) Put(dir, long, short string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	short = strings.ToUpper(short)
	if s.byLong[dir] == nil {
		s.byLong[dir] = map[string]string{}
	}
	s.byLong[dir][long] = short
	s.byShort[short] = long
	return nil
}

// OnVFSEvent implements vfs.Subscriber.
func (s *MemoryStore) OnVFSEvent(ev vfs.Event) {
	if ev.Op == vfs.OpDelete || ev.Op == vfs.OpRename {
		s.mu.Lock()
		defer s.mu.Unlock()
		
		dir := filepath.Dir(ev.HostPath)
		long := filepath.Base(ev.HostPath)
		
		if dirMap, ok := s.byLong[dir]; ok {
			if short, ok := dirMap[long]; ok {
				delete(dirMap, long)
				delete(s.byShort, short)
			}
			if len(dirMap) == 0 {
				delete(s.byLong, dir)
			}
		}
	}
}
