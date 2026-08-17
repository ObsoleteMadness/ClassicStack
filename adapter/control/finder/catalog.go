package finder

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

func joinStore(parent, name string) string {
	name = strings.Trim(name, "/")
	if parent == "" {
		return name
	}
	if name == "" {
		return parent
	}
	return parent + "/" + name
}

func (s *Service) pathFor(sess *Session, id uint32) (string, error) {
	ffs, err := sess.requireFS()
	if err != nil {
		return "", err
	}
	meta := ffs.Meta()
	if id == 0 || id == meta.RootCNID() {
		return "", nil
	}
	path, ok := meta.PathForCNID(id)
	if !ok {
		return "", fmt.Errorf("finder: node %d: %w", id, ErrNotFound)
	}
	return path, nil
}

// GetNode returns one catalog node by CNID.
func (s *Service) GetNode(sessionID string, id uint32) (Node, error) {
	sess, err := s.get(sessionID)
	if err != nil {
		return Node{}, err
	}
	ffs, err := sess.requireFS()
	if err != nil {
		return Node{}, err
	}
	path, err := s.pathFor(sess, id)
	if err != nil {
		return Node{}, err
	}
	info, err := ffs.Stat(path)
	if err != nil {
		return Node{}, err
	}
	parent := uint32(1)
	if path != "" {
		dir := path
		if i := strings.LastIndex(path, "/"); i >= 0 {
			dir = path[:i]
		} else {
			dir = ""
		}
		parent = ffs.Meta().EnsureCNID(dir)
	}
	name := path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		name = path[i+1:]
	}
	if path == "" {
		name = sess.Volume
		if name == "" {
			name = sess.ServerName
		}
	}
	n, err := nodeFrom(ffs, path, name, info.IsDir(), parent)
	if err == nil {
		s.log.Log2(log.Debug, "finder get node", log.Str("session", sessionID), log.Str("path", path))
	}
	return n, err
}

// Children lists directory entries under parent CNID.
func (s *Service) Children(sessionID string, parentID uint32) ([]Node, error) {
	sess, err := s.get(sessionID)
	if err != nil {
		return nil, err
	}
	ffs, err := sess.requireFS()
	if err != nil {
		return nil, err
	}
	path, err := s.pathFor(sess, parentID)
	if err != nil {
		return nil, err
	}
	ents, err := ffs.ReadDir(path)
	if err != nil {
		return nil, err
	}
	pid := ffs.Meta().EnsureCNID(path)
	out := make([]Node, 0, len(ents))
	for _, e := range ents {
		child := joinStore(path, e.Name())
		n, err := nodeFrom(ffs, child, e.Name(), e.IsDir(), pid)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	s.log.Log2(log.Debug, "finder list children", log.Str("path", path), log.Int("count", int64(len(out))))
	return out, nil
}

// Lookup finds a named child of parent.
func (s *Service) Lookup(sessionID string, parentID uint32, name string) (Node, error) {
	kids, err := s.Children(sessionID, parentID)
	if err != nil {
		return Node{}, err
	}
	for _, n := range kids {
		if n.Name == name {
			return n, nil
		}
	}
	return Node{}, ErrNotFound
}

// Mkdir creates a directory.
func (s *Service) Mkdir(sessionID string, parentID uint32, name string) (Node, error) {
	sess, err := s.get(sessionID)
	if err != nil {
		return Node{}, err
	}
	if sess.readOnly {
		return Node{}, ErrReadOnly
	}
	ffs, err := sess.requireFS()
	if err != nil {
		return Node{}, err
	}
	parent, err := s.pathFor(sess, parentID)
	if err != nil {
		return Node{}, err
	}
	path := joinStore(parent, name)
	if err := ffs.CreateDir(path); err != nil {
		return Node{}, err
	}
	s.log.Log1(log.Debug, "finder mkdir", log.Str("path", path))
	return nodeFrom(ffs, path, name, true, ffs.Meta().EnsureCNID(parent))
}

// CreateFile creates an empty file, optionally writing data and resource forks.
func (s *Service) CreateFile(sessionID string, parentID uint32, name string, data, resource, finderInfo []byte) (Node, error) {
	sess, err := s.get(sessionID)
	if err != nil {
		return Node{}, err
	}
	if sess.readOnly {
		return Node{}, ErrReadOnly
	}
	ffs, err := sess.requireFS()
	if err != nil {
		return Node{}, err
	}
	parent, err := s.pathFor(sess, parentID)
	if err != nil {
		return Node{}, err
	}
	path := joinStore(parent, name)
	f, err := ffs.CreateFile(path)
	if err != nil {
		return Node{}, err
	}
	_ = f.Close()
	if len(data) > 0 {
		if err := writeFork(ffs, path, fs.DataFork, data); err != nil {
			return Node{}, err
		}
	}
	if len(resource) > 0 {
		if err := writeFork(ffs, path, fs.ResourceFork, resource); err != nil {
			return Node{}, err
		}
	}
	if len(finderInfo) >= 32 {
		var fi [32]byte
		copy(fi[:], finderInfo)
		if err := ffs.WriteFinderInfo(path, fi); err != nil {
			return Node{}, err
		}
	}
	s.log.Log1(log.Debug, "finder create file", log.Str("path", path))
	return nodeFrom(ffs, path, name, false, ffs.Meta().EnsureCNID(parent))
}

func writeFork(ffs fs.ForkFS, path string, fork fs.ForkType, data []byte) error {
	f, err := ffs.OpenFork(path, fork, os.O_RDWR)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Truncate(int64(len(data))); err != nil {
		return err
	}
	_, err = f.WriteAt(data, 0)
	return err
}

// Rename renames a node within its parent.
func (s *Service) Rename(sessionID string, id uint32, newName string) error {
	sess, err := s.get(sessionID)
	if err != nil {
		return err
	}
	if sess.readOnly {
		return ErrReadOnly
	}
	ffs, err := sess.requireFS()
	if err != nil {
		return err
	}
	path, err := s.pathFor(sess, id)
	if err != nil {
		return err
	}
	dir := ""
	if i := strings.LastIndex(path, "/"); i >= 0 {
		dir = path[:i]
	}
	dest := joinStore(dir, newName)
	s.log.Log2(log.Debug, "finder rename", log.Str("from", path), log.Str("to", dest))
	if err := ffs.Rename(path, dest); err != nil {
		return err
	}
	return ffs.Meta().RebindCNID(path, dest)
}

// Move moves a node to a new parent directory.
func (s *Service) Move(sessionID string, id, newParent uint32) error {
	sess, err := s.get(sessionID)
	if err != nil {
		return err
	}
	if sess.readOnly {
		return ErrReadOnly
	}
	ffs, err := sess.requireFS()
	if err != nil {
		return err
	}
	path, err := s.pathFor(sess, id)
	if err != nil {
		return err
	}
	parent, err := s.pathFor(sess, newParent)
	if err != nil {
		return err
	}
	name := path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		name = path[i+1:]
	}
	dest := joinStore(parent, name)
	s.log.Log2(log.Debug, "finder move", log.Str("from", path), log.Str("to", dest))
	if err := ffs.Rename(path, dest); err != nil {
		return err
	}
	return ffs.Meta().RebindCNID(path, dest)
}

// Remove deletes a node.
func (s *Service) Remove(sessionID string, id uint32) error {
	sess, err := s.get(sessionID)
	if err != nil {
		return err
	}
	if sess.readOnly {
		return ErrReadOnly
	}
	ffs, err := sess.requireFS()
	if err != nil {
		return err
	}
	path, err := s.pathFor(sess, id)
	if err != nil {
		return err
	}
	s.log.Log1(log.Debug, "finder remove", log.Str("path", path))
	if err := ffs.Remove(path); err != nil {
		return err
	}
	return ffs.Meta().RemoveCNID(path)
}

// ReadFork reads a slice of a data or resource fork.
func (s *Service) ReadFork(sessionID string, id uint32, resource bool, off, length int64) ([]byte, error) {
	sess, err := s.get(sessionID)
	if err != nil {
		return nil, err
	}
	ffs, err := sess.requireFS()
	if err != nil {
		return nil, err
	}
	path, err := s.pathFor(sess, id)
	if err != nil {
		return nil, err
	}
	fork := fs.DataFork
	if resource {
		fork = fs.ResourceFork
	}
	if length <= 0 {
		sz, err := ffs.ForkLen(path, fork)
		if err != nil {
			return nil, err
		}
		length = sz - off
		if length < 0 {
			length = 0
		}
	}
	f, err := ffs.OpenFork(path, fork, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, length)
	n, err := f.ReadAt(buf, off)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	s.log.Log2(log.Debug, "finder read fork", log.Str("path", path), log.Int("n", int64(n)))
	return buf[:n], nil
}

// WriteFork writes a slice of a data or resource fork.
func (s *Service) WriteFork(sessionID string, id uint32, resource bool, off int64, data []byte) error {
	sess, err := s.get(sessionID)
	if err != nil {
		return err
	}
	if sess.readOnly {
		return ErrReadOnly
	}
	ffs, err := sess.requireFS()
	if err != nil {
		return err
	}
	path, err := s.pathFor(sess, id)
	if err != nil {
		return err
	}
	fork := fs.DataFork
	if resource {
		fork = fs.ResourceFork
	}
	f, err := ffs.OpenFork(path, fork, os.O_RDWR)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteAt(data, off)
	s.log.Log2(log.Debug, "finder write fork", log.Str("path", path), log.Int("n", int64(len(data))))
	return err
}

// WriteFinderInfo sets the 32-byte Finder info for a node.
func (s *Service) WriteFinderInfo(sessionID string, id uint32, info []byte) error {
	sess, err := s.get(sessionID)
	if err != nil {
		return err
	}
	if sess.readOnly {
		return ErrReadOnly
	}
	ffs, err := sess.requireFS()
	if err != nil {
		return err
	}
	path, err := s.pathFor(sess, id)
	if err != nil {
		return err
	}
	var fi [32]byte
	copy(fi[:], info)
	s.log.Log1(log.Debug, "finder write finderinfo", log.Str("path", path))
	return ffs.WriteFinderInfo(path, fi)
}
