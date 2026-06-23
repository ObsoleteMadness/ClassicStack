package metastore

import "time"

// DOS/FAT file-attribute bits, the cross-service vocabulary every file service
// (SMB, EtherDFS, NCP, AFP) maps onto. These match the FILE_ATTRIBUTE_* /
// SMB_FILE_ATTRIBUTES low byte so a value round-trips unchanged through the
// Windows-native backend.
const (
	DOSReadOnly  uint16 = 0x0001 // FILE_ATTRIBUTE_READONLY
	DOSHidden    uint16 = 0x0002 // FILE_ATTRIBUTE_HIDDEN
	DOSSystem    uint16 = 0x0004 // FILE_ATTRIBUTE_SYSTEM
	DOSVolume    uint16 = 0x0008 // FILE_ATTRIBUTE_VOLUME_ID (volume label)
	DOSDirectory uint16 = 0x0010 // FILE_ATTRIBUTE_DIRECTORY
	DOSArchive   uint16 = 0x0020 // FILE_ATTRIBUTE_ARCHIVE

	// DOSStorableMask is the set of attribute bits a host filesystem cannot infer
	// and that therefore must be persisted: read-only, hidden, system, archive.
	// Directory/volume are structural (derived from the entry), not stored.
	DOSStorableMask = DOSReadOnly | DOSHidden | DOSSystem | DOSArchive
)

// DOSAttr is the persisted DOS metadata for one path: the stored attribute bits
// plus the DOS create-time (which no POSIX filesystem records). It is the
// ClassicStack equivalent of Samba's XATTR_DOSINFO record — the same fields a
// non-DOS host cannot represent and so must keep on the side.
type DOSAttr struct {
	// Attrs is the stored attribute bitmask (DOSStorableMask subset). Structural
	// bits (Directory/Volume) are not persisted; a reader ORs them in from the
	// entry kind.
	Attrs uint16
	// CreateTime is the DOS/Windows creation timestamp. Zero means "unknown" — a
	// reader then falls back to the host mtime.
	CreateTime time.Time
}

// Has reports whether attribute bit a is set.
func (d DOSAttr) Has(a uint16) bool { return d.Attrs&a != 0 }

// DOSAttrStore persists DOS file attributes for paths that the host filesystem
// cannot natively represent. It is the typed facade the file services use; the
// concrete backend (metastore KV, a Samba-compatible user.DOSATTRIB xattr, a
// sidecar, or Windows-native passthrough) is selected per share and is swappable.
// Paths are the share's '/'-separated store paths.
type DOSAttrStore interface {
	// Get returns the stored attributes for path. ok is false when nothing is
	// stored (the caller then derives attributes from the entry).
	Get(path string) (attr DOSAttr, ok bool)
	// Set persists attr for path. The structural bits (Directory/Volume) are
	// ignored; only DOSStorableMask is kept.
	Set(path string, attr DOSAttr) error
	// Delete drops any stored attributes for path (called on remove).
	Delete(path string) error
	// Rename moves stored attributes from oldPath to newPath (called on rename),
	// preserving them across a move.
	Rename(oldPath, newPath string) error
}

// metaDOSAttrStore is the metastore-backed DOSAttrStore: the definitive per-share
// implementation (sqlite by default, mem for embedded/TinyGo builds), and the
// cache layer the interop backends (xattr/native) write through. It is the tdb
// equivalent of Samba's attribute database.
type metaDOSAttrStore struct {
	store Store
}

// NewDOSAttrStore returns a metastore-backed DOSAttrStore over store (nil → a
// volatile in-memory store, so a placeholder share still works).
func NewDOSAttrStore(store Store) DOSAttrStore {
	if store == nil {
		store, _ = NewMem("")
	}
	return &metaDOSAttrStore{store: store}
}

// metastore key layout (one share per store; callers scope by store):
//
//	"d/a/<path>" -> XATTR_DOSINFO v3 blob (attrs + create-time)
func dosAttrKey(path string) []byte { return []byte("d/a/" + cleanPath(path)) }

func (s *metaDOSAttrStore) Get(path string) (DOSAttr, bool) {
	v, ok := s.store.Get(dosAttrKey(path))
	if !ok {
		return DOSAttr{}, false
	}
	attr, err := DecodeDOSInfo(v)
	if err != nil {
		return DOSAttr{}, false
	}
	return attr, true
}

func (s *metaDOSAttrStore) Set(path string, attr DOSAttr) error {
	attr.Attrs &= DOSStorableMask
	return s.store.Put(dosAttrKey(path), EncodeDOSInfo(attr))
}

func (s *metaDOSAttrStore) Delete(path string) error {
	return s.store.Delete(dosAttrKey(path))
}

func (s *metaDOSAttrStore) Rename(oldPath, newPath string) error {
	v, ok := s.store.Get(dosAttrKey(oldPath))
	if !ok {
		return nil
	}
	if err := s.store.Put(dosAttrKey(newPath), v); err != nil {
		return err
	}
	return s.store.Delete(dosAttrKey(oldPath))
}
