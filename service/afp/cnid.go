package afp

import (
	"github.com/pgodw/omnitalk/netlog"
	"github.com/pgodw/omnitalk/pkg/cnid"
)

// CNID constants and the Store interface now live in pkg/cnid. These
// aliases preserve the historical AFP-package identifiers so the
// existing fork/directory/volume code keeps compiling unchanged during
// the lift-and-shift. New code should import pkg/cnid directly.
const (
	CNIDInvalid      = cnid.Invalid
	CNIDParentOfRoot = cnid.ParentOfRoot
	CNIDRoot         = cnid.Root
)

type (
	// CNIDStore is the AFP-package alias for cnid.Store.
	CNIDStore = cnid.Store
	// MemoryCNIDStore is the AFP-package alias for cnid.MemoryStore.
	MemoryCNIDStore = cnid.MemoryStore
	// SQLiteCNIDStore is the AFP-package alias for cnid.SQLiteStore.
	SQLiteCNIDStore = cnid.SQLiteStore
)

// NewMemoryCNIDStore is the AFP-package alias for cnid.NewMemoryStore.
func NewMemoryCNIDStore() *MemoryCNIDStore { return cnid.NewMemoryStore() }

// NewSQLiteCNIDStore is the AFP-package alias for cnid.NewSQLiteStore.
func NewSQLiteCNIDStore(volumeRootPath string) (*SQLiteCNIDStore, error) {
	return cnid.NewSQLiteStore(volumeRootPath)
}

// CNIDBackend creates a per-volume CNID store. The backend abstraction
// stays in service/afp because it is coupled to the AFP Volume type;
// later commits may introduce a pkg/cnid Factory if other services need
// per-volume backend selection.
type CNIDBackend interface {
	Open(volume Volume) CNIDStore
}

// MemoryCNIDBackend provides the default non-persistent CNID implementation.
type MemoryCNIDBackend struct{}

func (MemoryCNIDBackend) Open(volume Volume) CNIDStore {
	return cnid.NewMemoryStore()
}

// SQLiteCNIDBackend stores CNIDs in a per-volume SQLite database.
type SQLiteCNIDBackend struct{}

func (SQLiteCNIDBackend) Open(volume Volume) CNIDStore {
	store, err := cnid.NewSQLiteStore(volume.Config.Path)
	if err != nil {
		netlog.Warn("[AFP][CNID] sqlite init failed for volume=%q path=%q: %v; falling back to memory", volume.Config.Name, volume.Config.Path, err)
		return cnid.NewMemoryStore()
	}
	return store
}

func resolveCNIDBackend(options Options) CNIDBackend {
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
