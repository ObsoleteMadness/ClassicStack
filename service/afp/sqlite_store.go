package afp

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pgodw/omnitalk/netlog"
	_ "modernc.org/sqlite"
)

const afpSQLiteFilename = "_.afp.db"

func sqliteDBPath(volumeRootPath string) string {
	return filepath.Join(filepath.Clean(volumeRootPath), afpSQLiteFilename)
}

func openSQLiteDB(volumeRootPath string) (*sql.DB, error) {
	dbPath := sqliteDBPath(volumeRootPath)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("create sqlite dir for %q: %w", dbPath, err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite db %q: %w", dbPath, err)
	}
	// Single-writer access pattern avoids lock contention and keeps behavior
	// deterministic across concurrent AFP operations.
	db.SetMaxOpenConns(1)
	// Do not retain idle connections so temp-volume DB files are not held open
	// on Windows between AFP operations.
	db.SetMaxIdleConns(0)

	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	}
	for _, stmt := range pragmas {
		if _, execErr := db.Exec(stmt); execErr != nil {
			db.Close()
			return nil, fmt.Errorf("sqlite pragma %q on %q: %w", stmt, dbPath, execErr)
		}
	}

	netlog.Info("[AFP][SQLite] opened %q", dbPath)
	return db, nil
}
