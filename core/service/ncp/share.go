package ncp

import (
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/share"
)

// Volume is one NetWare volume re-expressed over the §9 storage seam. It HOLDS a
// shared share.Share (the bound fs.ForkFS + the config that built it) and adds
// only the NCP-specific concern: converting NetWare wire paths (VOL:dir/dir\file,
// length-prefixed, backslash- or forward-slash separated) to the seam's
// '/'-separated store paths. It holds NO storage-layout knowledge — it never
// imports path/filepath, never branches on runtime.GOOS, and reaches the
// filesystem only through sh.FS().
//
// A same-fs_type AFP volume / SMB share / NCP volume on one host path see the
// same forks and FinderInfo through the same ForkEngine — the basis for the
// cross-protocol coordination (§10d).
type Volume struct {
	sh *share.Share
}

// VolumeSpec names an NCP volume and the seam components to build it from.
type VolumeSpec struct {
	Name  string
	Share fs.ShareSpec
}

// NewVolume builds one Volume from a spec with no FS-mutation bus (the bus-less
// path used by tests and the zero-config default). A volume built this way is
// isolated. Production builds go through NewVolumeWithBus.
func NewVolume(spec VolumeSpec) (*Volume, error) {
	return NewVolumeWithBus(spec, nil)
}

// NewVolumeWithBus builds one Volume, assembling the share stack through
// share.Build over the supplied FS-mutation bus (§10d): when an NCP volume and an
// AFP volume / SMB share back the same host path, the service hands them the SAME
// bus so a mutation by one reaches the other. A nil bus means "isolated".
func NewVolumeWithBus(spec VolumeSpec, b bus.Bus) (*Volume, error) {
	spec.Share.Name = spec.Name
	// Stamp this service's origin onto the FS mutations this volume produces, so a
	// same-bus AFP/SMB reactor acts on them and NCP's own reactor skips them
	// (§10d). OriginBus is a no-op when b is nil.
	built, err := share.Build(spec.Share, fs.OriginBus(b, OriginNCP))
	if err != nil {
		return nil, err
	}
	return &Volume{sh: built}, nil
}

// newFromShare wraps an already-built shared Share (used by the service Manager
// when it has assembled the share itself).
func newFromShare(s *share.Share) *Volume { return &Volume{sh: s} }

// Name returns the volume's name.
func (v *Volume) Name() string { return v.sh.Name() }

// allows reports whether the session identity may see/bind this volume, per the
// volume's access allow-list. An empty (guest) identity is admitted only by a
// guest-open volume.
func (v *Volume) allows(user string) bool { return v.sh.Permissions().Allows(user) }

// FS returns the bound filesystem; the dispatch reaches files through it.
func (v *Volume) FS() fs.ForkFS { return v.sh.FS() }

// Close releases the bound filesystem's GC-invisible resources (fs.FSCloser); a no-op
// for a backend that owns none. Called at service Stop.
func (v *Volume) Close() error { return v.sh.Close() }

// codec is the volume's FilenameCodec.
func (v *Volume) codec() fs.FilenameCodec { return v.sh.Codec() }

// ShortName returns the 8.3 DOS short name for a store path (the DOS name space
// field), via the share's NameEngine — the same call AFP/SMB use.
func (v *Volume) ShortName(store string) string {
	n, err := v.sh.FS().ShortName(store)
	if err != nil {
		return baseName(store)
	}
	return n
}

// MediumName returns the 31-char "medium" name for a store path (the classic-AFP /
// Macintosh name-space limit), via the share's NameEngine.
func (v *Volume) MediumName(store string) string {
	n, err := v.sh.FS().MediumName(store)
	if err != nil {
		return baseName(store)
	}
	return n
}

// LongName returns the store-native leaf name (the OS/2 long name) — the name as
// stored, no derivation.
func (v *Volume) LongName(store string) string { return baseName(store) }

// baseName is the last '/'-separated element of a store path.
func baseName(store string) string {
	if i := strings.LastIndexByte(store, '/'); i >= 0 {
		return store[i+1:]
	}
	return store
}

// wireNameFor renders a store path's leaf into the bytes a client expects for the
// given name space, threading the right derivation engine and charset:
//
//	NameDOS → 8.3 short name, upper-cased (MacRoman bytes, ASCII-safe)
//	NameMAC → 31-char medium name, MacRoman charset
//	NameOS2 → store-native long name, OEM/ANSI charset
//	NameNFS → store-native long name, UTF-8 (case-sensitive)
//
// An element the target charset cannot represent falls back to the raw UTF-8 bytes
// rather than failing the whole reply.
func (v *Volume) wireNameFor(store string, ns uint8) []byte {
	var name string
	var wire fs.WireEncoding
	switch ns {
	case nsMAC:
		name, wire = v.MediumName(store), fs.WireMacRoman
	case nsOS2:
		name, wire = v.LongName(store), fs.WireANSI
	case nsNFS:
		name, wire = v.LongName(store), fs.WireUTF8
	default: // nsDOS
		name, wire = strings.ToUpper(v.ShortName(store)), fs.WireMacRoman
	}
	b, err := v.codec().Encode(fs.StoredName(name), wire)
	if err != nil {
		return []byte(name)
	}
	return b
}

// decodeWireName converts a client-sent name in name space ns to the store-native
// name, threading the right charset (the inverse of wireNameFor). NameDOS/OS2 use
// OEM/ANSI-ish charsets; NameMAC uses MacRoman; NameNFS uses UTF-8.
func (v *Volume) decodeWireName(wire []byte, ns uint8) (string, error) {
	var enc fs.WireEncoding
	switch ns {
	case nsMAC:
		enc = fs.WireMacRoman
	case nsNFS:
		enc = fs.WireUTF8
	default: // nsDOS, nsOS2
		enc = fs.WireANSI
	}
	stored, err := v.codec().Decode(wire, enc)
	if err != nil {
		return "", err
	}
	return string(stored), nil
}

// ResolvePath converts an NCP wire path to a store path. NetWare paths are
// uppercase 8.3 names separated by backslash or forward slash; an optional
// "VOL:" volume-name prefix (already resolved to this Volume by the caller) is
// stripped. Each element is decoded from the wire (DOS OEM / ANSI — NetWare 3.x is
// not Unicode) through the volume codec to the store-native name. "." and ".."
// are folded. An element the store charset cannot represent yields
// fs.ErrUnrepresentable rather than a mangled path.
func (v *Volume) ResolvePath(wirePath string) (string, error) {
	// Strip a leading "VOL:" prefix if present (the volume is already resolved).
	if i := strings.IndexByte(wirePath, ':'); i >= 0 {
		wirePath = wirePath[i+1:]
	}
	var elems []string
	for _, raw := range strings.FieldsFunc(wirePath, func(r rune) bool {
		return r == '\\' || r == '/'
	}) {
		if raw == "" || raw == "." {
			continue
		}
		if raw == ".." {
			if len(elems) > 0 {
				elems = elems[:len(elems)-1]
			}
			continue
		}
		stored, err := v.codec().Decode([]byte(raw), fs.WireANSI)
		if err != nil {
			return "", err
		}
		el := string(stored)
		if el == "" {
			continue
		}
		elems = append(elems, el)
	}
	return strings.Join(elems, "/"), nil
}
