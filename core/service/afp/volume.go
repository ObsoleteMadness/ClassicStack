package afp

import (
	stdfs "io/fs"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
)

// Volume is one AFP share re-expressed over the §9 storage seam. It binds a
// fs.ForkFS (the FileSystem + ForkEngine the share was built with), the share's
// FilenameCodec, and a metastore.CNIDStore — and nothing else. The volume holds
// NO storage-layout knowledge: it never imports path/filepath, never branches on
// runtime.GOOS, and never knows whether forks live in AppleDouble sidecars, NTFS
// streams, or Netatalk EAs. Every catalog operation flows through the seam.
//
// Store paths are always '/'-separated regardless of host (the FileSystem and
// CNIDStore both use this convention); the codec's ReservedSet — not the volume —
// decides which characters a given backend can hold.
type Volume struct {
	id    uint16
	name  string
	fsys  fs.ForkFS
	codec fs.FilenameCodec
	cnids *metastore.CNIDStore
}

// VolumeSpec names a share and the seam components to build it from. It mirrors
// fs.ShareSpec plus the AFP-facing volume id/name; the service turns each spec
// into a Volume via NewVolume.
type VolumeSpec struct {
	ID    uint16
	Name  string
	Share fs.ShareSpec
}

// NewVolume builds one Volume from a spec by assembling the share stack through
// fs.BuildShare (which validates the fs_type×fork_backend×filename_codec triple),
// then binding a CNIDStore over the same metastore the share uses. The returned
// volume consumes only seam interfaces from then on.
func NewVolume(spec VolumeSpec) (*Volume, error) {
	share, err := fs.BuildShare(spec.Share, nil)
	if err != nil {
		return nil, err
	}
	codec := codecOf(share)

	// CNID tracking rides the same metastore kind the share declares, so a
	// mem-snapshotted volume and a sqlite volume preserve CNIDs identically
	// across restarts (spec/16 §3). An empty path keeps it volatile.
	store, err := metastore.Open(metastoreKind(spec.Share), "")
	if err != nil {
		return nil, err
	}
	cnids := metastore.NewCNIDStore(store)
	// The root directory always exists and owns the well-known root CNID.
	cnids.EnsureReserved("", cnids.RootID())

	return &Volume{
		id:    spec.ID,
		name:  spec.Name,
		fsys:  share,
		codec: codec,
		cnids: cnids,
	}, nil
}

// ID returns the AFP volume id.
func (v *Volume) ID() uint16 { return v.id }

// Name returns the volume's display name.
func (v *Volume) Name() string { return v.name }

// codecOf reaches the FilenameCodec a built share carries, falling back to the
// MacRoman↔UTF-8 default if the share doesn't expose one (it always does today).
func codecOf(share fs.ForkFS) fs.FilenameCodec {
	if c, ok := share.(fs.Coded); ok {
		if codec := c.Codec(); codec != nil {
			return codec
		}
	}
	return fs.NewMacRomanUTF8FilenameCodec()
}

// metastoreKind returns the metastore kind a share's CNID store should use,
// defaulting to "mem" so the CNID registry works with no SQLite linked.
func metastoreKind(spec fs.ShareSpec) string {
	if spec.Metastore != "" {
		return spec.Metastore
	}
	return "mem"
}

// --- catalog operations, all over the seam ---

// ResolvePath walks an AFP pathname relative to parent and returns the store path
// of the target. The pathname is null-separated CNode names (a leading null is
// ignored; consecutive nulls ascend the tree). Each element is decoded from the
// request's wire charset — selected by pathType, threaded into the share codec —
// to the store-native name; an element the store charset cannot represent yields
// ErrUnrepresentable (→ AFP "illegal name") rather than a mangled path.
func (v *Volume) ResolvePath(parent, pathname string, pathType uint8) (string, error) {
	wire := wireFor(pathType)
	cur := parent

	if len(pathname) > 0 && pathname[0] == 0x00 {
		pathname = pathname[1:]
	}
	elements := strings.Split(pathname, "\x00")
	for i, el := range elements {
		if el == "" {
			// A trailing empty element is the terminating null; ignore it.
			// An interior empty element ascends one level toward the root.
			if i == len(elements)-1 {
				continue
			}
			cur = ascend(cur)
			continue
		}
		stored, err := v.codec.Decode([]byte(el), wire)
		if err != nil {
			return "", err
		}
		elem := string(stored)
		if elem == ".." {
			// Already handled via the empty-element ascend convention; an
			// explicit ".." element is rejected as an illegal name.
			return "", fs.ErrUnrepresentable
		}
		cur = joinStore(cur, elem)
	}
	return cur, nil
}

// EncodeName renders a store-native name back to the wire charset selected by
// pathType, for packing into a catalog reply. A name unrepresentable in the
// client's charset yields ErrUnrepresentable so the service can substitute or
// fail loudly rather than emit garbage.
func (v *Volume) EncodeName(stored string, pathType uint8) ([]byte, error) {
	return v.codec.Encode(fs.StoredName(stored), wireFor(pathType))
}

// CNID returns the catalog node id for a store path, allocating one on first
// sight. The mapping rides the volume's metastore, so it persists according to
// the store kind without the volume knowing which.
func (v *Volume) CNID(path string) uint32 { return v.cnids.Ensure(path) }

// PathForCNID reverses CNID: the store path a node id maps to.
func (v *Volume) PathForCNID(cnid uint32) (string, bool) { return v.cnids.Path(cnid) }

// Enumerate lists the children of a directory as store-native dir entries.
// Catalog packing (encoding names back to the wire charset, attaching CNIDs and
// fork lengths) is the caller's concern — done through EncodeName, CNID, and the
// fork engine — so the volume stays free of protocol-packing knowledge.
func (v *Volume) Enumerate(path string) ([]stdfs.DirEntry, error) {
	return v.fsys.ReadDir(path)
}

// Stat returns store-native metadata for a path.
func (v *Volume) Stat(path string) (stdfs.FileInfo, error) { return v.fsys.Stat(path) }

// OpenFork opens the data or resource fork of a file through the share's fork
// engine — whichever container (AppleDouble, ads, xattr) the share was built
// with. The volume neither knows nor cares which.
func (v *Volume) OpenFork(path string, fork fs.ForkType, flag int) (fs.File, error) {
	return v.fsys.OpenFork(path, fork, flag)
}

// ForkLen reports a fork's length through the fork engine.
func (v *Volume) ForkLen(path string, fork fs.ForkType) (int64, error) {
	return v.fsys.ForkLen(path, fork)
}

// FinderInfo reads the 32-byte FinderInfo for a path through the fork engine.
func (v *Volume) FinderInfo(path string) (info [32]byte, ok bool, err error) {
	return v.fsys.ReadFinderInfo(path)
}

// SetFinderInfo writes the 32-byte FinderInfo through the fork engine.
func (v *Volume) SetFinderInfo(path string, info [32]byte) error {
	return v.fsys.WriteFinderInfo(path, info)
}

// Rename moves a path, carrying its metadata sidecar/stream and rebinding the
// CNID subtree so node ids survive the move.
func (v *Volume) Rename(old, new string) error {
	if err := v.fsys.Rename(old, new); err != nil {
		return err
	}
	if err := v.fsys.MoveMetadata(old, new); err != nil {
		return err
	}
	v.cnids.Rebind(old, new)
	return nil
}

// Remove deletes a path, its metadata, and its CNID subtree.
func (v *Volume) Remove(path string) error {
	if err := v.fsys.DeleteMetadata(path); err != nil {
		return err
	}
	if err := v.fsys.Remove(path); err != nil {
		return err
	}
	v.cnids.Remove(path)
	return nil
}

// Capabilities reports the optional behaviours the share's FileSystem supports.
func (v *Volume) Capabilities() fs.Capabilities { return v.fsys.Capabilities() }

// --- store-path helpers (no path/filepath: store paths are always '/'-joined) ---

// joinStore appends one element to a '/'-separated store path.
func joinStore(dir, elem string) string {
	if dir == "" {
		return elem
	}
	return dir + "/" + elem
}

// ascend returns the parent of a '/'-separated store path; the root ("") is its
// own parent.
func ascend(path string) string {
	i := strings.LastIndexByte(path, '/')
	if i < 0 {
		return ""
	}
	return path[:i]
}
