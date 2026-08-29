package fs

import (
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
)

// meta_ads.go registers the "ads" MetaEngine backend, the default on an
// NTFS-backed Windows share (withDefaults). Like meta_xattr.go, name derivation
// and CNID tracking stay metastore-backed (collision-free 8.3 derivation and
// CNID's subtree-rebind both need a range-scannable store); DOS attributes/dates
// prefer the host's own NTFS attributes via buildDOSAttrStore's "native" backend
// (dosattr.go's hostNativeDOSAttr), degrading to a sidecar when the host isn't
// actually NTFS-backed. Distinct from fork_ads.go's SFM-compatible
// "AFP_AfpInfo"/"AFP_Resource" streams, which must stay byte-compatible with
// Services for Macintosh and are not reused here.
func init() {
	RegisterMetaEngine("ads", func(spec ShareSpec, base FileSystem, store metastore.Store) (MetaEngine, error) {
		return newMetaADSEngine(spec, base, store, nil), nil
	})
}

func newMetaADSEngine(spec ShareSpec, base FileSystem, store metastore.Store, logger log.Logger) *metaStoreEngine {
	if store == nil {
		store, _ = metastore.NewMem("")
	}
	if logger == nil {
		logger = log.New("meta.ads")
	}
	cnids := metastore.NewCNIDStore(store)
	cnids.EnsureReserved("", cnids.RootID())
	return &metaStoreEngine{
		names:   NewDerivedNameEngine(store),
		cnids:   cnids,
		attrs:   buildDOSAttrStore(dosBackendNative, base, store, logger.With(log.Str("component", "dosattr"))),
		eas:     metastore.NewEAStore(store, logger.With(log.Str("component", "eastore"))),
		logging: logger,
	}
}
