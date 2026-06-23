package smb

import (
	stdfs "io/fs"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/share"
)

// Share is one SMB tree connect re-expressed over the §9 storage seam. It HOLDS a
// shared share.Share (the bound fs.ForkFS + the config that built it) and adds
// only the SMB-specific concern: converting backslash/UTF-16 wire paths to the
// seam's '/'-separated store paths and threading the per-request wire charset
// (UTF-16 vs ANSI) into the share's codec. It holds NO storage-layout knowledge —
// it never imports path/filepath, never branches on runtime.GOOS, and reaches the
// filesystem only through sh.FS().
//
// A same-fs_type AFP volume and SMB share see the same forks and FinderInfo
// through the same ForkEngine — the basis for the AFP+SMB coordination M7 calls
// for. Catalog operations (Stat/OpenFork/Rename/Remove…) are FS operations: the
// dispatch calls sh.FS().X, and the FS carries fork metadata on Rename/Remove, so
// SMB never pairs those calls itself.
type Share struct {
	sh *share.Share
}

// ShareSpec names an SMB share and the seam components to build it from. Description
// is the operator remark NetShareEnum reports (the share comment); it is SMB-specific
// (not carried on fs.ShareSpec) and applied to the built share via SetDescription.
type ShareSpec struct {
	Name        string
	Description string
	Share       fs.ShareSpec
}

// NewShare builds one Share from a spec with no FS-mutation bus (the bus-less path
// used by tests and the zero-config default). A share built this way is isolated —
// its FS publishes to a private bus no one else holds, so it cannot coordinate with
// a same-host-path AFP volume (§10d). Production builds go through NewShareWithBus.
func NewShare(spec ShareSpec) (*Share, error) {
	return NewShareWithBus(spec, nil)
}

// NewShareWithBus builds one Share, assembling the share stack through share.Build
// over the supplied FS-mutation bus (§10d): when an SMB share and an AFP volume back
// the same host path, the service hands them the SAME bus so a mutation by one
// reaches the other. A nil bus means "isolated" (share.Build then makes a private
// one).
func NewShareWithBus(spec ShareSpec, b bus.Bus) (*Share, error) {
	spec.Share.Name = spec.Name
	// Stamp this service's origin onto the FS mutations this share produces, so a
	// same-bus AFP volume's reactor acts on them and SMB's own reactor skips them
	// (§10d). OriginBus is a no-op when b is nil.
	built, err := share.Build(spec.Share, fs.OriginBus(b, OriginSMB))
	if err != nil {
		return nil, err
	}
	if spec.Description != "" {
		built.SetDescription(spec.Description)
	}
	return &Share{sh: built}, nil
}

// newFromShare wraps an already-built shared Share (used by the service Manager
// when it has assembled the share itself).
func newFromShare(s *share.Share) *Share { return &Share{sh: s} }

// Name returns the share's tree name.
func (sh *Share) Name() string { return sh.sh.Name() }

// Description returns the operator-supplied human description (the NetShareEnum
// remark / share comment), or empty.
func (sh *Share) Description() string { return sh.sh.Description() }

// allows reports whether the session identity may see/bind this share, per the
// share's access allow-list. An empty (guest) identity is admitted only by a
// guest-open share.
func (sh *Share) allows(user string) bool { return sh.sh.Permissions().Allows(user) }

// FS returns the bound filesystem; the dispatch reaches files through it
// (sh.FS().Stat(p), sh.FS().OpenFork(p, fork, flag), sh.FS().Rename/Remove which
// carry fork metadata).
func (sh *Share) FS() fs.ForkFS { return sh.sh.FS() }

// dosAttrs returns the share's DOS-attribute store (the per-share backend
// assembled by BuildShare), or nil when the share exposes none. SMB persists the
// RO/HID/SYS/ARCH bits it cannot derive from the host through it.
func (sh *Share) dosAttrs() fs.DOSAttrStore {
	if da, ok := sh.sh.FS().(fs.DOSAttred); ok {
		return da.DOSAttrs()
	}
	return nil
}

// AttrsFor renders the DOS attribute word SMB reports for a store path: the
// host-derived defaults (dosAttrs) OR-ed with any persisted RO/HID/SYS/ARCH bits
// from the share's DOS-attribute store, so a Hidden/System bit a client set
// survives even though the POSIX host cannot represent it. A directory keeps its
// structural bit; the store contributes only the storable bits.
func (sh *Share) AttrsFor(store string, info stdfs.FileInfo) uint16 {
	a := dosAttrs(info)
	if da := sh.dosAttrs(); da != nil {
		if stored, ok := da.Get(store); ok {
			a |= stored.Attrs & uint16(fs.DOSStorableMask)
		}
	}
	return a
}

// SetAttrs persists the storable DOS attribute bits for a store path through the
// share's DOS-attribute store. A share with no store (synthetic backend) silently
// drops them. The structural bits are masked out.
func (sh *Share) SetAttrs(store string, attrs uint16) error {
	da := sh.dosAttrs()
	if da == nil {
		return nil
	}
	cur, _ := da.Get(store)
	cur.Attrs = attrs & uint16(fs.DOSStorableMask)
	return da.Set(store, cur)
}

// codec is the share's FilenameCodec, threaded with the per-request wire charset.
func (sh *Share) codec() fs.FilenameCodec { return sh.sh.Codec() }

// ResolvePath converts an SMB wire path to a store path, decoding each element
// from the request's wire charset — selected by the FLAGS2 Unicode bit via
// wireFor — to the store-native name. wirePath is the raw wire bytes: UTF-16LE
// when the Unicode flag is set, the negotiated OEM page (ANSI) otherwise. The
// path separator (backslash) is split in the wire charset's own encoding — a
// 2-byte 5C 00 unit under UTF-16, a single 5C byte under ANSI — so a UTF-16 name
// is never mis-split on a low byte. An element the store charset cannot represent
// yields fs.ErrUnrepresentable (→ STATUS_OBJECT_NAME_INVALID) rather than a
// mangled path; an unsupported wire charset yields fs.ErrWireUnsupported.
func (sh *Share) ResolvePath(wirePath []byte, flags2 uint16) (string, error) {
	wire := wireFor(flags2)

	var elems []string
	for _, raw := range splitWirePath(wirePath, wire) {
		if len(raw) == 0 {
			continue
		}
		stored, err := sh.codec().Decode(raw, wire)
		if err != nil {
			return "", err
		}
		el := string(stored)
		if el == "" || el == "." {
			continue
		}
		if el == ".." {
			if len(elems) > 0 {
				elems = elems[:len(elems)-1]
			}
			continue
		}
		elems = append(elems, el)
	}
	return strings.Join(elems, "/"), nil
}

// splitWirePath splits raw SMB path bytes on the backslash separator as encoded
// in the wire charset: a 2-byte little-endian unit (5C 00) for UTF-16LE, a single
// 5C byte otherwise. Forward slashes are also accepted as separators (DOS clients
// send either). Empty segments are preserved for the caller to skip.
func splitWirePath(raw []byte, wire fs.WireEncoding) [][]byte {
	if wire == fs.WireUTF16 {
		return splitUTF16Path(raw)
	}
	out := [][]byte{}
	start := 0
	for i := range raw {
		if raw[i] == '\\' || raw[i] == '/' {
			out = append(out, raw[start:i])
			start = i + 1
		}
	}
	return append(out, raw[start:])
}

// splitUTF16Path splits UTF-16LE bytes on the backslash (5C 00) or slash (2F 00)
// code unit, keeping the 2-byte framing intact for the surviving elements.
func splitUTF16Path(raw []byte) [][]byte {
	out := [][]byte{}
	start := 0
	for i := 0; i+1 < len(raw); i += 2 {
		hi := raw[i+1]
		lo := raw[i]
		if hi == 0x00 && (lo == '\\' || lo == '/') {
			out = append(out, raw[start:i])
			start = i + 2
		}
	}
	return append(out, raw[start:])
}

// EncodeName renders a store-native name back to the request's wire charset for
// packing into a directory-listing or find reply.
func (sh *Share) EncodeName(stored string, flags2 uint16) ([]byte, error) {
	return sh.codec().Encode(fs.StoredName(stored), wireFor(flags2))
}

// Catalog operations are FS operations: the dispatch reaches them through
// sh.FS() — e.g. sh.FS().ReadDir(p), sh.FS().Stat(p),
// sh.FS().OpenFork(p, fs.DataFork|fs.ResourceFork, flag),
// sh.FS().ReadFinderInfo(p). The FS carries fork metadata on Rename/Remove
// (core/fs §9), so SMB never pairs MoveMetadata/DeleteMetadata by hand.
