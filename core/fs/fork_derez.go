// SPDX-FileCopyrightText: Based on macresrources by Elliot Nunn
// SPDX-License-Identifier: MIT
package fs

import (
	"errors"
	"io"
	stdfs "io/fs"
	"os"

	"github.com/ObsoleteMadness/ClassicStack/core/macresources"
)

// fork_derez.go implements the "derez" fork adapter: a SIDECAR backend that stores a
// file's resource fork as human-readable, version-controllable text instead of an opaque
// binary blob. It is the on-disk form used by Elliot Nunn's macresources tool:
//
//	rdump/idump format & reference implementation: macresources by Elliot Nunn
//	https://github.com/elliotnunn/macresources
//
// The motivating use case is a developer working on a CLASSIC Mac codebase (e.g. a
// CodeWarrior project) who wants to check resources into git: a binary resource fork is
// undiffable, but the DeRez text form is. When a client READS the resource fork, derez
// reads the ".rdump" text sidecar and SERIALISES it back to the binary resource fork the
// Resource Manager expects; when a client WRITES the resource fork, derez DESERIALISES
// the binary fork back to ".rdump" text. The file's type/creator (the first 8 bytes of
// Finder info) are kept in a companion ".idump" sidecar — the same split macresources
// uses (the rdump carries the resources, the idump carries the Finder type/creator).
//
// Sidecars (store-relative, beside the data file):
//   - "<name>.rdump" — the Rez/DeRez text of the resource fork (core/macresources codec)
//   - "<name>.idump" — 8 bytes: 4-byte TYPE + 4-byte CREATOR (Finder info bytes 0..7)
//
// The data fork is the plain file (like AppleDouble). Comments are not represented in
// the rdump/idump pair, so derez drops them (read empty, write no-op). MetadataPaths
// reports BOTH sidecars so a same-host-path peer follows them on rename/delete.

const (
	derezRdumpExt = ".rdump"
	derezIdumpExt = ".idump"
)

type derezForkEngine struct {
	fs FileSystem
}

func newDerezForkEngine(base FileSystem) *derezForkEngine {
	return &derezForkEngine{fs: base}
}

func init() {
	RegisterForkAdapter("derez", func(spec ShareSpec, base FileSystem) (ForkEngine, error) {
		_ = spec
		return newDerezForkEngine(base), nil
	})
}

func (e *derezForkEngine) rdumpPath(path string) string { return path + derezRdumpExt }
func (e *derezForkEngine) idumpPath(path string) string { return path + derezIdumpExt }

// readResources reads the rdump sidecar and parses it to resources; ok is false when the
// sidecar is absent.
func (e *derezForkEngine) readResources(path string) (res []macresources.Resource, ok bool, err error) {
	b, err := e.readAll(e.rdumpPath(path))
	if err != nil {
		if errors.Is(err, stdfs.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	res, err = macresources.ParseRez(b)
	if err != nil {
		return nil, false, err
	}
	return res, true, nil
}

// writeResources serialises resources to the rdump sidecar (or removes it when empty).
func (e *derezForkEngine) writeResources(path string, res []macresources.Resource) error {
	if len(res) == 0 {
		err := e.fs.Remove(e.rdumpPath(path))
		if errors.Is(err, stdfs.ErrNotExist) {
			return nil
		}
		return err
	}
	return e.writeAll(e.rdumpPath(path), macresources.FormatRez(res))
}

func (e *derezForkEngine) readIdump(path string) (info [32]byte, ok bool) {
	b, err := e.readAll(e.idumpPath(path))
	if err != nil || len(b) < 8 {
		return [32]byte{}, false
	}
	copy(info[0:8], b[0:8])
	return info, true
}

func (e *derezForkEngine) writeIdump(path string, info [32]byte) error {
	// Only the type/creator (first 8 bytes) round-trip through the idump.
	return e.writeAll(e.idumpPath(path), info[0:8])
}

// --- ForkEngine ---

func (e *derezForkEngine) OpenFork(path string, fork ForkType, flag int) (File, error) {
	if fork == DataFork {
		return e.fs.OpenFile(path, flag)
	}
	res, ok, err := e.readResources(path)
	if err != nil {
		return nil, err
	}
	if !ok && flag&os.O_CREATE == 0 {
		return nil, stdfs.ErrNotExist
	}
	// Serialise the resources to the binary resource fork the client sees; buffer it,
	// and on write-back deserialise it to rdump text again.
	var bin []byte
	if ok {
		bin = macresources.BuildResourceFork(res)
	}
	return &derezForkFile{engine: e, path: path, data: append([]byte(nil), bin...)}, nil
}

func (e *derezForkEngine) ForkLen(path string, fork ForkType) (int64, error) {
	if fork == DataFork {
		info, err := e.fs.Stat(path)
		if err != nil {
			return 0, err
		}
		return info.Size(), nil
	}
	res, ok, err := e.readResources(path)
	if err != nil || !ok {
		return 0, err
	}
	return int64(len(macresources.BuildResourceFork(res))), nil
}

func (e *derezForkEngine) ReadFinderInfo(path string) (info [32]byte, ok bool, err error) {
	info, ok = e.readIdump(path)
	return info, ok, nil
}

func (e *derezForkEngine) WriteFinderInfo(path string, info [32]byte) error {
	return e.writeIdump(path, info)
}

// Comments are not part of the rdump/idump pair, so derez does not persist them.
func (e *derezForkEngine) ReadComment(path string) ([]byte, bool) { _ = path; return nil, false }
func (e *derezForkEngine) WriteComment(path string, c []byte) error {
	_ = path
	_ = c
	return nil
}

func (e *derezForkEngine) MoveMetadata(old, new string) error {
	if err := e.moveOne(e.rdumpPath(old), e.rdumpPath(new)); err != nil {
		return err
	}
	return e.moveOne(e.idumpPath(old), e.idumpPath(new))
}

func (e *derezForkEngine) moveOne(src, dst string) error {
	if _, err := e.fs.Stat(src); err != nil {
		if errors.Is(err, stdfs.ErrNotExist) {
			return nil
		}
		return err
	}
	return e.fs.Rename(src, dst)
}

func (e *derezForkEngine) DeleteMetadata(path string) error {
	for _, p := range []string{e.rdumpPath(path), e.idumpPath(path)} {
		if err := e.fs.Remove(p); err != nil && !errors.Is(err, stdfs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// MetadataPaths reports both sidecars (fs.ForkContainers): the rdump and idump files a
// same-host-path peer must follow on a rename/delete.
func (e *derezForkEngine) MetadataPaths(storePath string) []string {
	return []string{e.rdumpPath(storePath), e.idumpPath(storePath)}
}

// derezForkFile buffers the binary resource fork a client reads/writes; on Close/Sync it
// deserialises the buffer back to rdump text via the macresources codec.
type derezForkFile struct {
	engine *derezForkEngine
	path   string
	data   []byte
	dirty  bool
	closed bool
}

func (f *derezForkFile) ReadAt(p []byte, off int64) (int, error) {
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

func (f *derezForkFile) WriteAt(p []byte, off int64) (int, error) {
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

func (f *derezForkFile) Truncate(size int64) error {
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

func (f *derezForkFile) Stat() (stdfs.FileInfo, error) {
	if f.closed {
		return nil, stdfs.ErrClosed
	}
	_, base := splitPath(f.path)
	return memFileInfo{name: base, size: int64(len(f.data))}, nil
}

func (f *derezForkFile) Sync() error {
	if !f.dirty {
		return nil
	}
	return f.flush()
}

func (f *derezForkFile) Close() error {
	if f.closed {
		return nil
	}
	f.closed = true
	if !f.dirty {
		return nil
	}
	return f.flush()
}

// flush parses the buffered binary resource fork and writes it back as rdump text. An
// empty buffer removes the rdump sidecar (the resource fork was cleared).
func (f *derezForkFile) flush() error {
	f.dirty = false
	if len(f.data) == 0 {
		return f.engine.writeResources(f.path, nil)
	}
	res, err := macresources.ParseResourceFork(f.data)
	if err != nil {
		return err
	}
	return f.engine.writeResources(f.path, res)
}

func (e *derezForkEngine) readAll(path string) ([]byte, error) {
	f, err := e.fs.OpenFile(path, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer f.Close()
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

func (e *derezForkEngine) writeAll(path string, b []byte) error {
	f, err := e.fs.OpenFile(path, os.O_RDWR|os.O_CREATE)
	if err != nil {
		f, err = e.fs.CreateFile(path)
		if err != nil {
			return err
		}
	}
	defer f.Close()
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
