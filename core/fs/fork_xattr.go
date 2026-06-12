package fs

import (
	"errors"
	"io"
	stdfs "io/fs"
	"os"

	"github.com/ObsoleteMadness/ClassicStack/core/appledouble"
	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// xattrForkEngine stores resource forks and Finder metadata in the Netatalk
// extended-attribute ("ea = sys") layout, so a ClassicStack share over an
// existing Netatalk 3.x/4.x volume sees the same forks (spec/16 §1c):
//
//   - the Finder metadata lives in the "user.org.netatalk.Metadata" EA — a
//     fixed 402-byte AppleDouble v2 header (no payloads inline) whose ad_entry
//     table records FinderInfo, an optional comment, and the resource-fork
//     length; and
//   - the resource-fork bytes live in the "user.org.netatalk.ResourceFork" EA.
//
// EAs are addressed through the base FileSystem using a "path\x00ea\x00<name>"
// key: on a host FileSystem that maps that key to a real extended attribute the
// container is a true xattr; on any other FileSystem (e.g. the in-mem test FS)
// it degrades to an ordinary path key, so the engine's record handling stays
// testable without an xattr-capable host. The FinderInfo bytes are identical to
// the AppleDouble FinderInfo entry — only the container and the
// resource-fork-out-of-line split differ.
//
// Netatalk Metadata EA layout (libatalk/adouble/ad_open.c, AD_VERSION2):
//
//	magic      uint32 = 0x00051607        (AppleDouble v2 magic)
//	version    uint32 = 0x00020000
//	filler     [16]byte = "Netatalk        " (16 bytes, space-padded)
//	numEntries uint16
//	entries[numEntries]{ id uint32; offset uint32; length uint32 }
//	... entry payloads, all within the fixed 402-byte (AD_DATASZ_EA) blob ...
//
// The resource-fork ad_entry (id 2) records the fork length but its bytes are
// NOT in the blob (Netatalk stores them in the separate ResourceFork EA); the
// blob is a pure metadata header. ClassicStack reuses the core/appledouble codec
// for the header so FinderInfo/comment round-trip byte-for-byte with Netatalk.
type xattrForkEngine struct {
	fs FileSystem
}

func newXattrForkEngine(base FileSystem) *xattrForkEngine {
	return &xattrForkEngine{fs: base}
}

// Netatalk EA names for the metadata header and the resource fork.
const (
	xattrMetadataEA = "org.netatalk.Metadata"
	xattrResourceEA = "org.netatalk.ResourceFork"

	// AD_DATASZ_EA: Netatalk pads the Metadata EA to a fixed 402 bytes so the
	// ad_entry offsets are stable regardless of how many entries are present.
	xattrMetadataSize = 402

	// "Netatalk        " — the 16-byte filler Netatalk writes after the version,
	// preserved on round-trip so the EA is byte-identical to a Netatalk write.
	xattrFiller = "Netatalk        "
)

// eaPath returns the base-FileSystem key for an extended attribute of path. The
// NUL-delimited form cannot collide with an ordinary path element (which never
// contains a NUL) nor with the ads engine's "path:stream" keys.
func eaPath(path, name string) string { return path + "\x00ea\x00" + name }

// metadataEAPath / resourceEAPath name the two Netatalk EAs for a file path.
func metadataEAPath(path string) string { return eaPath(path, xattrMetadataEA) }
func resourceEAPath(path string) string { return eaPath(path, xattrResourceEA) }

// errBadMetadataEA marks a malformed or wrong-magic Metadata EA; like the ads
// engine treats a garbage AfpInfo stream, the xattr engine treats it as "no
// metadata present" rather than surfacing a decode error to a client.
var errBadMetadataEA = errors.New("fs: malformed org.netatalk.Metadata EA")

// --- small whole-EA read/write helpers over the base FileSystem. ---

func (e *xattrForkEngine) readAll(path string) ([]byte, error) {
	f, err := e.fs.OpenFile(path, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	buf := make([]byte, info.Size())
	if len(buf) == 0 {
		return buf, nil
	}
	n, err := f.ReadAt(buf, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buf[:n], nil
}

func (e *xattrForkEngine) writeAll(path string, b []byte) error {
	f, err := e.fs.OpenFile(path, os.O_RDWR|os.O_CREATE)
	if err != nil {
		f, err = e.fs.CreateFile(path)
		if err != nil {
			return err
		}
	}
	defer func() { _ = f.Close() }()
	if err := f.Truncate(0); err != nil {
		return err
	}
	if len(b) == 0 {
		return f.Sync()
	}
	if _, err := f.WriteAt(b, 0); err != nil {
		return err
	}
	return f.Sync()
}

// --- Metadata EA encode/parse (the 402-byte AppleDouble v2 header). ---

// encodeMetadataEA builds the fixed-size Netatalk Metadata EA from p and the
// recorded resource-fork length. The header carries no payload bytes for the
// resource fork (those live in the ResourceFork EA), so the ad_entry length
// records rsrcLen while the blob stays a pure header. The result reuses the
// core/appledouble Build so FinderInfo/comment match a sidecar byte-for-byte,
// then patches in the Netatalk filler and the out-of-line resource length and
// pads to AD_DATASZ_EA.
func encodeMetadataEA(p appledouble.Parsed, rsrcLen uint32) []byte {
	includeComment := p.HasComment && len(p.Comment) > 0
	var commentLen uint32
	if includeComment {
		commentLen = uint32(len(p.Comment))
	}
	hdr := appledouble.Build(p, includeComment, commentLen)

	// Netatalk's filler is "Netatalk        " rather than zero bytes; preserve
	// it so a blob written here is indistinguishable from a Netatalk write.
	copy(hdr[8:24], xattrFiller)

	// The resource-fork ad_entry length is the out-of-line ResourceFork EA size,
	// not bytes carried in the blob. appledouble.Build wrote the entry with the
	// (zero) inline resource length and no payload; rewrite just the length
	// field, leaving the offset pointing past the header where Netatalk parks it.
	patchResourceLen(hdr, rsrcLen)

	// Pad to the fixed AD_DATASZ_EA so ad_entry offsets are stable.
	if len(hdr) < xattrMetadataSize {
		padded := make([]byte, xattrMetadataSize)
		copy(padded, hdr)
		return padded
	}
	return hdr[:xattrMetadataSize]
}

// patchResourceLen rewrites the ResourceFork ad_entry's length field in a built
// AppleDouble header without touching the rest of the table. It walks the entry
// table rather than assuming a fixed slot so it survives the optional comment
// entry that shifts the resource entry's position.
func patchResourceLen(hdr []byte, rsrcLen uint32) {
	if len(hdr) < appledouble.HeaderSize {
		return
	}
	numEntries := int(bp.BE16(hdr[24:26]))
	for i := range numEntries {
		off := appledouble.HeaderSize + i*appledouble.EntrySize
		if off+appledouble.EntrySize > len(hdr) {
			return
		}
		if bp.BE32(hdr[off:off+4]) == appledouble.EntryIDResourceFork {
			bp.PutBE32(hdr[off+8:off+12], rsrcLen)
			return
		}
	}
}

// resourceLenFromEntries returns the ResourceFork ad_entry's recorded length by
// walking the entry table, the read-side counterpart to patchResourceLen. It
// returns 0 if there is no resource entry. The bytes themselves are out-of-line,
// so this never reads past the entry table.
func resourceLenFromEntries(b []byte) uint32 {
	if len(b) < appledouble.HeaderSize {
		return 0
	}
	numEntries := int(bp.BE16(b[24:26]))
	for i := range numEntries {
		off := appledouble.HeaderSize + i*appledouble.EntrySize
		if off+appledouble.EntrySize > len(b) {
			return 0
		}
		if bp.BE32(b[off:off+4]) == appledouble.EntryIDResourceFork {
			return bp.BE32(b[off+8 : off+12])
		}
	}
	return 0
}

// parseMetadataEA decodes a Metadata EA. It validates the AppleDouble magic and
// returns the parsed header plus the recorded resource-fork length; the resource
// bytes themselves come from the ResourceFork EA, so Parsed.Resource is ignored.
func parseMetadataEA(b []byte) (appledouble.Parsed, uint32, error) {
	if len(b) < appledouble.HeaderSize {
		return appledouble.Parsed{}, 0, errBadMetadataEA
	}
	if bp.BE32(b[0:4]) != appledouble.Magic {
		return appledouble.Parsed{}, 0, errBadMetadataEA
	}
	p, err := appledouble.Parse(b)
	if err != nil {
		return appledouble.Parsed{}, 0, errBadMetadataEA
	}
	// The resource-fork bytes are out-of-line (in the ResourceFork EA), so the
	// ad_entry's recorded length exceeds the blob — appledouble.Parse's bounds
	// check skips that entry and never sets ResourceLenAt. Read the length
	// straight from the entry table instead.
	rsrcLen := resourceLenFromEntries(b)
	return p, rsrcLen, nil
}

// readMetadataEA reads and decodes the Metadata EA, if present. A missing or
// garbage EA is reported as absent (Netatalk tolerance), not an error.
func (e *xattrForkEngine) readMetadataEA(path string) (appledouble.Parsed, bool, error) {
	b, err := e.readAll(metadataEAPath(path))
	if err != nil {
		if errors.Is(err, stdfs.ErrNotExist) {
			return appledouble.Parsed{}, false, nil
		}
		return appledouble.Parsed{}, false, err
	}
	p, _, err := parseMetadataEA(b)
	if err != nil {
		return appledouble.Parsed{}, false, nil
	}
	return p, true, nil
}

// writeMetadataEA rebuilds and writes the Metadata EA for path, recording the
// current resource-fork length so the ad_entry table stays consistent with the
// out-of-line ResourceFork EA.
func (e *xattrForkEngine) writeMetadataEA(path string, p appledouble.Parsed) error {
	rsrcLen, err := e.resourceLen(path)
	if err != nil {
		return err
	}
	return e.writeAll(metadataEAPath(path), encodeMetadataEA(p, uint32(rsrcLen)))
}

// resourceLen reports the size of the out-of-line ResourceFork EA (0 if absent).
func (e *xattrForkEngine) resourceLen(path string) (int64, error) {
	info, err := e.fs.Stat(resourceEAPath(path))
	if err != nil {
		if errors.Is(err, stdfs.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	return info.Size(), nil
}

// --- ForkEngine ---

func (e *xattrForkEngine) OpenFork(path string, fork ForkType, flag int) (File, error) {
	if fork == DataFork {
		// The data fork is the file itself; defer to the base FileSystem.
		return e.fs.OpenFile(path, flag)
	}
	// The resource fork is the out-of-line ResourceFork EA, backed directly by
	// the base FileSystem so reads/writes stream through. A xattrResourceFork
	// wrapper keeps the Metadata EA's recorded length in sync on Sync/Close.
	eaPath := resourceEAPath(path)
	f, err := e.fs.OpenFile(eaPath, flag)
	if err != nil {
		if errors.Is(err, stdfs.ErrNotExist) && flag&os.O_CREATE != 0 {
			f, err = e.fs.CreateFile(eaPath)
		}
		if err != nil {
			return nil, err
		}
	}
	return &xattrResourceFork{engine: e, path: path, inner: f}, nil
}

func (e *xattrForkEngine) ForkLen(path string, fork ForkType) (int64, error) {
	if fork == DataFork {
		info, err := e.fs.Stat(path)
		if err != nil {
			return 0, err
		}
		return info.Size(), nil
	}
	return e.resourceLen(path)
}

func (e *xattrForkEngine) ReadFinderInfo(path string) (info [32]byte, ok bool, err error) {
	p, present, err := e.readMetadataEA(path)
	if err != nil || !present || !p.HasFinder {
		return [32]byte{}, false, err
	}
	return p.FinderInfo, true, nil
}

func (e *xattrForkEngine) WriteFinderInfo(path string, info [32]byte) error {
	p, _, err := e.readMetadataEA(path)
	if err != nil {
		return err
	}
	p.FinderInfo = info
	p.HasFinder = true
	return e.writeMetadataEA(path, p)
}

func (e *xattrForkEngine) ReadComment(path string) (c []byte, ok bool) {
	p, present, err := e.readMetadataEA(path)
	if err != nil || !present || !p.HasComment {
		return nil, false
	}
	return p.Comment, true
}

func (e *xattrForkEngine) WriteComment(path string, c []byte) error {
	p, _, err := e.readMetadataEA(path)
	if err != nil {
		return err
	}
	p.Comment = append([]byte(nil), c...)
	p.HasComment = len(c) > 0
	return e.writeMetadataEA(path, p)
}

func (e *xattrForkEngine) MoveMetadata(old, new string) error {
	for _, ea := range []func(string) string{metadataEAPath, resourceEAPath} {
		src := ea(old)
		if _, err := e.fs.Stat(src); err != nil {
			if errors.Is(err, stdfs.ErrNotExist) {
				continue
			}
			return err
		}
		if err := e.fs.Rename(src, ea(new)); err != nil {
			return err
		}
	}
	return nil
}

func (e *xattrForkEngine) DeleteMetadata(path string) error {
	for _, ea := range []func(string) string{metadataEAPath, resourceEAPath} {
		if err := e.fs.Remove(ea(path)); err != nil && !errors.Is(err, stdfs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// --- xattrResourceFork (File) ---

// xattrResourceFork wraps the ResourceFork EA handle so that, after the resource
// fork's length changes (Truncate / extending WriteAt), the Metadata EA's
// recorded ad_entry length is refreshed on Sync/Close. Netatalk keeps the two in
// step; without this, an enumerate that reads the length from the Metadata EA
// would disagree with the actual ResourceFork EA size.
type xattrResourceFork struct {
	engine *xattrForkEngine
	path   string
	inner  File
	dirty  bool
	closed bool
}

func (f *xattrResourceFork) ReadAt(p []byte, off int64) (int, error) {
	return f.inner.ReadAt(p, off)
}

func (f *xattrResourceFork) WriteAt(p []byte, off int64) (int, error) {
	n, err := f.inner.WriteAt(p, off)
	if n > 0 {
		f.dirty = true
	}
	return n, err
}

func (f *xattrResourceFork) Truncate(size int64) error {
	if err := f.inner.Truncate(size); err != nil {
		return err
	}
	f.dirty = true
	return nil
}

func (f *xattrResourceFork) Stat() (stdfs.FileInfo, error) { return f.inner.Stat() }

func (f *xattrResourceFork) Sync() error {
	if err := f.inner.Sync(); err != nil {
		return err
	}
	if !f.dirty {
		return nil
	}
	// Refresh the Metadata EA's recorded resource length. If no Metadata EA
	// exists yet, seed one so the length is recorded (matching Netatalk, which
	// always carries a Metadata EA once a resource fork is present).
	p, _, err := f.engine.readMetadataEA(f.path)
	if err != nil {
		return err
	}
	if err := f.engine.writeMetadataEA(f.path, p); err != nil {
		return err
	}
	f.dirty = false
	return nil
}

func (f *xattrResourceFork) Close() error {
	if f.closed {
		return nil
	}
	err := f.Sync()
	if cerr := f.inner.Close(); err == nil {
		err = cerr
	}
	f.closed = true
	return err
}

var _ ForkEngine = (*xattrForkEngine)(nil)
