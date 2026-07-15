package fs

import (
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
)

// meta_xattr.go registers the "xattr" MetaEngine backend, the default on Linux
// (withDefaults). Name derivation and CNID tracking stay metastore-backed —
// collision-free 8.3 derivation needs to see a directory's siblings, and CNID's
// prefix-scan subtree-rebind needs a range-scannable store, neither of which a
// single per-file xattr value can replace — but DOS attributes/dates prefer the
// host's own extended attributes via buildDOSAttrStore's "xattr" backend
// (a Samba-compatible user.DOSATTRIB xattr, degrading to a sidecar when the host
// doesn't actually support xattrs), rather than the bare metastore meta_store.go
// falls back to. This is the SAME xattr mechanism dosattr.go already drives
// (hostXattrDOSAttr); it is intentionally distinct from fork_xattr.go's
// Netatalk-compatible "org.netatalk.*" EAs, which must stay byte-compatible with
// Netatalk and are not reused here.
func init() {
	RegisterMetaEngine("xattr", func(spec ShareSpec, base FileSystem, store metastore.Store) (MetaEngine, error) {
		return newMetaXattrEngine(spec, base, store, nil), nil
	})
}

func newMetaXattrEngine(spec ShareSpec, base FileSystem, store metastore.Store, logger log.Logger) *metaStoreEngine {
	if store == nil {
		store, _ = metastore.NewMem("")
	}
	if logger == nil {
		logger = log.New("meta.xattr")
	}
	cnids := metastore.NewCNIDStore(store)
	cnids.EnsureReserved("", cnids.RootID())
	return &metaStoreEngine{
		names:   NewDerivedNameEngine(store),
		cnids:   cnids,
		attrs:   buildDOSAttrStore(dosBackendXattr, base, store, logger.With(log.Str("component", "dosattr"))),
		eas:     metastore.NewEAStore(store, logger.With(log.Str("component", "eastore"))),
		logging: logger,
	}
}
