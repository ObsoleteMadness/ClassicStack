package smb

import (
	stdfs "io/fs"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// Share is one SMB tree connect re-expressed over the §9 storage seam. Like the
// AFP Volume, it binds a fs.ForkFS and the share's FilenameCodec and holds NO
// storage-layout knowledge: it never imports path/filepath, never branches on
// runtime.GOOS, and never knows which container (AppleDouble sidecar, NTFS ADS,
// Netatalk EA) backs a resource fork. SMB wire paths are backslash-separated;
// the share converts them to the seam's '/'-separated store paths and threads the
// per-request wire charset (UTF-16 vs ANSI) into the codec.
//
// A same-FS AFP volume and SMB share built over one fs_type therefore see the
// same forks and FinderInfo through the same ForkEngine — the basis for the
// AFP+SMB coordination the M7 "Done when" calls for.
type Share struct {
	name  string
	fsys  fs.ForkFS
	codec fs.FilenameCodec
}

// ShareSpec names an SMB share and the seam components to build it from.
type ShareSpec struct {
	Name  string
	Share fs.ShareSpec
}

// NewShare builds one Share from a spec by assembling the share stack through
// fs.BuildShare, which validates the fs_type×fork_backend×filename_codec triple.
func NewShare(spec ShareSpec) (*Share, error) {
	built, err := fs.BuildShare(spec.Share, nil)
	if err != nil {
		return nil, err
	}
	return &Share{name: spec.Name, fsys: built, codec: codecOf(built)}, nil
}

// Name returns the share's tree name.
func (sh *Share) Name() string { return sh.name }

// codecOf reaches the FilenameCodec a built share carries, falling back to the
// identity (POSIX-bytes) codec — which advertises every wire charset — if the
// share doesn't expose one.
func codecOf(built fs.ForkFS) fs.FilenameCodec {
	if c, ok := built.(fs.Coded); ok {
		if codec := c.Codec(); codec != nil {
			return codec
		}
	}
	return fs.NewIdentityFilenameCodec()
}

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
		stored, err := sh.codec.Decode(raw, wire)
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
	return sh.codec.Encode(fs.StoredName(stored), wireFor(flags2))
}

// --- catalog operations, all over the seam ---

// List returns the children of a directory as store-native dir entries. Name
// encoding back to the wire charset is the caller's concern (via EncodeName).
func (sh *Share) List(path string) ([]stdfs.DirEntry, error) { return sh.fsys.ReadDir(path) }

// Stat returns store-native metadata for a path.
func (sh *Share) Stat(path string) (stdfs.FileInfo, error) { return sh.fsys.Stat(path) }

// OpenData opens the data fork (the file itself) through the seam.
func (sh *Share) OpenData(path string, flag int) (fs.File, error) {
	return sh.fsys.OpenFork(path, fs.DataFork, flag)
}

// OpenResource opens the AFP_Resource stream / resource fork through the share's
// fork engine — the same engine an AFP volume on this fs_type uses, so a fork
// written by one protocol is visible to the other.
func (sh *Share) OpenResource(path string, flag int) (fs.File, error) {
	return sh.fsys.OpenFork(path, fs.ResourceFork, flag)
}

// FinderInfo reads the 32-byte FinderInfo (the AFP_AfpInfo payload on ads shares)
// through the fork engine.
func (sh *Share) FinderInfo(path string) (info [32]byte, ok bool, err error) {
	return sh.fsys.ReadFinderInfo(path)
}

// Rename moves a path, carrying its metadata container.
func (sh *Share) Rename(old, new string) error {
	if err := sh.fsys.Rename(old, new); err != nil {
		return err
	}
	return sh.fsys.MoveMetadata(old, new)
}

// Remove deletes a path and its metadata container.
func (sh *Share) Remove(path string) error {
	if err := sh.fsys.DeleteMetadata(path); err != nil {
		return err
	}
	return sh.fsys.Remove(path)
}

// Capabilities reports the optional behaviours the share's FileSystem supports.
func (sh *Share) Capabilities() fs.Capabilities { return sh.fsys.Capabilities() }
