package afp

import (
	stdfs "io/fs"
	"strings"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
	"github.com/ObsoleteMadness/ClassicStack/core/share"
)

// Volume is one AFP share: the AFP-facing id over a shared share.Share (the bound
// fs.ForkFS + the config that built it), reaching CNID tracking through the
// share's own fs.MetaEngine (sh.FS().Meta()) rather than a separate store — a
// same-share AFP volume and SMB share now see the SAME CNID/name/attr state
// instead of two disconnected metastore instances. It holds NO storage-layout
// knowledge: it never imports path/filepath, never branches on runtime.GOOS, and
// never knows whether forks live in AppleDouble sidecars, NTFS streams, or
// Netatalk EAs — it reaches the filesystem only through v.FS(). Its only
// additions over the shared share are the AFP id and the AFP wire-path codec
// threading.
//
// Store paths are always '/'-separated regardless of host (the FileSystem and
// MetaEngine both use this convention); the codec's ReservedSet — not the volume —
// decides which characters a given backend can hold.
type Volume struct {
	id uint16
	sh *share.Share

	dtOnce sync.Once
	dt     *desktopDB // lazily-built Desktop database (icons + APPL mappings)

	extMap *ExtensionMap // default type/creator by extension; nil = none

	// sizeLimit is the volume size reported to AFP clients, in bytes; 0 = the
	// classic-friendly default (defaultVolumeSizeLimit). Presentation only —
	// classic clients derive their allocation-block size from the reported
	// BytesTotal, so this sets the Finder's "size on disk" granularity.
	sizeLimit uint64
}

// SizeLimit returns the volume size reported to AFP clients, in bytes.
func (v *Volume) SizeLimit() uint64 {
	if v.sizeLimit == 0 {
		return defaultVolumeSizeLimit
	}
	return v.sizeLimit
}

// meta returns the share's mandatory MetaEngine — the single source of CNID
// tracking (and derived names/attrs) for this volume.
func (v *Volume) meta() fs.MetaEngine { return v.sh.FS().Meta() }

// SetExtensionMap installs the extension→type/creator map this volume consults to
// default Finder info for files that have none stored. A nil map disables defaulting.
func (v *Volume) SetExtensionMap(m *ExtensionMap) { v.extMap = m }

// VolumeSpec names a share and the seam components to build it from. It mirrors
// fs.ShareSpec plus the AFP-facing volume id/name; the service turns each spec
// into a Volume via NewVolume.
type VolumeSpec struct {
	ID    uint16
	Name  string
	Share fs.ShareSpec
	// ExtMap is the optional extension→type/creator default map the volume consults for
	// files with no stored Finder info. Built by the compose/cmd edge (which reads the
	// configured ExtMapPath, or DefaultExtMapPath when empty); nil = no defaulting.
	ExtMap *ExtensionMap
	// SizeLimit is the reported volume size in bytes (the section's size_limit,
	// MiB, converted at the compose edge); 0 = the classic-friendly default.
	SizeLimit uint64
}

// NewVolume builds one Volume from a spec with no FS-mutation bus (the bus-less
// path used by tests and the zero-config default). A volume built this way is
// isolated: its FS publishes to a private bus no one else holds, so it cannot
// coordinate with a same-host-path SMB share (§10d). Production builds go through
// NewVolumeWithBus, which the service feeds the shared per-host-path bus.
func NewVolume(spec VolumeSpec) (*Volume, error) {
	return NewVolumeWithBus(spec, nil)
}

// NewVolumeWithBus builds one Volume, assembling the share stack through
// share.Build over the supplied FS-mutation bus (§10d): when an AFP volume and an
// SMB share back the same host path, the service hands them the SAME bus so a
// mutation by one reaches the other. A nil bus means "isolated" (share.Build then
// makes a private one). CNID tracking rides the share's own fs.MetaEngine
// (sh.FS().Meta()) — the ONE metastore.Store BuildShare already opened for
// names/CNID/attrs — instead of this volume opening a second, disconnected
// store as it did before the MetaEngine consolidation. A same-bus SMB share
// therefore sees the identical CNID/name/attr state, not a separate copy.
func NewVolumeWithBus(spec VolumeSpec, b bus.Bus) (*Volume, error) {
	spec.Share.Name = spec.Name
	// Stamp this service's origin onto the FS mutations this volume produces, so a
	// same-bus SMB share's reactor acts on them and AFP's own reactor skips them
	// (§10d). OriginBus is a no-op when b is nil.
	sh, err := share.Build(spec.Share, fs.OriginBus(b, OriginAFP))
	if err != nil {
		return nil, err
	}

	// The root directory always exists and owns the well-known root CNID.
	// BuildShare's MetaEngine already reserves it (meta_store.go/meta_xattr.go/
	// meta_ads.go all call EnsureReserved("", RootID()) at construction), but the
	// call is idempotent so repeating it here is harmless and self-documenting.
	meta := sh.FS().Meta()
	meta.EnsureCNID("")

	return &Volume{id: spec.ID, sh: sh, sizeLimit: spec.SizeLimit}, nil
}

// ID returns the AFP volume id.
func (v *Volume) ID() uint16 { return v.id }

// Name returns the volume's display name.
func (v *Volume) Name() string { return v.sh.Name() }

// allows reports whether the session identity may see/open this volume, per the
// share's access allow-list. An empty (guest) identity is admitted only by a
// guest-open volume.
func (v *Volume) allows(user string) bool { return v.sh.Permissions().Allows(user) }

// FS returns the bound filesystem. AFP dispatch reaches catalog/fork operations
// through it (v.FS().Stat(p), v.FS().OpenFork(p, fork, flag), v.FS().ReadDir(p),
// v.FS().DiskUsage(p)); the FS carries fork metadata on Rename/Remove.
func (v *Volume) FS() fs.ForkFS { return v.sh.FS() }

// Close releases the bound filesystem's GC-invisible resources (fs.FSCloser); a no-op
// for a backend that owns none. Called at service Stop, not on RemoveShare.
func (v *Volume) Close() error { return v.sh.Close() }

// codec is the share's FilenameCodec, threaded per request with the AFP wire
// charset (selected by the path-type byte).
func (v *Volume) codec() fs.FilenameCodec { return v.sh.Codec() }

// ensureDesktop builds the volume's Desktop database on first FPOpenDT. The
// database (icons + APPL mappings) is volume-scoped state shared by every session
// that opens the Desktop, so it is created once and lives for the volume's life.
func (v *Volume) ensureDesktop() { v.dtOnce.Do(func() { v.dt = newDesktopDB() }) }

// desktop returns the volume's Desktop database, building it if a command reaches
// it before FPOpenDT (defensive — the dispatch path always opens it first).
func (v *Volume) desktop() *desktopDB {
	v.ensureDesktop()
	return v.dt
}

// --- AFP-specific path/CNID operations; catalog ops are FS ops via v.FS() ---

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
		stored, err := v.codec().Decode([]byte(el), wire)
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
	return v.codec().Encode(fs.StoredName(stored), wireFor(pathType))
}

// CNID returns the catalog node id for a store path, allocating one on first
// sight. The mapping rides the share's MetaEngine, so it persists according to
// the store kind without the volume knowing which.
func (v *Volume) CNID(path string) uint32 { return v.meta().EnsureCNID(path) }

// PathForCNID reverses CNID: the store path a node id maps to.
func (v *Volume) PathForCNID(cnid uint32) (string, bool) { return v.meta().PathForCNID(cnid) }

// ParentCNID returns the catalog node id of a path's parent directory. The
// volume root's parent is the synthetic CNIDParentOfRoot (1), per AFP (Inside
// Macintosh: Networking, "Directory parameters" — ParentDirID of the root is 1).
func (v *Volume) ParentCNID(path string) uint32 {
	if path == "" {
		return metastore.CNIDParentOfRoot
	}
	return v.meta().EnsureCNID(ascend(path))
}

// Enumerate lists the children of a directory as store-native dir entries.
// Catalog packing (encoding names back to the wire charset, attaching CNIDs and
// fork lengths) is the caller's concern — done through EncodeName, CNID, and the
// fork engine — so the volume stays free of protocol-packing knowledge. It is a
// thin pass to v.FS().ReadDir; the dispatch may equally call v.FS() directly.
func (v *Volume) Enumerate(path string) ([]stdfs.DirEntry, error) {
	return v.FS().ReadDir(path)
}

// Stat returns store-native metadata for a path (thin pass to v.FS().Stat).
func (v *Volume) Stat(path string) (stdfs.FileInfo, error) { return v.FS().Stat(path) }

// ForkLen reports a fork's length through the fork engine.
func (v *Volume) ForkLen(path string, fork fs.ForkType) (int64, error) {
	return v.FS().ForkLen(path, fork)
}

// FinderInfo reads the 32-byte AFP Finder info (16-byte FInfo + 16-byte
// FXInfo) for a path through the fork engine. A path with no stored Finder info
// reports the zero record (ok == false), which the catalog packer emits as 32
// zero bytes — the AFP convention for "no Finder info yet".
func (v *Volume) FinderInfo(path string) (info [32]byte, ok bool) {
	fi, present, err := v.FS().ReadFinderInfo(path)
	if err == nil && present {
		return fi, true
	}
	// No stored Finder info: fall back to the extension map's default type/creator
	// (e.g. a `.txt` → TEXT/ttxt) so a file copied in without classic metadata still
	// opens with the right application on the Mac. A path with no extension or no
	// matching entry stays "no Finder info" (32 zero bytes), the prior behaviour.
	if mp, hit := v.extMap.Lookup(path); hit {
		return mp.FinderInfo(), true
	}
	return [32]byte{}, false
}

// SetFinderInfo persists the 32-byte AFP Finder info for a path through the fork
// engine (the write side of FinderInfo, used by FPSetFileDirParms/Set*Parms).
func (v *Volume) SetFinderInfo(path string, info [32]byte) error {
	return v.FS().WriteFinderInfo(path, info)
}

// createTime returns a path's stored DOS/AFP creation time via the share's
// MetaEngine, falling back to the host mtime when nothing is stored (first-ever
// stat of a file that predates the MetaEngine attrs, or a MetaEngine backend
// whose attrs store declined for this path). No POSIX filesystem records a
// distinct creation time, so this is the only source of a real one.
func (v *Volume) createTime(path string, info stdfs.FileInfo) time.Time {
	if attr, ok := v.meta().Attrs(path); ok && !attr.CreateTime.IsZero() {
		return attr.CreateTime
	}
	return info.ModTime()
}

// ShortName returns the volume's 8.3-style short name for a path's final
// element, derived through the share's NameEngine. The engine returns a store
// path; the caller wants just the leaf for the wire, so the parent is trimmed.
func (v *Volume) ShortName(path string) string {
	if path == "" {
		// The volume root's short name is the configured volume name (matching
		// MediumName and main's catalogNameForPath), not an empty leaf.
		return v.Name()
	}
	n, err := v.FS().ShortName(path)
	if err != nil || n == "" {
		_, base := splitStore(path)
		return base
	}
	_, base := splitStore(n)
	return base
}

// MediumName returns the volume's classic-AFP "long" name for a path's final
// element: the 31-character medium name derived through the share's NameEngine,
// case-insensitive for lookup but stored in its original case (Windows-FS
// semantics). The AFP wire long name is capped at 31 bytes, so an over-long host
// name is mapped deterministically (with a "-N" collision suffix) rather than
// truncated raw — and reverses to the same host name across requests. The engine
// returns a store path; the leaf is taken for the wire.
func (v *Volume) MediumName(path string) string {
	if path == "" {
		// The volume root has no host name element of its own; AFP clients must
		// see the configured volume name for the root catalog entry (it drives the
		// mounted volume's window title). Matches main's catalogNameForPath, which
		// substitutes the volume name when the path is the volume root.
		return v.Name()
	}
	n, err := v.FS().MediumName(path)
	if err != nil || n == "" {
		_, base := splitStore(path)
		return base
	}
	_, base := splitStore(n)
	return base
}

// renamePath moves a path inside the volume and rebinds the CNID subtree so node
// ids survive the move. The FS carries the metadata container with the data fork
// (core/fs §9), so the only step AFP adds is the CNID rebind.
func (v *Volume) renamePath(old, new string) error {
	if err := v.FS().Rename(old, new); err != nil {
		return err
	}
	return v.meta().RebindCNID(old, new)
}

// removePath deletes a path inside the volume (data + metadata, via the FS) and
// its CNID subtree.
func (v *Volume) removePath(path string) error {
	if err := v.FS().Remove(path); err != nil {
		return err
	}
	return v.meta().RemoveCNID(path)
}

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
