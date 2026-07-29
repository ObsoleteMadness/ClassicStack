//go:build sqlite || all

package sqlite

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"

	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
)

// kind is the store-kind name this adapter registers.
const kind = "sqlite"

func init() {
	metastore.Register(kind, open)
}

// store is a keyed metastore.Store backed by a single SQLite table. Keys and
// values are opaque BLOBs; the CNID/shortname/desktop schema lives in the
// caller's key layout exactly as it does for the in-memory store.
type store struct {
	db *sql.DB
}

// open creates/opens the SQLite database at path. An empty path uses a private
// in-memory database (still SQLite, but not persisted) so tests can exercise the
// adapter without touching disk.
func open(path string) (metastore.Store, error) {
	dsn := path
	if dsn == "" {
		dsn = ":memory:"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("metastore/sqlite: open %q: %w", path, err)
	}
	// A keyed store is a single-writer workload; one connection avoids
	// "database is locked" on the in-memory DSN and keeps ordering simple.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS kv (k BLOB PRIMARY KEY, v BLOB NOT NULL)`); err != nil {
		_ = db.Close() // best-effort cleanup; returning the create-table error
		return nil, fmt.Errorf("metastore/sqlite: create table: %w", err)
	}
	return &store{db: db}, nil
}

func (s *store) Get(key []byte) ([]byte, bool) {
	var v []byte
	err := s.db.QueryRow(`SELECT v FROM kv WHERE k = ?`, key).Scan(&v)
	if err != nil {
		return nil, false
	}
	return v, true
}

func (s *store) Put(key, val []byte) error {
	_, err := s.db.Exec(
		`INSERT INTO kv (k, v) VALUES (?, ?) ON CONFLICT(k) DO UPDATE SET v = excluded.v`,
		key, val,
	)
	return err
}

func (s *store) Delete(key []byte) error {
	_, err := s.db.Exec(`DELETE FROM kv WHERE k = ?`, key)
	return err
}

// Range visits rows whose key begins with prefix in sorted key order until fn
// returns false, matching the in-memory store's deterministic iteration.
func (s *store) Range(prefix []byte, fn func(k, v []byte) bool) error {
	rows, err := s.db.Query(
		`SELECT k, v FROM kv WHERE substr(k, 1, ?) = ? ORDER BY k`,
		len(prefix), prefix,
	)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v []byte
		if err := rows.Scan(&k, &v); err != nil {
			return err
		}
		if !fn(k, v) {
			return nil
		}
	}
	return rows.Err()
}

func (s *store) Sync() error { return nil } // each Exec is durable

func (s *store) Close() error { return s.db.Close() }
