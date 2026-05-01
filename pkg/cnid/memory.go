package cnid

import (
	"path/filepath"
	"strings"
	"sync"
)

// MemoryStore keeps CNIDs in memory for the lifetime of the process. It
// is the default backend when persistence is not required (tests,
// minimal builds, or callers that explicitly do not want a SQLite file).
type MemoryStore struct {
	mu         sync.RWMutex
	cnidToPath map[uint32]string
	pathToCNID map[string]uint32
	nextCNID   uint32
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		cnidToPath: make(map[uint32]string),
		pathToCNID: make(map[string]uint32),
		nextCNID:   firstDynamic,
	}
}

func (s *MemoryStore) RootID() uint32 { return Root }

func (s *MemoryStore) Path(cnid uint32) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	path, ok := s.cnidToPath[cnid]
	return path, ok
}

func (s *MemoryStore) CNID(path string) (uint32, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cnid, ok := s.pathToCNID[filepath.Clean(path)]
	return cnid, ok
}

func (s *MemoryStore) Ensure(path string) uint32 {
	path = filepath.Clean(path)

	s.mu.Lock()
	defer s.mu.Unlock()

	if cnid, ok := s.pathToCNID[path]; ok {
		return cnid
	}

	cnid := s.nextAvailableCNIDLocked()
	s.cnidToPath[cnid] = path
	s.pathToCNID[path] = cnid
	return cnid
}

func (s *MemoryStore) EnsureReserved(path string, cnid uint32) uint32 {
	path = filepath.Clean(path)

	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.pathToCNID[path]; ok {
		return existing
	}
	if existingPath, ok := s.cnidToPath[cnid]; ok && existingPath != path {
		delete(s.pathToCNID, existingPath)
	}

	s.cnidToPath[cnid] = path
	s.pathToCNID[path] = cnid
	if cnid >= s.nextCNID {
		s.nextCNID = cnid + 1
		if s.nextCNID < firstDynamic {
			s.nextCNID = firstDynamic
		}
	}
	return cnid
}

func (s *MemoryStore) Rebind(oldPath, newPath string) {
	oldPath = filepath.Clean(oldPath)
	newPath = filepath.Clean(newPath)
	prefix := oldPath + string(filepath.Separator)

	s.mu.Lock()
	defer s.mu.Unlock()

	for cnid, path := range s.cnidToPath {
		if path != oldPath && !strings.HasPrefix(path, prefix) {
			continue
		}
		suffix := strings.TrimPrefix(path, oldPath)
		mapped := filepath.Clean(newPath + suffix)
		delete(s.pathToCNID, path)
		s.cnidToPath[cnid] = mapped
		s.pathToCNID[mapped] = cnid
	}
}

func (s *MemoryStore) Remove(path string) {
	path = filepath.Clean(path)
	prefix := path + string(filepath.Separator)

	s.mu.Lock()
	defer s.mu.Unlock()

	for cnid, current := range s.cnidToPath {
		if current == path || strings.HasPrefix(current, prefix) {
			delete(s.cnidToPath, cnid)
			delete(s.pathToCNID, current)
		}
	}
}

func (s *MemoryStore) nextAvailableCNIDLocked() uint32 {
	for {
		cnid := s.nextCNID
		s.nextCNID++
		if cnid < firstDynamic {
			continue
		}
		if _, exists := s.cnidToPath[cnid]; !exists {
			return cnid
		}
	}
}
