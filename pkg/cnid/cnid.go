// Package cnid tracks the mapping between AFP Catalog Node IDs and
// current filesystem paths for a single volume. The package is AFP-
// agnostic — future services (macgarden, others) can reuse the Store
// interface and its in-memory and SQLite implementations without
// pulling in anything from service/afp.
package cnid

const (
	// Invalid signals an error or "no CNID" sentinel.
	Invalid uint32 = 0
	// ParentOfRoot is the synthetic parent of the root directory.
	ParentOfRoot uint32 = 1
	// Root identifies a volume's root directory.
	Root uint32 = 2
	// firstDynamic is the first CNID assignable to non-root objects.
	firstDynamic uint32 = 3
)

// Store tracks CNID <-> path bindings. Implementations must be safe for
// concurrent use. Callers treat paths as opaque strings but are free to
// expect that path.Clean-equivalent normalisation happens internally.
type Store interface {
	RootID() uint32
	Path(cnid uint32) (string, bool)
	CNID(path string) (uint32, bool)
	Ensure(path string) uint32
	EnsureReserved(path string, cnid uint32) uint32
	Rebind(oldPath, newPath string)
	Remove(path string)
}
