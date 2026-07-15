package fs

import (
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
)

// meta_store.go is the metastore-backed MetaEngine: the universal fallback that
// works on any host (memfs, zipfs, network shares, a host with no ADS/xattr
// support) — the same role "nofork" plays for ForkEngine, except MetaEngine has
// no true no-op mode since 8.3 name derivation must always work for a DOS
// client. It composes three previously-separate pieces behind one interface:
// name derivation (core/fs/name.go's derivedNameEngine), CNID tracking
// (core/metastore.CNIDStore), and DOS attributes/dates
// (core/metastore.DOSAttrStore) — all now sharing the ONE metastore.Store a
// share's BuildShare opens, instead of AFP's CNID store and the name/attr
// stores being disconnected instances as they were before this file existed.
func init() {
	RegisterMetaEngine("metastore", func(spec ShareSpec, base FileSystem, store metastore.Store) (MetaEngine, error) {
		return newMetaStoreEngine(spec, base, store, nil), nil
	})
}

type metaStoreEngine struct {
	names   NameEngine // core/fs/name.go's derivedNameEngine
	cnids   *metastore.CNIDStore
	attrs   DOSAttrStore      // buildDOSAttrStore's native→xattr→sidecar→metastore chain
	eas     metastore.EAStore // metastore-backed; shared by every MetaEngine backend
	logging log.Logger        // established at construction, never nil; sinks own level filtering
}

// newMetaStoreEngine builds the metastore-backed MetaEngine over store (nil → a
// volatile in-memory store). A nil logger gets a no-op logger. Attrs are built
// via the existing buildDOSAttrStore "auto" preference chain (native → xattr →
// sidecar → metastore) so this fallback MetaEngine still prefers a host-native
// attribute store when one is available, not just the bare metastore.
func newMetaStoreEngine(spec ShareSpec, base FileSystem, store metastore.Store, logger log.Logger) *metaStoreEngine {
	if store == nil {
		store, _ = metastore.NewMem("")
	}
	if logger == nil {
		logger = log.New("meta.store")
	}
	cnids := metastore.NewCNIDStore(store)
	cnids.EnsureReserved("", cnids.RootID())
	return &metaStoreEngine{
		names:   NewDerivedNameEngine(store),
		cnids:   cnids,
		attrs:   buildDOSAttrStore(dosBackendAuto, base, store, logger.With(log.Str("component", "dosattr"))),
		eas:     metastore.NewEAStore(store, logger.With(log.Str("component", "eastore"))),
		logging: logger,
	}
}

func (e *metaStoreEngine) ShortName(dir, long string) string {
	got := e.names.Bind(dir, long, ShortName)
	e.logging.Log2(log.Debug, "derived short name", log.Str("long", long), log.Str("short", got))
	return got
}

func (e *metaStoreEngine) MediumName(dir, long string) string {
	got := e.names.Bind(dir, long, MediumName)
	e.logging.Log2(log.Debug, "derived medium name", log.Str("long", long), log.Str("medium", got))
	return got
}

func (e *metaStoreEngine) ToLong(dir, derived string, kind NameKind) (string, bool) {
	long, ok := e.names.ToLong(dir, derived, kind)
	if !ok {
		e.logging.Log1(log.Debug, "name reverse-lookup miss", log.Str("derived", derived))
	}
	return long, ok
}

func (e *metaStoreEngine) RootCNID() uint32 { return e.cnids.RootID() }

func (e *metaStoreEngine) CNID(path string) (uint32, bool) {
	cnid, ok := e.cnids.CNID(path)
	if !ok {
		e.logging.Log1(log.Debug, "cnid cache miss", log.Str("path", path))
	}
	return cnid, ok
}

func (e *metaStoreEngine) EnsureCNID(path string) uint32 {
	cnid := e.cnids.Ensure(path)
	e.logging.Log2(log.Debug, "cnid ensured", log.Str("path", path), log.Int("cnid", int64(cnid)))
	return cnid
}

func (e *metaStoreEngine) PathForCNID(cnid uint32) (string, bool) {
	path, ok := e.cnids.Path(cnid)
	if !ok {
		e.logging.Log1(log.Debug, "cnid path-lookup miss", log.Int("cnid", int64(cnid)))
	}
	return path, ok
}

func (e *metaStoreEngine) RebindCNID(oldPath, newPath string) error {
	e.logging.Log2(log.Debug, "cnid rebind", log.Str("old", oldPath), log.Str("new", newPath))
	e.cnids.Rebind(oldPath, newPath)
	return nil
}

func (e *metaStoreEngine) RemoveCNID(path string) error {
	e.logging.Log1(log.Debug, "cnid remove", log.Str("path", path))
	e.cnids.Remove(path)
	return nil
}

func (e *metaStoreEngine) Attrs(path string) (DOSAttr, bool) { return e.attrs.Get(path) }

func (e *metaStoreEngine) SetAttrs(path string, attr DOSAttr) error {
	return e.attrs.Set(path, attr)
}

func (e *metaStoreEngine) DeleteAttrs(path string) error { return e.attrs.Delete(path) }

func (e *metaStoreEngine) RenameAttrs(oldPath, newPath string) error {
	return e.attrs.Rename(oldPath, newPath)
}

func (e *metaStoreEngine) EAs(path string) ([]EA, bool) { return e.eas.Get(path) }

func (e *metaStoreEngine) SetEAs(path string, eas []EA) error {
	return e.eas.Set(path, eas)
}

func (e *metaStoreEngine) DeleteEAs(path string) error { return e.eas.Delete(path) }

func (e *metaStoreEngine) RenameEAs(oldPath, newPath string) error {
	return e.eas.Rename(oldPath, newPath)
}

var _ MetaEngine = (*metaStoreEngine)(nil)
