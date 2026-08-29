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

// Close releases the bound filesystem's GC-invisible resources (fs.FSCloser); a
// no-op for a backend that owns none. Called at service Stop, not on RemoveShare.
func (sh *Share) Close() error { return sh.sh.Close() }

// meta returns the share's MetaEngine (the per-share names/CNID/DOS-attribute
// facade assembled by BuildShare). It is mandatory — every ForkFS carries one —
// so this never returns nil.
func (sh *Share) meta() fs.MetaEngine { return sh.sh.FS().Meta() }

// AttrsFor renders the DOS attribute word SMB reports for a store path: the
// host-derived defaults (dosAttrs) OR-ed with any persisted RO/HID/SYS/ARCH bits
// from the share's MetaEngine, so a Hidden/System bit a client set survives even
// though the POSIX host cannot represent it. A directory keeps its structural
// bit; the store contributes only the storable bits.
func (sh *Share) AttrsFor(store string, info stdfs.FileInfo) uint16 {
	a := dosAttrs(info)
	if stored, ok := sh.meta().Attrs(store); ok {
		a |= stored.Attrs & uint16(fs.DOSStorableMask)
	}
	return a
}

// SetAttrs persists the storable DOS attribute bits for a store path through the
// share's MetaEngine. The structural bits are masked out.
func (sh *Share) SetAttrs(store string, attrs uint16) error {
	m := sh.meta()
	cur, _ := m.Attrs(store)
	cur.Attrs = attrs & uint16(fs.DOSStorableMask)
	return m.SetAttrs(store, cur)
}

// EAs returns the OS/2-style named extended attributes stored for a store
// path (empty when none are stored), through the share's MetaEngine.
func (sh *Share) EAs(store string) []fs.EA {
	eas, _ := sh.meta().EAs(store)
	return eas
}

// SetEAs applies eas as an upsert against the store path's existing EA list —
// [MS-CIFS] §2.2.8.4.2 describes SMB_INFO_SET_EAS as setting "a specific
// list" (i.e. these entries), not replacing the whole set, matching the OS/2
// DosSetPathInfo/DosSetFileInfo EA API convention this command mirrors. Named
// entries in eas overwrite any existing value; every other stored EA is left
// untouched. An entry with a zero-length Value DELETES that name (the same
// OS/2 API convention), rather than storing an empty value — OS/2 Workplace
// Shell issues one SET_PATH_INFO per changed EA (netbeui.pcap 2026-07-14:
// separate .SUBJECT/.ICON/.COMMENTS/.KEYPHRASES requests on the same file),
// so a naive full-replace here would silently discard every EA set by an
// earlier request.
func (sh *Share) SetEAs(store string, eas []fs.EA) error {
	m := sh.meta()
	cur, _ := m.EAs(store)
	merged := make([]fs.EA, 0, len(cur)+len(eas))
	merged = append(merged, cur...)
	for _, e := range eas {
		idx := -1
		for i, c := range merged {
			if c.Name == e.Name {
				idx = i
				break
			}
		}
		switch {
		case len(e.Value) == 0:
			if idx >= 0 {
				merged = append(merged[:idx], merged[idx+1:]...)
			}
		case idx >= 0:
			merged[idx] = e
		default:
			merged = append(merged, e)
		}
	}
	return m.SetEAs(store, merged)
}

// longNameEA is the OS/2 HPFS-convention EA name a FAT-mounted volume uses to
// carry a file's true long name alongside its 8.3 host name. Set via
// TRANS2_SET_PATH/FILE_INFORMATION SMB_INFO_SET_EAS (netbeui.pcap frame 666).
const longNameEA = ".LONGNAME"

// eatASCIIMarker is FEA2's typed single-value encoding for a plain-text EA
// value ([OS/2 EA API] EAT_ASCII = 0xFFFD, little-endian on the wire): 2-byte
// type, 2-byte length, then the text itself. OS/2 Workplace Shell always
// writes .LONGNAME this way (netbeui.pcap frame 666: `fd ff 17 00 "This is a
// new title.exe"`).
const eatASCIIMarker = 0xFFFD

// longNameText decodes an EA value into its display text: the FEA2 typed
// EAT_ASCII envelope when present, else the bytes taken as raw text (a bare
// value, as a non-WPS client or test fixture might set).
func longNameText(value []byte) string {
	if len(value) >= 4 {
		typ := uint16(value[0]) | uint16(value[1])<<8
		length := int(uint16(value[2]) | uint16(value[3])<<8)
		if typ == eatASCIIMarker && 4+length <= len(value) {
			return string(value[4 : 4+length])
		}
	}
	return string(value)
}

// longNameFor returns the display name stored in store's .LONGNAME EA, or ""
// when none is set.
func (sh *Share) longNameFor(store string) string {
	for _, e := range sh.EAs(store) {
		if e.Name == longNameEA {
			return longNameText(e.Value)
		}
	}
	return ""
}

// foldLongName scans dir for a child whose stored .LONGNAME EA matches want
// case-insensitively, returning that child's actual host name. This lets an
// OS/2 client open/list a file by the long name it set via .LONGNAME even
// though the host entry itself is an 8.3 name.
func (sh *Share) foldLongName(dir, want string) (string, bool) {
	entries, err := sh.FS().ReadDir(dir)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		full := e.Name()
		if dir != "" {
			full = dir + "/" + full
		}
		if long := sh.longNameFor(full); long != "" && strings.EqualFold(long, want) {
			return e.Name(), true
		}
	}
	return "", false
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
//
// The decoded path is then case-folded to the on-disk casing (fs.ResolveFold)
// when it differs — SMB filenames are caseless by convention regardless of
// whether the host filesystem is (netbeui.pcap 2026-07-13 frames 2783/2802:
// OS/2 WPS SET_PATH_INFO creates "foo.lnk", a later QUERY_PATH_INFO asks for
// "foo.LNK"). Without folding, every MetaEngine-backed lookup keyed on the
// exact store path — EAs, DOS attributes, CNID — silently misses on a
// differently-cased request even though Stat/ReadDir degrade gracefully. A
// path that does not yet exist (a create/rename target) is returned as
// typed — ResolveFold's miss case preserves the requested casing.
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
	path := strings.Join(elems, "/")
	if resolved, ok := fs.ResolveFold(sh.FS(), path); ok {
		path = resolved
	} else if resolved, ok := sh.resolveLongNames(elems); ok {
		path = resolved
	}
	return path, nil
}

// resolveLongNames is ResolveFold's OS/2 .LONGNAME-aware fallback: when a plain
// case-fold does not fully resolve a path, retry component by component,
// accepting either a case-insensitive host-name match or a match against a
// child's stored .LONGNAME EA (the true long name an OS/2 client set over an
// 8.3 host name — netbeui.pcap frame 666 sets .LONGNAME; frames 812/813 then
// open the file by that long name). ok is false as soon as a component
// resolves neither way, mirroring ResolveFold's miss contract.
func (sh *Share) resolveLongNames(elems []string) (string, bool) {
	resolved := make([]string, 0, len(elems))
	dir := ""
	for _, want := range elems {
		if actual, ok := foldComponentEA(sh, dir, want); ok {
			resolved = append(resolved, actual)
			dir = strings.Join(resolved, "/")
			continue
		}
		return "", false
	}
	return strings.Join(resolved, "/"), true
}

// foldComponentEA resolves one path component against dir: an exact or
// case-folded host-name match first, else a .LONGNAME EA match.
func foldComponentEA(sh *Share, dir, want string) (string, bool) {
	full := want
	if dir != "" {
		full = dir + "/" + want
	}
	if _, err := sh.FS().Stat(full); err == nil {
		return want, true
	}
	entries, err := sh.FS().ReadDir(dir)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if strings.EqualFold(e.Name(), want) {
			return e.Name(), true
		}
	}
	if actual, ok := sh.foldLongName(dir, want); ok {
		return actual, true
	}
	return "", false
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
