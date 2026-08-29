package fs

import (
	"errors"
	"io"
	stdfs "io/fs"
	"os"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// fork_applesingle.go implements the "applesingle" fork adapter: a TRUE AppleSingle
// backend where the plain store file IS one self-contained AppleSingle container
// holding the data fork, the resource fork, and the Finder metadata in a single stream
// — not an AppleDouble sidecar beside a separate data file. This is the format
// produced by classic Mac transfer tools and the `.as` files in a MacBinary/AppleSingle
// archive; it lets a directory of AppleSingle files be served with both forks intact.
//
// Format (AppleSingle/AppleDouble spec; magic distinguishes the two):
//   - 26-byte header: magic(4)=0x00051600, version(4)=0x00020000, filler(16)=0,
//     entry count(2);
//   - then N 12-byte entry descriptors: id(4), offset(4), length(4);
//   - then the entry payloads. Entry IDs: data fork=1, resource fork=2, comment=4,
//     Finder info=9 (the set this seam uses).
//
// Because everything lives in ONE file, OpenFork(DataFork) and OpenFork(ResourceFork)
// both read-modify-write the same container (buffered, flushed on Close), as do the
// FinderInfo / comment accessors. There is no separate sidecar, so MetadataPaths
// returns nil — nothing moves alongside the file on rename/delete (the file itself is
// the container, handled by the base FileSystem).

// AppleSingle magic/version and the entry IDs this engine reads/writes.
const (
	appleSingleMagic   uint32 = 0x00051600
	appleSingleVersion uint32 = 0x00020000

	asHeaderSize = 26 // magic(4)+version(4)+filler(16)+entryCount(2)
	asEntrySize  = 12 // id(4)+offset(4)+length(4)

	asEntryDataFork     uint32 = 1
	asEntryResourceFork uint32 = 2
	asEntryComment      uint32 = 4
	asEntryFinderInfo   uint32 = 9
)

// appleSingleForkEngine serves a store tree where each file is an AppleSingle container.
type appleSingleForkEngine struct {
	fs FileSystem
}

func newAppleSingleForkEngine(base FileSystem) *appleSingleForkEngine {
	return &appleSingleForkEngine{fs: base}
}

func init() {
	RegisterForkAdapter("applesingle", func(spec ShareSpec, base FileSystem) (ForkEngine, error) {
		_ = spec
		return newAppleSingleForkEngine(base), nil
	})
}

// asContainer is the decoded contents of one AppleSingle file.
type asContainer struct {
	data     []byte
	resource []byte
	finder   [32]byte
	comment  []byte
	hasData  bool
	hasRsrc  bool
	hasFind  bool
	hasCmt   bool
}

// readContainer reads and decodes the AppleSingle file at path. ok is false when the
// file does not exist; a present file with the wrong magic is an error (it is not an
// AppleSingle container, so the engine must not silently overwrite it).
func (e *appleSingleForkEngine) readContainer(path string) (asContainer, bool, error) {
	b, err := e.readAll(path)
	if err != nil {
		if errors.Is(err, stdfs.ErrNotExist) {
			return asContainer{}, false, nil
		}
		return asContainer{}, false, err
	}
	c, err := decodeAppleSingle(b)
	if err != nil {
		return asContainer{}, false, err
	}
	return c, true, nil
}

// decodeAppleSingle parses an AppleSingle byte stream into its forks/metadata.
func decodeAppleSingle(b []byte) (asContainer, error) {
	var c asContainer
	if len(b) < asHeaderSize {
		return c, stdfs.ErrInvalid
	}
	if bp.BE32(b[0:4]) != appleSingleMagic {
		return c, stdfs.ErrInvalid
	}
	n := int(bp.BE16(b[24:26]))
	descBase := asHeaderSize
	if descBase+n*asEntrySize > len(b) {
		return c, stdfs.ErrInvalid
	}
	for i := 0; i < n; i++ {
		d := descBase + i*asEntrySize
		id := bp.BE32(b[d : d+4])
		off := int(bp.BE32(b[d+4 : d+8]))
		ln := int(bp.BE32(b[d+8 : d+12]))
		if off < 0 || ln < 0 || off+ln > len(b) {
			return c, stdfs.ErrInvalid
		}
		payload := b[off : off+ln]
		switch id {
		case asEntryDataFork:
			c.data = append([]byte(nil), payload...)
			c.hasData = true
		case asEntryResourceFork:
			c.resource = append([]byte(nil), payload...)
			c.hasRsrc = true
		case asEntryFinderInfo:
			c.hasFind = true
			copy(c.finder[:], payload) // tolerate <32; copy fills what it can
		case asEntryComment:
			c.comment = append([]byte(nil), payload...)
			c.hasCmt = true
		}
	}
	return c, nil
}

// asResourceChunk is the allocation granularity Apple recommends for the resource fork
// in an AppleSingle file: rounding the resource entry's slot up to 4K leaves a "hole"
// after it, so a later resource-fork edit that still fits the slot does not shift the
// data fork and forces no full rewrite (CiderPress2 AppleSingle-notes). The descriptor
// records the TRUE resource length; the slack between true length and the 4K-rounded
// allocation is a permitted gap.
const asResourceChunk = 4096

// encodeAppleSingle serialises a container to canonical AppleSingle bytes. Entry order
// follows the writing recommendations: FinderInfo, then comment, then the resource fork
// (allocated in 4K chunks so it can grow in place), then the DATA FORK LAST — the data
// fork is the entry most often appended to, so keeping it at EOF lets it grow without
// disturbing the others. FinderInfo is always emitted so a fresh container is
// well-formed. A reader (decodeAppleSingle) honours arbitrary offsets/holes, so this
// layout round-trips through any conformant parser.
func encodeAppleSingle(c asContainer) []byte {
	type ent struct {
		id      uint32
		payload []byte
		alloc   int // bytes reserved before the next entry (>= len(payload)); the "hole"
	}
	// FinderInfo first (always present), then comment, then resource (4K-allocated),
	// then data fork last.
	ents := []ent{{asEntryFinderInfo, c.finder[:], 32}}
	if c.hasCmt && len(c.comment) > 0 {
		ents = append(ents, ent{asEntryComment, c.comment, len(c.comment)})
	}
	if c.hasRsrc {
		ents = append(ents, ent{asEntryResourceFork, c.resource, roundUp(len(c.resource), asResourceChunk)})
	}
	if c.hasData {
		ents = append(ents, ent{asEntryDataFork, c.data, len(c.data)})
	}

	header := asHeaderSize + len(ents)*asEntrySize
	out := make([]byte, header)
	bp.PutBE32(out[0:4], appleSingleMagic)
	bp.PutBE32(out[4:8], appleSingleVersion)
	bp.PutBE16(out[24:26], uint16(len(ents)))

	off := header
	for i, en := range ents {
		d := asHeaderSize + i*asEntrySize
		bp.PutBE32(out[d:d+4], en.id)
		bp.PutBE32(out[d+4:d+8], uint32(off))
		bp.PutBE32(out[d+8:d+12], uint32(len(en.payload))) // TRUE length, not the alloc
		out = append(out, en.payload...)
		// Pad to the entry's allocation, leaving a hole before the next entry.
		if pad := en.alloc - len(en.payload); pad > 0 {
			out = append(out, make([]byte, pad)...)
		}
		off += en.alloc
	}
	return out
}

// roundUp rounds n up to the next multiple of chunk (chunk must be > 0). A zero n
// allocates one empty chunk's worth of nothing — returns 0, so an absent resource fork
// reserves no slack.
func roundUp(n, chunk int) int {
	if n <= 0 {
		return 0
	}
	if r := n % chunk; r != 0 {
		return n + (chunk - r)
	}
	return n
}

func (e *appleSingleForkEngine) writeContainer(path string, c asContainer) error {
	return e.writeAll(path, encodeAppleSingle(c))
}

func (e *appleSingleForkEngine) readAll(path string) ([]byte, error) {
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

func (e *appleSingleForkEngine) writeAll(path string, b []byte) error {
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

// --- ForkEngine ---

func (e *appleSingleForkEngine) OpenFork(path string, fork ForkType, flag int) (File, error) {
	c, ok, err := e.readContainer(path)
	if err != nil {
		return nil, err
	}
	if !ok && flag&os.O_CREATE == 0 {
		return nil, stdfs.ErrNotExist
	}
	if fork == DataFork {
		return &asForkFile{engine: e, path: path, fork: DataFork, data: append([]byte(nil), c.data...)}, nil
	}
	return &asForkFile{engine: e, path: path, fork: ResourceFork, data: append([]byte(nil), c.resource...)}, nil
}

func (e *appleSingleForkEngine) ForkLen(path string, fork ForkType) (int64, error) {
	c, ok, err := e.readContainer(path)
	if err != nil || !ok {
		return 0, err
	}
	if fork == DataFork {
		return int64(len(c.data)), nil
	}
	return int64(len(c.resource)), nil
}

func (e *appleSingleForkEngine) ReadFinderInfo(path string) (info [32]byte, ok bool, err error) {
	c, present, err := e.readContainer(path)
	if err != nil || !present || !c.hasFind {
		return [32]byte{}, false, err
	}
	return c.finder, true, nil
}

func (e *appleSingleForkEngine) WriteFinderInfo(path string, info [32]byte) error {
	c, _, err := e.readContainer(path)
	if err != nil {
		return err
	}
	c.finder = info
	c.hasFind = true
	return e.writeContainer(path, c)
}

func (e *appleSingleForkEngine) ReadComment(path string) (cmt []byte, ok bool) {
	c, present, err := e.readContainer(path)
	if err != nil || !present || !c.hasCmt {
		return nil, false
	}
	return c.comment, true
}

func (e *appleSingleForkEngine) WriteComment(path string, cmt []byte) error {
	c, _, err := e.readContainer(path)
	if err != nil {
		return err
	}
	c.comment = append([]byte(nil), cmt...)
	c.hasCmt = len(cmt) > 0
	return e.writeContainer(path, c)
}

// MoveMetadata is a no-op: the container IS the file, so the base FileSystem's Rename of
// the data path already moves every fork and the metadata with it.
func (e *appleSingleForkEngine) MoveMetadata(old, new string) error {
	_ = old
	_ = new
	return nil
}

// DeleteMetadata is a no-op: removing the file removes the whole container.
func (e *appleSingleForkEngine) DeleteMetadata(path string) error {
	_ = path
	return nil
}

// MetadataPaths returns nil (fs.ForkContainers): an AppleSingle file has NO separate
// container — the data file itself holds the metadata, so nothing extra moves on a
// rename/delete and a same-host-path peer has no sidecar to follow.
func (e *appleSingleForkEngine) MetadataPaths(storePath string) []string {
	_ = storePath
	return nil
}

// asForkFile is a buffered view of one fork (data or resource) within an AppleSingle
// container that flushes the WHOLE container back on Close/Sync when written.
type asForkFile struct {
	engine *appleSingleForkEngine
	path   string
	fork   ForkType
	data   []byte
	dirty  bool
	closed bool
}

func (f *asForkFile) ReadAt(p []byte, off int64) (int, error) {
	if f.closed {
		return 0, stdfs.ErrClosed
	}
	if off < 0 {
		return 0, stdfs.ErrInvalid
	}
	if off >= int64(len(f.data)) {
		return 0, io.EOF
	}
	n := copy(p, f.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (f *asForkFile) WriteAt(p []byte, off int64) (int, error) {
	if f.closed {
		return 0, stdfs.ErrClosed
	}
	if off < 0 {
		return 0, stdfs.ErrInvalid
	}
	need := int(off) + len(p)
	if need > len(f.data) {
		nb := make([]byte, need)
		copy(nb, f.data)
		f.data = nb
	}
	copy(f.data[off:], p)
	f.dirty = true
	return len(p), nil
}

func (f *asForkFile) Truncate(size int64) error {
	if f.closed {
		return stdfs.ErrClosed
	}
	if size < 0 {
		return stdfs.ErrInvalid
	}
	if int(size) <= len(f.data) {
		f.data = append([]byte(nil), f.data[:size]...)
	} else {
		nb := make([]byte, size)
		copy(nb, f.data)
		f.data = nb
	}
	f.dirty = true
	return nil
}

func (f *asForkFile) Stat() (stdfs.FileInfo, error) {
	if f.closed {
		return nil, stdfs.ErrClosed
	}
	_, base := splitPath(f.path)
	return memFileInfo{name: base, size: int64(len(f.data))}, nil
}

func (f *asForkFile) Sync() error {
	if !f.dirty {
		return nil
	}
	return f.flush()
}

func (f *asForkFile) Close() error {
	if f.closed {
		return nil
	}
	f.closed = true
	if !f.dirty {
		return nil
	}
	return f.flush()
}

// flush merges this fork back into the on-disk container, preserving the other fork +
// metadata, then rewrites the whole AppleSingle file.
func (f *asForkFile) flush() error {
	c, _, err := f.engine.readContainer(f.path)
	if err != nil {
		return err
	}
	if f.fork == DataFork {
		c.data = append([]byte(nil), f.data...)
		c.hasData = true
	} else {
		c.resource = append([]byte(nil), f.data...)
		c.hasRsrc = true
	}
	f.dirty = false
	return f.engine.writeContainer(f.path, c)
}
