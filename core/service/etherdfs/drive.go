package etherdfs

import (
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/etherdfs"
	"github.com/ObsoleteMadness/ClassicStack/core/share"
)

// Drive is one EtherDFS export re-expressed over the §9 storage seam. It HOLDS a
// shared share.Share (the bound fs.ForkFS + the config that built it) and adds
// only the EtherDFS-specific concern: converting DOS wire paths (backslashes, a
// leading drive letter) to the seam's '/'-separated store paths. It holds NO
// storage-layout knowledge — it never imports path/filepath, never branches on
// runtime.GOOS, and reaches the filesystem only through sh.FS().
//
// A same-host-path AFP volume / SMB share and EtherDFS drive see the same forks
// through the same ForkEngine — the basis for §10d coordination. Catalog
// operations (Stat/OpenFile/Rename/Remove/ReadDir…) are FS operations: the
// dispatch calls sh.FS().X, and the FS carries fork metadata on Rename/Remove,
// so EtherDFS never pairs those calls itself.
type Drive struct {
	sh *share.Share
}

// DriveSpec names an EtherDFS drive and the seam components to build it from.
// Unlike SMB there is no Description remark — EtherDFS carries no share comment
// on the wire.
type DriveSpec struct {
	Name  string
	Share fs.ShareSpec
}

// NewDriveWithBus builds one Drive, assembling the share stack through share.Build
// over the supplied FS-mutation bus (§10d): when an EtherDFS drive and an AFP
// volume / SMB share back the same host path, the service hands them the SAME bus
// so a mutation by one reaches the other. A nil bus means "isolated".
func NewDriveWithBus(spec DriveSpec, b bus.Bus) (*Drive, error) {
	spec.Share.Name = spec.Name
	built, err := share.Build(spec.Share, fs.OriginBus(b, OriginEtherDFS))
	if err != nil {
		return nil, err
	}
	return &Drive{sh: built}, nil
}

// Name returns the drive's name (its DOS drive letter).
func (d *Drive) Name() string { return d.sh.Name() }

// FS returns the bound filesystem; the dispatch reaches files through it
// (d.FS().Stat(p), d.FS().OpenFile(p, flag), d.FS().Rename/Remove which carry
// fork metadata).
func (d *Drive) FS() fs.ForkFS { return d.sh.FS() }

// dosAttrs returns the drive's DOS-attribute store (the per-share backend
// assembled by BuildShare), or nil when the share exposes none. EtherDFS persists
// and serves the FAT RO/HID/SYS/ARCH bits through it, so attributes survive across
// the host filesystem (which cannot represent them) per the configured backend.
func (d *Drive) dosAttrs() fs.DOSAttrStore {
	if da, ok := d.sh.FS().(fs.DOSAttred); ok {
		return da.DOSAttrs()
	}
	return nil
}

// names returns the drive's NameEngine, for reversing a wire 8.3 short name a DOS
// client sent back to the stored host (long) name. nil when the share exposes none.
func (d *Drive) names() fs.NameEngine {
	if n, ok := d.sh.FS().(fs.Named); ok {
		return n.Names()
	}
	return nil
}

// ReadOnly reports whether the drive rejects writes.
func (d *Drive) ReadOnly() bool { return d.sh.ReadOnly() }

// resolvePath converts an EtherDFS wire path to a store path: backslashes become
// forward slashes and a leading drive letter is stripped (NormalizePath), then
// each element is cleaned of "."/".." so a client cannot escape the drive root.
// Each surviving element is mapped from the 8.3 short name the DOS client sent
// back to the real host (long) name via the share's NameEngine — so a client that
// listed "REPORT~1.TXT" and now opens it reaches the host file "ReportFinal.txt".
// An element that is already a real host name (no reverse binding) passes through
// unchanged.
func (d *Drive) resolvePath(wirePath string) string {
	norm := etherdfs.NormalizePath(wirePath)
	if norm == "" {
		return ""
	}
	ne := d.names()
	var elems []string
	dir := ""
	for el := range strings.SplitSeq(norm, "/") {
		switch el {
		case "", ".":
			continue
		case "..":
			if len(elems) > 0 {
				elems = elems[:len(elems)-1]
				dir = strings.Join(elems, "/")
			}
			continue
		}
		// Reverse a derived 8.3 name to its stored host name within this directory.
		// ToLong returns the input unchanged when there is no binding (a real host
		// name the client typed directly), so this is safe for both.
		resolved := el
		if ne != nil {
			if long, ok := ne.ToLong(dir, el, fs.ShortName); ok {
				resolved = long
			}
		}
		elems = append(elems, resolved)
		dir = strings.Join(elems, "/")
	}
	return strings.Join(elems, "/")
}
