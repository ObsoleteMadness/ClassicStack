// Package xfer holds the protocol-agnostic file operations the CLI runs over
// fs.ForkFS pairs: List, Copy, Move, Remove, SetAttr. Because every client (and the
// host side) is an fs.ForkFS, these operations don't know or care whether a side is
// AFP, SMB, NCP, EtherDFS, or the local disk — remote→host, host→remote and
// remote→remote copies are ONE code path.
//
// Copy preserves everything both sides can represent: the data fork always; the
// resource fork and Finder info when both sides carry forks (a ForkEngine that is not
// the no-op adapter); and DOS attributes when both sides expose a MetaEngine. A side
// that cannot represent a piece of metadata simply drops it — the copy still succeeds
// with the data fork intact.
//
// Ring: CLIENT.
package xfer

import (
	"errors"
	"fmt"
	"io"
	stdfs "io/fs"
	"os"
	"path"
	"sort"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// copyBufSize is the data-fork streaming chunk. It is comfortably larger than any one
// protocol's per-request cap (ASP quantum 4624, SMB MaxBufferSize) so the underlying
// File.WriteAt/ReadAt — which the protocol client chunks internally — sees big writes.
const copyBufSize = 64 * 1024

// Entry is one listing row, protocol-neutral. Type/Creator are the MacRoman
// four-char codes from Finder info (empty when the side carries no forks or the entry
// has none); Attr is the DOS attribute set (zero when unavailable).
type Entry struct {
	Name     string
	IsDir    bool
	Size     int64
	RsrcSize int64 // resource-fork length, 0 when none/unsupported
	Type     string
	Creator  string
	Attr     fs.DOSAttr
}

// List returns the entries in dir on fsys, enriched with fork/type/creator/attr where
// the backend supports them (best-effort: a metadata read that fails is skipped, the
// row still returned).
func List(fsys fs.ForkFS, dir string) ([]Entry, error) {
	des, err := fsys.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(des))
	for _, de := range des {
		e := Entry{Name: de.Name(), IsDir: de.IsDir()}
		full := joinPath(dir, de.Name())
		if info, err := de.Info(); err == nil {
			e.Size = info.Size()
		}
		if !e.IsDir {
			if n, err := fsys.ForkLen(full, fs.ResourceFork); err == nil {
				e.RsrcSize = n
			}
			if fi, ok, err := fsys.ReadFinderInfo(full); err == nil && ok {
				e.Type = string(trimNUL(fi[0:4]))
				e.Creator = string(trimNUL(fi[4:8]))
			}
		}
		if attr, ok := fsys.Meta().Attrs(full); ok {
			e.Attr = attr
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Copy copies src (a file or, recursively, a directory) from srcFS to dst on dstFS,
// preserving data fork, resource fork, Finder info, and DOS attributes where both
// sides support them. dst names the destination path (not its parent); a directory
// copy creates dst and recurses into it.
func Copy(srcFS, dstFS fs.ForkFS, src, dst string) error {
	info, err := srcFS.Stat(src)
	if err != nil {
		return fmt.Errorf("stat %s: %w", src, err)
	}
	if info.IsDir() {
		return copyDir(srcFS, dstFS, src, dst)
	}
	return copyFile(srcFS, dstFS, src, dst)
}

// copyDir recurses: create dst, copy every child, then carry the directory's own
// metadata (Finder info / DOS attrs) last.
func copyDir(srcFS, dstFS fs.ForkFS, src, dst string) error {
	if err := dstFS.CreateDir(dst); err != nil && !errors.Is(err, stdfs.ErrExist) {
		return fmt.Errorf("mkdir %s: %w", dst, err)
	}
	des, err := srcFS.ReadDir(src)
	if err != nil {
		return err
	}
	for _, de := range des {
		cs := joinPath(src, de.Name())
		cd := joinPath(dst, de.Name())
		if de.IsDir() {
			if err := copyDir(srcFS, dstFS, cs, cd); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(srcFS, dstFS, cs, cd); err != nil {
			return err
		}
	}
	copyMeta(srcFS, dstFS, src, dst, true)
	return nil
}

// copyFile copies one file's data fork, then its resource fork and metadata.
func copyFile(srcFS, dstFS fs.ForkFS, src, dst string) error {
	if err := copyFork(srcFS, dstFS, src, dst, fs.DataFork, true); err != nil {
		return fmt.Errorf("copy data fork %s: %w", src, err)
	}
	// Resource fork: only when the source actually has one (ForkLen > 0). A side
	// that cannot represent it (nofork adapter) returns 0 and we skip silently.
	if n, err := srcFS.ForkLen(src, fs.ResourceFork); err == nil && n > 0 {
		if err := copyFork(srcFS, dstFS, src, dst, fs.ResourceFork, false); err != nil {
			return fmt.Errorf("copy resource fork %s: %w", src, err)
		}
	}
	copyMeta(srcFS, dstFS, src, dst, false)
	return nil
}

// copyFork streams one fork from src to dst. createData creates the destination file
// (data fork); the resource fork opens the already-created file's resource fork.
func copyFork(srcFS, dstFS fs.ForkFS, src, dst string, fork fs.ForkType, createData bool) error {
	var (
		in  fs.File
		err error
	)
	if fork == fs.DataFork {
		in, err = srcFS.OpenFile(src, os.O_RDONLY)
	} else {
		in, err = srcFS.OpenFork(src, fork, os.O_RDONLY)
	}
	if err != nil {
		return err
	}
	defer in.Close()

	var out fs.File
	if fork == fs.DataFork && createData {
		out, err = dstFS.CreateFile(dst)
	} else if fork == fs.DataFork {
		out, err = dstFS.OpenFile(dst, os.O_RDWR|os.O_CREATE|os.O_TRUNC)
	} else {
		out, err = dstFS.OpenFork(dst, fork, os.O_RDWR|os.O_CREATE|os.O_TRUNC)
	}
	if err != nil {
		return err
	}
	defer out.Close()

	buf := make([]byte, copyBufSize)
	var off int64
	for {
		n, rerr := in.ReadAt(buf, off)
		if n > 0 {
			if _, werr := out.WriteAt(buf[:n], off); werr != nil {
				return werr
			}
			off += int64(n)
		}
		if rerr == io.EOF || (rerr == nil && n == 0) {
			break
		}
		if rerr != nil && rerr != io.EOF {
			return rerr
		}
	}
	return out.Sync()
}

// copyMeta carries Finder info and DOS attributes from src to dst, best-effort. A
// side that cannot represent a piece (no-op fork adapter, no stored attrs) is a no-op.
func copyMeta(srcFS, dstFS fs.ForkFS, src, dst string, _ bool) {
	if fi, ok, err := srcFS.ReadFinderInfo(src); err == nil && ok {
		_ = dstFS.WriteFinderInfo(dst, fi)
	}
	if attr, ok := srcFS.Meta().Attrs(src); ok {
		_ = dstFS.Meta().SetAttrs(dst, attr)
	}
	if c, ok := srcFS.ReadComment(src); ok {
		_ = dstFS.WriteComment(dst, c)
	}
}

// Move renames within one FS when both sides are the same, else copies then removes.
// (The CLI only calls Move within one connection; a cross-FS move is copy+remove.)
func Move(fsys fs.ForkFS, src, dst string) error {
	return fsys.Rename(src, dst)
}

// Remove deletes a path (recursively for a directory) on fsys, carrying metadata
// removal through the shareFS Remove wrapper.
func Remove(fsys fs.ForkFS, target string) error {
	info, err := fsys.Stat(target)
	if err != nil {
		return err
	}
	if info.IsDir() {
		des, err := fsys.ReadDir(target)
		if err != nil {
			return err
		}
		for _, de := range des {
			if err := Remove(fsys, joinPath(target, de.Name())); err != nil {
				return err
			}
		}
	}
	return fsys.Remove(target)
}

// SetAttr updates the DOS attribute bits on target: set adds the bits in mask, clear
// removes them. It reads the current attrs (or a zero value), applies the change, and
// writes back through the MetaEngine.
func SetAttr(fsys fs.ForkFS, target string, set, clear uint16) error {
	attr, _ := fsys.Meta().Attrs(target)
	attr.Attrs |= set
	attr.Attrs &^= clear
	return fsys.Meta().SetAttrs(target, attr)
}

// SetType writes the four-char Finder type code, preserving the creator and the rest
// of the Finder info. An empty or non-4-char code is padded/truncated to 4 bytes.
func SetType(fsys fs.ForkFS, target, typ string) error {
	fi, _, _ := fsys.ReadFinderInfo(target)
	copyFourCC(fi[0:4], typ)
	return fsys.WriteFinderInfo(target, fi)
}

// SetCreator writes the four-char Finder creator code, preserving the type.
func SetCreator(fsys fs.ForkFS, target, creator string) error {
	fi, _, _ := fsys.ReadFinderInfo(target)
	copyFourCC(fi[4:8], creator)
	return fsys.WriteFinderInfo(target, fi)
}

// copyFourCC writes a four-char code into dst[0:4], space-padding short codes and
// truncating long ones (classic OSType/Creator are exactly four bytes).
func copyFourCC(dst []byte, code string) {
	for i := 0; i < 4; i++ {
		if i < len(code) {
			dst[i] = code[i]
		} else {
			dst[i] = ' '
		}
	}
}

// trimNUL trims trailing NUL bytes from a four-char code for display.
func trimNUL(b []byte) []byte {
	end := len(b)
	for end > 0 && (b[end-1] == 0) {
		end--
	}
	return b[:end]
}

// joinPath joins a '/'-separated share path element, treating "" as the root.
func joinPath(dir, name string) string {
	if dir == "" {
		return name
	}
	return path.Join(dir, name)
}
