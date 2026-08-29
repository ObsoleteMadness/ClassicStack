//go:build sqlite || all

package registry

// Blank-import the SQLite metastore adapter so its init() registers the "sqlite"
// store kind whenever this binary is built with the sqlite (or all) tag. A share
// configured with Metastore="sqlite" then persists CNID/shortname/desktop
// entries; the default build links no SQLite and falls back to the mem store.
import _ "github.com/ObsoleteMadness/ClassicStack/adapter/metastore/sqlite"
