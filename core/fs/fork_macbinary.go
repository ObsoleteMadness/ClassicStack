package fs

import (
	"errors"
	"io"
	stdfs "io/fs"
	"os"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// fork_macbinary.go implements the "macbinary" fork adapter: a single-container backend
// where each store file is a MacBinary II archive holding the data fork, the resource
// fork, and the Finder type/creator + flags in a 128-byte header. Like AppleSingle it is
// self-contained (the plain file IS the container), so OpenFork(Data/Resource) and the
// FinderInfo accessor read-modify-write the same file and MetadataPaths is nil.
//
// MacBinary II layout (the fields this seam needs):
//   - byte   0: old version, always 0
//   - byte   1: filename length (1..63)
//   - bytes  2..64: filename (Pascal-padded to 63)
//   - bytes 65..68: file TYPE (4)         -> FinderInfo[0:4]
//   - bytes 69..72: file CREATOR (4)      -> FinderInfo[4:8]
//   - byte  73: Finder flags high byte    -> FinderInfo[8]
//   - byte  74: always 0
//   - bytes 75..76: vertical position; 77..78 horizontal; 79..80 window/folder id
//     (Finder window geometry -> FinderInfo[10:16]; we round-trip what we hold)
//   - byte  81: protected flag; byte 82: always 0
//   - bytes 83..86: data-fork length (BE32)
//   - bytes 87..90: resource-fork length (BE32)
//   - bytes 91..94 creation date; 95..98 modification date (Mac epoch)
//   - byte 122: MacBinary version (129 = II); byte 123: min version to extract
//   - bytes 124..125: CRC of the header; 126..127: reserved
// Forks follow the header, each padded to a 128-byte boundary: data fork first, then
// resource fork. The Finder flags low byte (in the MacBinary II extended area) is not
// modelled; FinderInfo bytes 8..9 carry the high flags byte we read/write.

const (
	mbHeaderSize    = 128
	mbForkAlign     = 128
	mbVersionII     = 129 // byte 122 for MacBinary II
	mbMaxNameLen    = 63
	mbOffName       = 2
	mbOffType       = 65
	mbOffCreator    = 69
	mbOffFlagsHi    = 73
	mbOffDataLen    = 83
	mbOffRsrcLen    = 87
	mbOffVersion    = 122
	mbOffMinVersion = 123
)

type macBinaryForkEngine struct {
	fs FileSystem
}

func newMacBinaryForkEngine(base FileSystem) *macBinaryForkEngine {
	return &macBinaryForkEngine{fs: base}
}

func init() {
	RegisterForkAdapter("macbinary", func(spec ShareSpec, base FileSystem) (ForkEngine, error) {
		_ = spec
		return newMacBinaryForkEngine(base), nil
	})
}

// mbContainer is the decoded contents of one MacBinary file.
type mbContainer struct {
	name     string
	finder   [32]byte // bytes 0:8 = type/creator, 8 = flags-hi (the subset MacBinary carries)
	data     []byte
	resource []byte
	hasFind  bool
}

func (e *macBinaryForkEngine) readContainer(path string) (mbContainer, bool, error) {
	b, err := e.readAll(path)
	if err != nil {
		if errors.Is(err, stdfs.ErrNotExist) {
			return mbContainer{}, false, nil
		}
		return mbContainer{}, false, err
	}
	c, err := decodeMacBinary(b)
	if err != nil {
		return mbContainer{}, false, err
	}
	return c, true, nil
}

// decodeMacBinary parses a MacBinary II byte stream. A stream that fails the validity
// checks (version byte, zero bytes, name length) is rejected so the engine never
// overwrites a non-MacBinary file as if it were one.
func decodeMacBinary(b []byte) (mbContainer, error) {
	var c mbContainer
	if len(b) < mbHeaderSize {
		return c, stdfs.ErrInvalid
	}
	if b[0] != 0 || b[74] != 0 || b[82] != 0 {
		return c, stdfs.ErrInvalid
	}
	nameLen := int(b[1])
	if nameLen < 1 || nameLen > mbMaxNameLen {
		return c, stdfs.ErrInvalid
	}
	if b[mbOffVersion] != mbVersionII {
		return c, stdfs.ErrInvalid
	}
	c.name = string(b[mbOffName : mbOffName+nameLen])
	copy(c.finder[0:4], b[mbOffType:mbOffType+4])
	copy(c.finder[4:8], b[mbOffCreator:mbOffCreator+4])
	c.finder[8] = b[mbOffFlagsHi]
	c.hasFind = true

	dataLen := int(bp.BE32(b[mbOffDataLen : mbOffDataLen+4]))
	rsrcLen := int(bp.BE32(b[mbOffRsrcLen : mbOffRsrcLen+4]))
	off := mbHeaderSize
	dataEnd := off + dataLen
	if dataLen < 0 || dataEnd > len(b) {
		return c, stdfs.ErrInvalid
	}
	c.data = append([]byte(nil), b[off:dataEnd]...)
	off = mbAlign(dataEnd)
	rsrcEnd := off + rsrcLen
	if rsrcLen < 0 || rsrcEnd > len(b) {
		return c, stdfs.ErrInvalid
	}
	c.resource = append([]byte(nil), b[off:rsrcEnd]...)
	return c, nil
}

// encodeMacBinary serialises a container to MacBinary II bytes (header + padded forks).
func encodeMacBinary(c mbContainer) []byte {
	name := c.name
	if name == "" {
		name = "untitled"
	}
	if len(name) > mbMaxNameLen {
		name = name[:mbMaxNameLen]
	}
	h := make([]byte, mbHeaderSize)
	h[1] = byte(len(name))
	copy(h[mbOffName:], name)
	copy(h[mbOffType:mbOffType+4], c.finder[0:4])
	copy(h[mbOffCreator:mbOffCreator+4], c.finder[4:8])
	h[mbOffFlagsHi] = c.finder[8]
	bp.PutBE32(h[mbOffDataLen:mbOffDataLen+4], uint32(len(c.data)))
	bp.PutBE32(h[mbOffRsrcLen:mbOffRsrcLen+4], uint32(len(c.resource)))
	h[mbOffVersion] = mbVersionII
	h[mbOffMinVersion] = mbVersionII

	out := h
	out = append(out, c.data...)
	out = padTo(out, mbForkAlign)
	out = append(out, c.resource...)
	out = padTo(out, mbForkAlign)
	return out
}

func mbAlign(n int) int {
	if r := n % mbForkAlign; r != 0 {
		return n + (mbForkAlign - r)
	}
	return n
}

func padTo(b []byte, align int) []byte {
	if r := len(b) % align; r != 0 {
		b = append(b, make([]byte, align-r)...)
	}
	return b
}

func (e *macBinaryForkEngine) writeContainer(path string, c mbContainer) error {
	if c.name == "" {
		_, c.name = splitPath(path)
	}
	return e.writeAll(path, encodeMacBinary(c))
}

func (e *macBinaryForkEngine) readAll(path string) ([]byte, error) {
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

func (e *macBinaryForkEngine) writeAll(path string, b []byte) error {
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

func (e *macBinaryForkEngine) OpenFork(path string, fork ForkType, flag int) (File, error) {
	c, ok, err := e.readContainer(path)
	if err != nil {
		return nil, err
	}
	if !ok && flag&os.O_CREATE == 0 {
		return nil, stdfs.ErrNotExist
	}
	if fork == DataFork {
		return &mbForkFile{engine: e, path: path, fork: DataFork, data: append([]byte(nil), c.data...)}, nil
	}
	return &mbForkFile{engine: e, path: path, fork: ResourceFork, data: append([]byte(nil), c.resource...)}, nil
}

func (e *macBinaryForkEngine) ForkLen(path string, fork ForkType) (int64, error) {
	c, ok, err := e.readContainer(path)
	if err != nil || !ok {
		return 0, err
	}
	if fork == DataFork {
		return int64(len(c.data)), nil
	}
	return int64(len(c.resource)), nil
}

func (e *macBinaryForkEngine) ReadFinderInfo(path string) (info [32]byte, ok bool, err error) {
	c, present, err := e.readContainer(path)
	if err != nil || !present || !c.hasFind {
		return [32]byte{}, false, err
	}
	return c.finder, true, nil
}

func (e *macBinaryForkEngine) WriteFinderInfo(path string, info [32]byte) error {
	c, _, err := e.readContainer(path)
	if err != nil {
		return err
	}
	c.finder = info
	c.hasFind = true
	return e.writeContainer(path, c)
}

// MacBinary has no comment field; comments are dropped (read empty, write no-op) so the
// engine still satisfies ForkEngine.
func (e *macBinaryForkEngine) ReadComment(path string) ([]byte, bool) { _ = path; return nil, false }
func (e *macBinaryForkEngine) WriteComment(path string, c []byte) error {
	_ = path
	_ = c
	return nil
}

// MoveMetadata / DeleteMetadata are no-ops: the container IS the file.
func (e *macBinaryForkEngine) MoveMetadata(old, new string) error { _ = old; _ = new; return nil }
func (e *macBinaryForkEngine) DeleteMetadata(path string) error   { _ = path; return nil }

// MetadataPaths returns nil: a MacBinary file has no separate container to coordinate.
func (e *macBinaryForkEngine) MetadataPaths(storePath string) []string { _ = storePath; return nil }

// mbForkFile is a buffered view of one fork within a MacBinary container.
type mbForkFile struct {
	engine *macBinaryForkEngine
	path   string
	fork   ForkType
	data   []byte
	dirty  bool
	closed bool
}

func (f *mbForkFile) ReadAt(p []byte, off int64) (int, error) {
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

func (f *mbForkFile) WriteAt(p []byte, off int64) (int, error) {
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

func (f *mbForkFile) Truncate(size int64) error {
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

func (f *mbForkFile) Stat() (stdfs.FileInfo, error) {
	if f.closed {
		return nil, stdfs.ErrClosed
	}
	_, base := splitPath(f.path)
	return memFileInfo{name: base, size: int64(len(f.data))}, nil
}

func (f *mbForkFile) Sync() error {
	if !f.dirty {
		return nil
	}
	return f.flush()
}

func (f *mbForkFile) Close() error {
	if f.closed {
		return nil
	}
	f.closed = true
	if !f.dirty {
		return nil
	}
	return f.flush()
}

func (f *mbForkFile) flush() error {
	c, _, err := f.engine.readContainer(f.path)
	if err != nil {
		return err
	}
	if f.fork == DataFork {
		c.data = append([]byte(nil), f.data...)
	} else {
		c.resource = append([]byte(nil), f.data...)
	}
	f.dirty = false
	return f.engine.writeContainer(f.path, c)
}
