package fs

// MetaEngine is the single per-share interface for everything the storage seam
// tracks about a path beyond its bytes: derived DOS/AFP names, CNIDs, and DOS
// attributes/dates a host filesystem cannot natively represent. It plays the
// same mandatory, share-scoped role ForkEngine plays for resource forks/Finder
// info — BuildShare always resolves exactly one MetaEngine (never a null/no-op
// state), selected per share via the registry in meta_registry.go and defaulted
// per host platform in withDefaults.
//
// CNID is always backed by an internal metastore instance regardless of which
// MetaEngine backend a share resolves to (see meta_store.go) — its prefix-scan
// subtree-rebind semantics (renaming a directory cheaply rebinds every
// descendant) don't map onto a single native attribute/stream value, and unlike
// Finder info it has no SFM/Netatalk interop reason to live in a native
// attribute. A "native" MetaEngine backend (xattr on Linux, ADS on Windows) is
// native for names/attrs/dates only.
type MetaEngine interface {
	// ShortName returns the derived 8.3 DOS name for long in dir, allocating and
	// persisting a fresh one (with a ~N collision suffix) the first time a given
	// long name is seen in that directory. A long name that already fits 8.3 is
	// bound and returned as-is (no synthetic suffix).
	ShortName(dir, long string) string
	// MediumName returns the derived 31-character classic-AFP name for long in
	// dir, allocating and persisting a fresh one (with a -N collision suffix) the
	// first time. A long name that already fits 31 characters is bound as-is.
	MediumName(dir, long string) string
	// ToLong reverses ShortName/MediumName: the long name a derived name maps to
	// in dir, for the given kind. ok is false when derived is not a name this
	// engine has bound (e.g. a client echoed back something it invented).
	ToLong(dir, derived string, kind NameKind) (long string, ok bool)

	// RootCNID returns the volume root CNID (AFP's well-known CNIDRoot).
	RootCNID() uint32
	// CNID returns the CNID bound to path, if any.
	CNID(path string) (cnid uint32, ok bool)
	// EnsureCNID returns the CNID for path, allocating a fresh one on first sight.
	EnsureCNID(path string) uint32
	// PathForCNID returns the path bound to cnid, if any.
	PathForCNID(cnid uint32) (path string, ok bool)
	// RebindCNID moves path (and its subtree) from oldPath to newPath, preserving
	// CNIDs — called after a rename.
	RebindCNID(oldPath, newPath string) error
	// RemoveCNID deletes path and its subtree from the CNID mapping — called
	// after a remove.
	RemoveCNID(path string) error

	// Attrs returns the stored DOS attributes/dates for path. ok is false when
	// nothing is stored (the caller then derives attributes from the entry).
	Attrs(path string) (attr DOSAttr, ok bool)
	// SetAttrs persists attr for path.
	SetAttrs(path string, attr DOSAttr) error
	// DeleteAttrs drops any stored attributes for path (called on remove).
	DeleteAttrs(path string) error
	// RenameAttrs moves stored attributes from oldPath to newPath (called on
	// rename), preserving them across a move.
	RenameAttrs(oldPath, newPath string) error
}
