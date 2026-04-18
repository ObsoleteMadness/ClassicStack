package afp

import (
	"database/sql"
	"path/filepath"
	"strings"
	"sync"

	"github.com/pgodw/omnitalk/go/netlog"
)

const (
	CNIDInvalid      uint32 = 0
	CNIDParentOfRoot uint32 = 1
	CNIDRoot         uint32 = 2
	firstDynamicCNID uint32 = 3
)

// CNIDStore tracks the mapping between AFP catalog node IDs and current paths
// for a single volume.
type CNIDStore interface {
	RootID() uint32
	Path(cnid uint32) (string, bool)
	CNID(path string) (uint32, bool)
	Ensure(path string) uint32
	EnsureReserved(path string, cnid uint32) uint32
	Rebind(oldPath, newPath string)
	Remove(path string)
}

// CNIDBackend creates a per-volume CNID store implementation.
type CNIDBackend interface {
	Open(volume Volume) CNIDStore
}

// MemoryCNIDBackend provides the default non-persistent CNID implementation.
type MemoryCNIDBackend struct{}

func (MemoryCNIDBackend) Open(volume Volume) CNIDStore {
	return NewMemoryCNIDStore()
}

// SQLiteCNIDBackend stores CNIDs in a per-volume SQLite database.
type SQLiteCNIDBackend struct{}

func (SQLiteCNIDBackend) Open(volume Volume) CNIDStore {
	store, err := NewSQLiteCNIDStore(volume.Config.Path)
	if err != nil {
		netlog.Warn("[AFP][CNID] sqlite init failed for volume=%q path=%q: %v; falling back to memory", volume.Config.Name, volume.Config.Path, err)
		return NewMemoryCNIDStore()
	}
	return store
}

func resolveCNIDBackend(options AFPOptions) CNIDBackend {
	if options.CNIDStoreBackend != nil {
		return options.CNIDStoreBackend
	}
	switch options.CNIDBackend {
	case "", "sqlite":
		return SQLiteCNIDBackend{}
	case "memory":
		return MemoryCNIDBackend{}
	default:
		return SQLiteCNIDBackend{}
	}
}

// SQLiteCNIDStore keeps CNIDs in SQLite for persistence across restarts.
type SQLiteCNIDStore struct {
	mu sync.Mutex
	db *sql.DB
}

func NewSQLiteCNIDStore(volumeRootPath string) (*SQLiteCNIDStore, error) {
	db, err := openSQLiteDB(volumeRootPath)
	if err != nil {
		return nil, err
	}
	store := &SQLiteCNIDStore{db: db}
	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteCNIDStore) initSchema() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS cnid_paths (
			cnid INTEGER PRIMARY KEY,
			path TEXT NOT NULL UNIQUE
		);
		CREATE INDEX IF NOT EXISTS idx_cnid_paths_path ON cnid_paths(path);
	`)
	return err
}

func (s *SQLiteCNIDStore) RootID() uint32 { return CNIDRoot }

func (s *SQLiteCNIDStore) Path(cnid uint32) (string, bool) {
	var path string
	err := s.db.QueryRow("SELECT path FROM cnid_paths WHERE cnid = ?", cnid).Scan(&path)
	if err != nil {
		return "", false
	}
	return path, true
}

func (s *SQLiteCNIDStore) CNID(path string) (uint32, bool) {
	path = filepath.Clean(path)
	var cnid uint32
	err := s.db.QueryRow("SELECT cnid FROM cnid_paths WHERE path = ?", path).Scan(&cnid)
	if err != nil {
		return 0, false
	}
	return cnid, true
}

func (s *SQLiteCNIDStore) Ensure(path string) uint32 {
	path = filepath.Clean(path)

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return CNIDInvalid
	}
	defer tx.Rollback()

	if cnid, ok := selectCNIDByPathTx(tx, path); ok {
		_ = tx.Commit()
		return cnid
	}

	cnid, err := nextAvailableCNIDTx(tx)
	if err != nil {
		return CNIDInvalid
	}
	if _, err := tx.Exec("INSERT INTO cnid_paths(cnid, path) VALUES(?, ?)", cnid, path); err != nil {
		return CNIDInvalid
	}
	if err := tx.Commit(); err != nil {
		return CNIDInvalid
	}
	return cnid
}

func (s *SQLiteCNIDStore) EnsureReserved(path string, cnid uint32) uint32 {
	path = filepath.Clean(path)

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return CNIDInvalid
	}
	defer tx.Rollback()

	if existing, ok := selectCNIDByPathTx(tx, path); ok {
		_ = tx.Commit()
		return existing
	}

	if existingPath, ok := selectPathByCNIDTx(tx, cnid); ok && existingPath != path {
		if _, err := tx.Exec("DELETE FROM cnid_paths WHERE cnid = ?", cnid); err != nil {
			return CNIDInvalid
		}
	}

	if _, err := tx.Exec("INSERT INTO cnid_paths(cnid, path) VALUES(?, ?)", cnid, path); err != nil {
		return CNIDInvalid
	}
	if err := tx.Commit(); err != nil {
		return CNIDInvalid
	}
	return cnid
}

func (s *SQLiteCNIDStore) Rebind(oldPath, newPath string) {
	oldPath = filepath.Clean(oldPath)
	newPath = filepath.Clean(newPath)
	prefix := oldPath + string(filepath.Separator)

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()

	rows, err := tx.Query("SELECT cnid, path FROM cnid_paths")
	if err != nil {
		return
	}
	defer rows.Close()

	type row struct {
		cnid uint32
		path string
	}
	updates := make([]row, 0)
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.cnid, &r.path); err != nil {
			return
		}
		if r.path != oldPath && !strings.HasPrefix(r.path, prefix) {
			continue
		}
		updates = append(updates, r)
	}
	for _, r := range updates {
		suffix := strings.TrimPrefix(r.path, oldPath)
		mapped := filepath.Clean(newPath + suffix)
		if _, err := tx.Exec("UPDATE cnid_paths SET path = ? WHERE cnid = ?", mapped, r.cnid); err != nil {
			return
		}
	}
	_ = tx.Commit()
}

func (s *SQLiteCNIDStore) Remove(path string) {
	path = filepath.Clean(path)
	prefix := path + string(filepath.Separator)

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()

	rows, err := tx.Query("SELECT cnid, path FROM cnid_paths")
	if err != nil {
		return
	}
	defer rows.Close()

	toDelete := make([]uint32, 0)
	for rows.Next() {
		var cnid uint32
		var current string
		if err := rows.Scan(&cnid, &current); err != nil {
			return
		}
		if current == path || strings.HasPrefix(current, prefix) {
			toDelete = append(toDelete, cnid)
		}
	}
	for _, cnid := range toDelete {
		if _, err := tx.Exec("DELETE FROM cnid_paths WHERE cnid = ?", cnid); err != nil {
			return
		}
	}
	_ = tx.Commit()
}

func selectCNIDByPathTx(tx *sql.Tx, path string) (uint32, bool) {
	var cnid uint32
	err := tx.QueryRow("SELECT cnid FROM cnid_paths WHERE path = ?", path).Scan(&cnid)
	if err != nil {
		return 0, false
	}
	return cnid, true
}

func selectPathByCNIDTx(tx *sql.Tx, cnid uint32) (string, bool) {
	var path string
	err := tx.QueryRow("SELECT path FROM cnid_paths WHERE cnid = ?", cnid).Scan(&path)
	if err != nil {
		return "", false
	}
	return path, true
}

func nextAvailableCNIDTx(tx *sql.Tx) (uint32, error) {
	var maxCNID uint32
	if err := tx.QueryRow("SELECT COALESCE(MAX(cnid), 0) FROM cnid_paths").Scan(&maxCNID); err != nil {
		return 0, err
	}
	if maxCNID < firstDynamicCNID-1 {
		return firstDynamicCNID, nil
	}
	return maxCNID + 1, nil
}

// MemoryCNIDStore keeps CNIDs in-memory for the lifetime of the AFP service.
type MemoryCNIDStore struct {
	mu         sync.RWMutex
	cnidToPath map[uint32]string
	pathToCNID map[string]uint32
	nextCNID   uint32
}

func NewMemoryCNIDStore() *MemoryCNIDStore {
	return &MemoryCNIDStore{
		cnidToPath: make(map[uint32]string),
		pathToCNID: make(map[string]uint32),
		nextCNID:   firstDynamicCNID,
	}
}

func (s *MemoryCNIDStore) RootID() uint32 { return CNIDRoot }

func (s *MemoryCNIDStore) Path(cnid uint32) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	path, ok := s.cnidToPath[cnid]
	return path, ok
}

func (s *MemoryCNIDStore) CNID(path string) (uint32, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cnid, ok := s.pathToCNID[filepath.Clean(path)]
	return cnid, ok
}

func (s *MemoryCNIDStore) Ensure(path string) uint32 {
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

func (s *MemoryCNIDStore) EnsureReserved(path string, cnid uint32) uint32 {
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
		if s.nextCNID < firstDynamicCNID {
			s.nextCNID = firstDynamicCNID
		}
	}
	return cnid
}

func (s *MemoryCNIDStore) Rebind(oldPath, newPath string) {
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

func (s *MemoryCNIDStore) Remove(path string) {
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

func (s *MemoryCNIDStore) nextAvailableCNIDLocked() uint32 {
	for {
		cnid := s.nextCNID
		s.nextCNID++
		if cnid < firstDynamicCNID {
			continue
		}
		if _, exists := s.cnidToPath[cnid]; !exists {
			return cnid
		}
	}
}
