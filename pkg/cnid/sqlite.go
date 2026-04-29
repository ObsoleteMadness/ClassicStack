//go:build sqlite_cnid || all

package cnid

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

// SQLiteFilename is the standard CNID database filename dropped at the
// root of a volume.
const SQLiteFilename = "_.afp.db"

// SQLitePath returns the canonical location of the CNID database file
// for a volume whose filesystem root is volumeRootPath.
func SQLitePath(volumeRootPath string) string {
	return filepath.Join(filepath.Clean(volumeRootPath), SQLiteFilename)
}

// OpenSQLiteDB opens (creating if necessary) the CNID SQLite database
// for a volume at volumeRootPath. It is exported so callers that want
// to share a *sql.DB between CNID and other per-volume metadata (e.g.
// Desktop DB) can do so.
func OpenSQLiteDB(volumeRootPath string) (*sql.DB, error) {
	dbPath := SQLitePath(volumeRootPath)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create sqlite dir for %q: %w", dbPath, err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite db %q: %w", dbPath, err)
	}
	// Single-writer access pattern keeps behaviour deterministic under
	// concurrent AFP operations and avoids Windows lock contention.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)

	for _, stmt := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	} {
		if _, execErr := db.Exec(stmt); execErr != nil {
			db.Close()
			return nil, fmt.Errorf("sqlite pragma %q on %q: %w", stmt, dbPath, execErr)
		}
	}

	slog.Default().Info("opened cnid sqlite database", "path", dbPath, "source", "CNID")
	return db, nil
}

// SQLiteStore persists CNIDs in a per-volume SQLite database.
type SQLiteStore struct {
	mu sync.Mutex
	db *sql.DB
}

// NewSQLiteStore opens (or creates) the CNID database under volumeRootPath.
func NewSQLiteStore(volumeRootPath string) (*SQLiteStore, error) {
	db, err := OpenSQLiteDB(volumeRootPath)
	if err != nil {
		return nil, err
	}
	store := &SQLiteStore{db: db}
	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) initSchema() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS cnid_paths (
			cnid INTEGER PRIMARY KEY,
			path TEXT NOT NULL UNIQUE
		);
		CREATE INDEX IF NOT EXISTS idx_cnid_paths_path ON cnid_paths(path);
	`)
	return err
}

func (s *SQLiteStore) RootID() uint32 { return Root }

func (s *SQLiteStore) Path(cnid uint32) (string, bool) {
	var path string
	err := s.db.QueryRow("SELECT path FROM cnid_paths WHERE cnid = ?", cnid).Scan(&path)
	if err != nil {
		return "", false
	}
	return path, true
}

func (s *SQLiteStore) CNID(path string) (uint32, bool) {
	path = filepath.Clean(path)
	var cnid uint32
	err := s.db.QueryRow("SELECT cnid FROM cnid_paths WHERE path = ?", path).Scan(&cnid)
	if err != nil {
		return 0, false
	}
	return cnid, true
}

func (s *SQLiteStore) Ensure(path string) uint32 {
	path = filepath.Clean(path)

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return Invalid
	}
	defer tx.Rollback()

	if cnid, ok := selectCNIDByPathTx(tx, path); ok {
		_ = tx.Commit()
		return cnid
	}

	cnid, err := nextAvailableCNIDTx(tx)
	if err != nil {
		return Invalid
	}
	if _, err := tx.Exec("INSERT INTO cnid_paths(cnid, path) VALUES(?, ?)", cnid, path); err != nil {
		return Invalid
	}
	if err := tx.Commit(); err != nil {
		return Invalid
	}
	return cnid
}

func (s *SQLiteStore) EnsureReserved(path string, cnid uint32) uint32 {
	path = filepath.Clean(path)

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return Invalid
	}
	defer tx.Rollback()

	if existing, ok := selectCNIDByPathTx(tx, path); ok {
		_ = tx.Commit()
		return existing
	}

	if existingPath, ok := selectPathByCNIDTx(tx, cnid); ok && existingPath != path {
		if _, err := tx.Exec("DELETE FROM cnid_paths WHERE cnid = ?", cnid); err != nil {
			return Invalid
		}
	}

	if _, err := tx.Exec("INSERT INTO cnid_paths(cnid, path) VALUES(?, ?)", cnid, path); err != nil {
		return Invalid
	}
	if err := tx.Commit(); err != nil {
		return Invalid
	}
	return cnid
}

func (s *SQLiteStore) Rebind(oldPath, newPath string) {
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
	var updates []row
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

func (s *SQLiteStore) Remove(path string) {
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

	var toDelete []uint32
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
	if maxCNID < firstDynamic-1 {
		return firstDynamic, nil
	}
	return maxCNID + 1, nil
}
