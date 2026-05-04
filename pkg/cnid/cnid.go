// Package cnid tracks the mapping between AFP Catalog Node IDs and
// current filesystem paths for a single volume, and additionally
// holds the per-volume 8.3 shortname bindings used by AFP's
// PathTypeShortNames and by SMB/DOS clients. The package is AFP-
// agnostic — any service can reuse the Store interface and its
// in-memory and SQLite implementations.
package cnid

import "github.com/ObsoleteMadness/ClassicStack/pkg/shortname"

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

// Store tracks CNID <-> path bindings and the per-volume shortname
// mapping. Implementations must be safe for concurrent use. Callers
// treat paths as opaque strings but are free to expect that
// path.Clean-equivalent normalisation happens internally.
//
// Embedding shortname.Store keeps shortname the conceptually general
// primitive (used by SMB, AFP, DOS clients) and CNID the per-volume
// composite that bundles shortname bindings with CNID/path tracking,
// without forcing a circular dependency.
type Store interface {
	RootID() uint32
	Path(cnid uint32) (string, bool)
	CNID(path string) (uint32, bool)
	Ensure(path string) uint32
	EnsureReserved(path string, cnid uint32) uint32
	Rebind(oldPath, newPath string)
	Remove(path string)
	shortname.Store
}
