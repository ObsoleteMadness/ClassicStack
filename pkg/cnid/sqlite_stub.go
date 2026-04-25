//go:build !sqlite_cnid

package cnid

import (
	"database/sql"
	"errors"
	"path/filepath"
)

// SQLiteFilename is the standard CNID database filename dropped at the
// root of a volume. The constant remains exported in stub builds so
// callers can detect/skip the sidecar regardless of which CNID backend
// is compiled in.
const SQLiteFilename = "_.afp.db"

// ErrSQLiteDisabled is returned by SQLite-backed constructors when the
// binary is built without the "sqlite_cnid" build tag. Callers should
// fall back to MemoryStore.
var ErrSQLiteDisabled = errors.New("sqlite CNID backend not built; rebuild with -tags sqlite_cnid")

// SQLitePath returns the canonical CNID database location even in stub
// builds so callers that filter the sidecar by name keep working.
func SQLitePath(volumeRootPath string) string {
	return filepath.Join(filepath.Clean(volumeRootPath), SQLiteFilename)
}

// SQLiteStore is a stub type so external alias declarations
// (e.g. service/afp.SQLiteCNIDStore) keep compiling under !sqlite_cnid.
// The real implementation lives in sqlite.go behind //go:build sqlite_cnid.
//
// All methods are no-ops; the stub is only ever returned alongside
// ErrSQLiteDisabled, so callers fall back to MemoryStore before any
// method is invoked.
type SQLiteStore struct{}

func (*SQLiteStore) RootID() uint32                                  { return Root }
func (*SQLiteStore) Path(_ uint32) (string, bool)                    { return "", false }
func (*SQLiteStore) CNID(_ string) (uint32, bool)                    { return 0, false }
func (*SQLiteStore) Ensure(_ string) uint32                          { return 0 }
func (*SQLiteStore) EnsureReserved(_ string, cnid uint32) uint32     { return cnid }
func (*SQLiteStore) Rebind(_ string, _ string)                       {}
func (*SQLiteStore) Remove(_ string)                                 {}

// OpenSQLiteDB always returns ErrSQLiteDisabled in stub builds.
func OpenSQLiteDB(_ string) (*sql.DB, error) { return nil, ErrSQLiteDisabled }

// NewSQLiteStore always returns ErrSQLiteDisabled in stub builds.
func NewSQLiteStore(_ string) (*SQLiteStore, error) { return nil, ErrSQLiteDisabled }
