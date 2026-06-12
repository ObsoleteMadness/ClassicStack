package metastore

import (
	"errors"
	"os"
	"path/filepath"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// Snapshot wire format (stdlib only; no encoding/binary → no reflect):
//
//	magic  [4]byte = "MST1"
//	repeat:
//	  keyLen uint32 BE, key bytes
//	  valLen uint32 BE, val bytes
//
// A missing file loads as empty. A truncated/corrupt file is a load error.
var snapshotMagic = [4]byte{'M', 'S', 'T', '1'}

// ErrCorruptSnapshot is returned by load when the file is not a valid snapshot.
var ErrCorruptSnapshot = errors.New("metastore: corrupt snapshot")

// save serialises the map to path atomically (temp file + rename). Caller holds at least RLock.
func (s *memStore) save() error {
	buf := make([]byte, 0, 4+len(s.m)*32)
	buf = append(buf, snapshotMagic[:]...)
	for k, v := range s.m {
		buf = bp.AppendBE32(buf, uint32(len(k)))
		buf = append(buf, k...)
		buf = bp.AppendBE32(buf, uint32(len(v)))
		buf = append(buf, v...)
	}

	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".metastore-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(buf); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, s.path)
}

// load reads path into the map. A missing file is not an error (empty store).
func (s *memStore) load() error {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(b) < 4 || [4]byte{b[0], b[1], b[2], b[3]} != snapshotMagic {
		return ErrCorruptSnapshot
	}
	off := 4
	for off < len(b) {
		k, n, err := readField(b, off)
		if err != nil {
			return err
		}
		off = n
		v, n2, err := readField(b, off)
		if err != nil {
			return err
		}
		off = n2
		s.m[string(k)] = v
	}
	return nil
}

// readField reads one BE32-length-prefixed field starting at off, returning the bytes and the
// next offset.
func readField(b []byte, off int) (field []byte, next int, err error) {
	if off+4 > len(b) {
		return nil, 0, ErrCorruptSnapshot
	}
	n := int(bp.BE32(b[off:]))
	off += 4
	if n < 0 || off+n > len(b) {
		return nil, 0, ErrCorruptSnapshot
	}
	return append([]byte(nil), b[off:off+n]...), off + n, nil
}
