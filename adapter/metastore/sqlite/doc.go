// Package sqlite is the SQLite-backed metastore.Store adapter (build tag
// "sqlite" or "all"). It registers the "sqlite" store kind so a share with
// Metastore="sqlite" persists its CNID / shortname / desktop entries in a
// single-table keyed database file, while the default build links no SQLite at
// all and falls back to the in-memory store.
//
// Ring: ADAPTER (implements core/metastore.Store). Importing this package for
// its init() side-effect is enough to make the kind available:
//
//	import _ "github.com/ObsoleteMadness/ClassicStack/adapter/metastore/sqlite"
package sqlite
