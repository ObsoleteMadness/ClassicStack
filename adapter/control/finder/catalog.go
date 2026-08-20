package finder

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/asp"
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

// hiddenListingName reports sidecar / Netatalk metadata names that catalogs omit
// when the share's fork adapter consumes them (AppleDouble, derez). A nofork
// share does not implement ListingFilter, so a host `._file` stays visible.
func hiddenListingName(ffs fs.ForkFS, name string) bool {
	if f, ok := ffs.(fs.ListingFilter); ok && f.HiddenName(name) {
		return true
	}
	switch strings.ToLower(name) {
	case ".appledesktop", ".desktop.db":
		return true
	}
	return false
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

func (s *Service) nodeAt(sess *Session, ffs fs.ForkFS, path, name string, isDir bool) (Node, error) {
	return nodeFrom(sess, ffs, path, name, isDir)
}

// GetNode returns one catalog node by native ref (CNID or store path).
func (s *Service) GetNode(sessionID string, ref NodeRef) (Node, error) {
	sess, err := s.get(sessionID)
	if err != nil {
		return Node{}, err
	}
	ffs, err := sess.requireFS()
	if err != nil {
		return Node{}, err
	}
	path, err := s.storePath(sess, ref)
	if err != nil {
		return Node{}, err
	}
	info, err := ffs.Stat(path)
	if err != nil {
		return Node{}, err
	}
	name := leafOf(path)
	n, err := s.nodeAt(sess, ffs, path, name, info.IsDir())
	if err == nil {
		s.log.Log2(log.Debug, "finder get node", log.Str("session", sessionID), log.Str("path", path))
	}
	return n, err
}

// Children lists directory entries under parent.
func (s *Service) Children(sessionID string, parent NodeRef) ([]Node, error) {
	sess, err := s.get(sessionID)
	if err != nil {
		return nil, err
	}
	ffs, err := sess.requireFS()
	if err != nil {
		return nil, err
	}
	path, err := s.storePath(sess, parent)
	if err != nil {
		return nil, err
	}
	ents, err := ffs.ReadDir(path)
	if err != nil {
		return nil, err
	}
	out := make([]Node, 0, len(ents))
	for _, e := range ents {
		if hiddenListingName(ffs, e.Name()) {
			continue
		}
		child := joinStore(path, e.Name())
		n, err := nodeFromEntry(sess, ffs, child, e)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	s.log.Log2(log.Debug, "finder list children", log.Str("path", path), log.Int("count", int64(len(out))))
	return out, nil
}

// Lookup finds a named child of parent via Stat, not a full directory listing.
func (s *Service) Lookup(sessionID string, parent NodeRef, name string) (Node, error) {
	sess, err := s.get(sessionID)
	if err != nil {
		return Node{}, err
	}
	ffs, err := sess.requireFS()
	if err != nil {
		return Node{}, err
	}
	dir, err := s.storePath(sess, parent)
	if err != nil {
		return Node{}, err
	}
	name = strings.Trim(name, "/")
	if name == "" || strings.Contains(name, "/") {
		return Node{}, ErrNotFound
	}
	path := joinStore(dir, name)
	info, err := ffs.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Node{}, ErrNotFound
		}
		return Node{}, err
	}
	s.log.Log2(log.Debug, "finder lookup", log.Str("path", path), log.Str("name", name))
	return nodeFrom(sess, ffs, path, name, info.IsDir())
}

// ResolvePath walks a store-relative path from the volume root to a native node.
func (s *Service) ResolvePath(sessionID, path string) (Node, error) {
	sess, err := s.get(sessionID)
	if err != nil {
		return Node{}, err
	}
	if sess.addressBy() == AddressPath {
		return s.GetNode(sessionID, PathRef(strings.Trim(path, "/")))
	}
	ffs, err := sess.requireFS()
	if err != nil {
		return Node{}, err
	}
	path = strings.Trim(path, "/")
	id, ok := ffs.Meta().CNID(path)
	if !ok {
		return Node{}, ErrNotFound
	}
	return s.GetNode(sessionID, CNIDRef(id))
}

// PathOf returns the store-relative path for a native ref.
func (s *Service) PathOf(sessionID string, ref NodeRef) (string, error) {
	sess, err := s.get(sessionID)
	if err != nil {
		return "", err
	}
	return s.storePath(sess, ref)
}

// Mkdir creates a directory.
func (s *Service) Mkdir(sessionID string, parent NodeRef, name string) (Node, error) {
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
	dir, err := s.storePath(sess, parent)
	if err != nil {
		return Node{}, err
	}
	path := joinStore(dir, name)
	if err := ffs.CreateDir(path); err != nil {
		return Node{}, err
	}
	s.log.Log1(log.Debug, "finder mkdir", log.Str("path", path))
	return nodeFrom(sess, ffs, path, name, true)
}

// CreateFile creates an empty file, optionally writing data and resource forks.
func (s *Service) CreateFile(sessionID string, parent NodeRef, name string, data, resource, finderInfo []byte) (Node, error) {
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
	dir, err := s.storePath(sess, parent)
	if err != nil {
		return Node{}, err
	}
	path := joinStore(dir, name)
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
	return nodeFrom(sess, ffs, path, name, false)
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

func rebindCNID(sess *Session, ffs fs.ForkFS, oldPath, newPath string) error {
	if sess.addressBy() != AddressCNID {
		return nil
	}
	return ffs.Meta().RebindCNID(oldPath, newPath)
}

// Rename renames a node within its parent.
func (s *Service) Rename(sessionID string, ref NodeRef, newName string) error {
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
	path, err := s.storePath(sess, ref)
	if err != nil {
		return err
	}
	dir := parentPathOf(path)
	dest := joinStore(dir, newName)
	s.log.Log2(log.Debug, "finder rename", log.Str("from", path), log.Str("to", dest))
	if err := ffs.Rename(path, dest); err != nil {
		return err
	}
	return rebindCNID(sess, ffs, path, dest)
}

// Move moves a node to a new parent directory.
func (s *Service) Move(sessionID string, ref, newParent NodeRef) error {
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
	path, err := s.storePath(sess, ref)
	if err != nil {
		return err
	}
	parent, err := s.storePath(sess, newParent)
	if err != nil {
		return err
	}
	name := leafOf(path)
	dest := joinStore(parent, name)
	s.log.Log2(log.Debug, "finder move", log.Str("from", path), log.Str("to", dest))
	if err := ffs.Rename(path, dest); err != nil {
		return err
	}
	return rebindCNID(sess, ffs, path, dest)
}

// Remove deletes a node.
func (s *Service) Remove(sessionID string, ref NodeRef) error {
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
	path, err := s.storePath(sess, ref)
	if err != nil {
		return err
	}
	s.log.Log1(log.Debug, "finder remove", log.Str("path", path))
	if err := ffs.Remove(path); err != nil {
		return err
	}
	if sess.addressBy() == AddressCNID {
		return ffs.Meta().RemoveCNID(path)
	}
	return nil
}

// ReadFork reads a slice of a data or resource fork. Like classicstack-web
// withOpenFork, it always opens the fork first, reads, and closes when finished
// so classic servers do not leak fork slots. A missing length reads until EOF
// in ASP-quantum chunks instead of ForkLen + allocating the whole fork.
func (s *Service) ReadFork(sessionID string, ref NodeRef, resource bool, off, length int64) ([]byte, error) {
	sess, err := s.get(sessionID)
	if err != nil {
		return nil, err
	}
	ffs, err := sess.requireFS()
	if err != nil {
		return nil, err
	}
	path, err := s.storePath(sess, ref)
	if err != nil {
		return nil, err
	}
	fork := fs.DataFork
	if resource {
		fork = fs.ResourceFork
	}
	f, err := ffs.OpenFork(path, fork, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var data []byte
	if length > 0 {
		buf := make([]byte, length)
		n, err := f.ReadAt(buf, off)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		data = buf[:n]
	} else {
		chunk := make([]byte, asp.QuantumSize)
		pos := off
		for {
			n, err := f.ReadAt(chunk, pos)
			if n > 0 {
				data = append(data, chunk[:n]...)
				pos += int64(n)
			}
			if n == 0 || errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, err
			}
		}
	}
	s.log.Log2(log.Debug, "finder read fork", log.Str("path", path), log.Int("n", int64(len(data))))
	return data, nil
}

// WriteFork writes a slice of a data or resource fork.
func (s *Service) WriteFork(sessionID string, ref NodeRef, resource bool, off int64, data []byte) error {
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
	path, err := s.storePath(sess, ref)
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
func (s *Service) WriteFinderInfo(sessionID string, ref NodeRef, info []byte) error {
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
	path, err := s.storePath(sess, ref)
	if err != nil {
		return err
	}
	var fi [32]byte
	copy(fi[:], info)
	s.log.Log1(log.Debug, "finder write finderinfo", log.Str("path", path))
	return ffs.WriteFinderInfo(path, fi)
}

// WriteAttrs patches boolean file flags by capability id (readonly, hidden, …).
func (s *Service) WriteAttrs(sessionID string, ref NodeRef, patch map[string]bool) error {
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
	path, err := s.storePath(sess, ref)
	if err != nil {
		return err
	}
	s.log.Log1(log.Debug, "finder write attrs", log.Str("path", path))
	attr, _ := ffs.Meta().Attrs(path)
	changed := false
	for id, v := range patch {
		switch id {
		case "readonly":
			attr.Attrs = setBit(attr.Attrs, fs.DOSReadOnly, v)
			changed = true
		case "hidden":
			attr.Attrs = setBit(attr.Attrs, fs.DOSHidden, v)
			changed = true
		case "system":
			attr.Attrs = setBit(attr.Attrs, fs.DOSSystem, v)
			changed = true
		case "archive":
			attr.Attrs = setBit(attr.Attrs, fs.DOSArchive, v)
			changed = true
		case "invisible", "locked":
			if err := patchFinderFlag(ffs, path, id, v); err != nil {
				return err
			}
		}
	}
	if changed {
		if err := ffs.Meta().SetAttrs(path, attr); err != nil {
			return err
		}
	}
	return nil
}

func setBit(bits, mask uint16, on bool) uint16 {
	if on {
		return bits | mask
	}
	return bits &^ mask
}

func patchFinderFlag(ffs fs.ForkFS, path, id string, on bool) error {
	fi, ok, err := ffs.ReadFinderInfo(path)
	if err != nil {
		return err
	}
	if !ok {
		fi = [32]byte{}
	}
	flags := uint16(fi[8])<<8 | uint16(fi[9])
	const kIsInvisible = 0x4000
	const kNameLocked = 0x1000
	bit := uint16(0)
	switch id {
	case "invisible":
		bit = kIsInvisible
	case "locked":
		bit = kNameLocked
	}
	if on {
		flags |= bit
	} else {
		flags &^= bit
	}
	fi[8] = byte(flags >> 8)
	fi[9] = byte(flags)
	return ffs.WriteFinderInfo(path, fi)
}
