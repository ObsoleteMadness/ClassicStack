package finder

import (
	"context"
	"fmt"
	"os"

	"github.com/ObsoleteMadness/ClassicStack/core/archive"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

func expandRef(req ExpandRequest) NodeRef {
	if req.Path != "" || req.ID.ByPath {
		if req.ID.ByPath {
			return req.ID
		}
		return PathRef(req.Path)
	}
	return req.ID
}

// Expand unpacks a classic Mac archive next to its parent folder on the session catalog.
func (s *Service) Expand(ctx context.Context, req ExpandRequest, emit func(OpProgress)) error {
	sess, ffs, err := s.sessionFS(req.SessionID)
	if err != nil {
		return err
	}
	if sess.readOnly {
		return ErrReadOnly
	}
	path, err := s.storePath(sess, expandRef(req))
	if err != nil {
		return err
	}
	info, err := ffs.Stat(path)
	if err != nil || info.IsDir() {
		return fmt.Errorf("finder: expand: %w", ErrNotFound)
	}
	data, err := readWholeFork(ffs, path, fs.DataFork)
	if err != nil {
		return err
	}
	var fi [32]byte
	if got, ok, err := ffs.ReadFinderInfo(path); err == nil && ok {
		fi = got
	}
	name := path
	if i := len(path) - 1; i >= 0 {
		for j := i; j >= 0; j-- {
			if path[j] == '/' {
				name = path[j+1:]
				break
			}
			if j == 0 {
				name = path
			}
		}
	}
	if emit != nil {
		emit(OpProgress{Phase: PhaseExpanding, Path: name})
	}
	nodes, err := archive.Expand(name, data, nil, fi)
	if err != nil {
		return err
	}
	parent := ""
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			parent = path[:i]
			break
		}
	}
	var written int64
	var writeTree func(context.Context, string, []archive.Node) error
	writeTree = func(ctx context.Context, parentPath string, kids []archive.Node) error {
		for _, n := range kids {
			if err := ctx.Err(); err != nil {
				return err
			}
			dst := joinStore(parentPath, n.Name)
			if n.IsDir {
				if err := ffs.CreateDir(dst); err != nil && !os.IsExist(err) {
					return err
				}
				if err := writeTree(ctx, dst, n.Children); err != nil {
					return err
				}
				continue
			}
			f, err := ffs.CreateFile(dst)
			if err != nil {
				return err
			}
			if len(n.Data) > 0 {
				if _, err := f.WriteAt(n.Data, 0); err != nil {
					_ = f.Close()
					return err
				}
			}
			_ = f.Close()
			if len(n.Resource) > 0 {
				rf, err := ffs.OpenFork(dst, fs.ResourceFork, os.O_RDWR|os.O_CREATE|os.O_TRUNC)
				if err != nil {
					return err
				}
				if _, err := rf.WriteAt(n.Resource, 0); err != nil {
					_ = rf.Close()
					return err
				}
				_ = rf.Close()
			}
			_ = ffs.WriteFinderInfo(dst, n.FinderInfo)
			written += int64(len(n.Data) + len(n.Resource))
			if emit != nil {
				emit(OpProgress{Phase: PhaseExpanding, Path: n.Name, BytesDone: written})
			}
		}
		return nil
	}
	if err := writeTree(ctx, parent, nodes); err != nil {
		return err
	}
	s.log.Log2(log.Debug, "finder expanded archive", log.Str("path", path), log.Int("count", int64(len(nodes))))
	if emit != nil {
		emit(OpProgress{Phase: PhaseExpanding, Done: true})
	}
	return nil
}

func readWholeFork(ffs fs.ForkFS, path string, fork fs.ForkType) ([]byte, error) {
	n, err := ffs.ForkLen(path, fork)
	if err != nil || n <= 0 {
		if fork == fs.DataFork {
			f, err := ffs.OpenFile(path, os.O_RDONLY)
			if err != nil {
				return nil, err
			}
			defer f.Close()
			info, err := f.Stat()
			if err != nil {
				return nil, err
			}
			n = info.Size()
		} else {
			return nil, err
		}
	}
	f, err := ffs.OpenFork(path, fork, os.O_RDONLY)
	if err != nil {
		if fork == fs.DataFork {
			f, err = ffs.OpenFile(path, os.O_RDONLY)
		}
		if err != nil {
			return nil, err
		}
	}
	defer f.Close()
	buf := make([]byte, n)
	off := int64(0)
	for off < n {
		got, err := f.ReadAt(buf[off:], off)
		if got > 0 {
			off += int64(got)
		}
		if err != nil {
			break
		}
	}
	return buf[:off], nil
}
