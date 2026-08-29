package metastore

import "github.com/ObsoleteMadness/ClassicStack/core/log"

// EA is one named extended-attribute value — the storage-layer equivalent of
// an [MS-CIFS] §2.2.1.2.2 SMB_FEA record, minus the wire-only length
// prefixes. OS/2 (via the SMB TRANS2_SET_PATH_INFORMATION/
// TRANS2_QUERY_PATH_INFORMATION SMB_INFO_SET_EAS/SMB_INFO_QUERY_ALL_EAS
// levels, and NT_TRANSACT_CREATE) uses these to attach named metadata like
// ".LONGNAME"/".CLASSINFO"/".TYPE" to a file. The value's internal typing
// (OS/2's own EAT_ASCII/EAT_BINARY/EAT_MVMT tags) is opaque to this layer —
// stored and returned byte-for-byte.
type EA struct {
	Name   string
	Value  []byte
	NeedEA bool // mirrors SMB_FEA's FILE_NEED_EA (0x80) flag
}

// EAStore persists named extended attributes for paths, mirroring
// DOSAttrStore's shape. Paths are the share's '/'-separated store paths.
type EAStore interface {
	// Get returns the stored EAs for path. ok is false when nothing is
	// stored (the caller then reports an empty EA list).
	Get(path string) (eas []EA, ok bool)
	// Set persists eas for path, replacing any previously stored list —
	// matching SMB_INFO_SET_EAS "set the EA list" semantics.
	Set(path string, eas []EA) error
	// Delete drops any stored EAs for path (called on remove).
	Delete(path string) error
	// Rename moves stored EAs from oldPath to newPath (called on rename).
	Rename(oldPath, newPath string) error
}

// metaEAStore is the metastore-backed EAStore, the definitive per-share
// implementation shared by every MetaEngine backend (metastore/xattr/ads) —
// unlike DOS attributes, EAs have no host-native (NTFS/xattr) storage this
// codebase targets, so there is only the one implementation.
type metaEAStore struct {
	store   Store
	logging log.Logger // established at construction, never nil; sinks own level filtering
}

// NewEAStore returns a metastore-backed EAStore over store (nil → a volatile
// in-memory store). A nil logger gets a no-op logger, matching the rest of
// the codebase's injection convention.
func NewEAStore(store Store, logger log.Logger) EAStore {
	if store == nil {
		store, _ = NewMem("")
	}
	if logger == nil {
		logger = log.New("eastore")
	}
	return &metaEAStore{store: store, logging: logger}
}

// eaListVersion1 tags the on-disk EA-list record encoding below. It is a
// ClassicStack-private record, not [MS-CIFS] SMB_FEA_LIST — the SMB layer
// transcodes between this and the wire SMB_FEA_LIST shape (OEM names,
// different length-field widths) so the stored form stays independent of
// any client's request charset.
const eaListVersion1 = 1

// metastore key layout: "d/e/<path>" -> EA-list v1 blob (see dosattr.go for
// the sibling "d/a/"/"d/x/" DOS-attribute keys).
func eaKey(path string) []byte { return []byte("d/e/" + cleanPath(path)) }

// encodeEAList renders eas as a self-contained record: version(2) count(2),
// then per entry needEA(1) nameLen(2) name[nameLen] valueLen(4)
// value[valueLen]. Names are stored as UTF-8.
func encodeEAList(eas []EA) []byte {
	size := 4
	for _, e := range eas {
		size += 1 + 2 + len(e.Name) + 4 + len(e.Value)
	}
	out := make([]byte, size)
	putLE16(out[0:2], eaListVersion1)
	putLE16(out[2:4], uint16(len(eas)))
	off := 4
	for _, e := range eas {
		if e.NeedEA {
			out[off] = 1
		}
		off++
		putLE16(out[off:off+2], uint16(len(e.Name)))
		off += 2
		off += copy(out[off:], e.Name)
		putLE32(out[off:off+4], uint32(len(e.Value)))
		off += 4
		off += copy(out[off:], e.Value)
	}
	return out
}

// decodeEAList parses a record written by encodeEAList. A truncated or
// unrecognised-version blob yields (nil, false) — treated as "no EAs
// stored" rather than surfacing a decode error to a client.
func decodeEAList(b []byte) ([]EA, bool) {
	if len(b) < 4 || le16(b[0:2]) != eaListVersion1 {
		return nil, false
	}
	count := int(le16(b[2:4]))
	out := make([]EA, 0, count)
	off := 4
	for i := 0; i < count; i++ {
		if off+1+2 > len(b) {
			return nil, false
		}
		needEA := b[off] != 0
		off++
		nameLen := int(le16(b[off : off+2]))
		off += 2
		if off+nameLen+4 > len(b) {
			return nil, false
		}
		name := string(b[off : off+nameLen])
		off += nameLen
		valueLen := int(le32(b[off : off+4]))
		off += 4
		if off+valueLen > len(b) {
			return nil, false
		}
		value := append([]byte(nil), b[off:off+valueLen]...)
		off += valueLen
		out = append(out, EA{Name: name, Value: value, NeedEA: needEA})
	}
	return out, true
}

func (s *metaEAStore) Get(path string) ([]EA, bool) {
	v, ok := s.store.Get(eaKey(path))
	if !ok {
		s.logging.Log1(log.Debug, "ea cache miss", log.Str("path", path))
		return nil, false
	}
	eas, ok := decodeEAList(v)
	if !ok {
		s.logging.Log1(log.Debug, "ea decode failed, treating as miss", log.Str("path", path))
		return nil, false
	}
	s.logging.Log1(log.Debug, "ea cache hit", log.Str("path", path))
	return eas, true
}

func (s *metaEAStore) Set(path string, eas []EA) error {
	if err := s.store.Put(eaKey(path), encodeEAList(eas)); err != nil {
		return err
	}
	s.logging.Log1(log.Debug, "ea set", log.Str("path", path))
	return nil
}

func (s *metaEAStore) Delete(path string) error {
	s.logging.Log1(log.Debug, "ea delete", log.Str("path", path))
	return s.store.Delete(eaKey(path))
}

func (s *metaEAStore) Rename(oldPath, newPath string) error {
	s.logging.Log2(log.Debug, "ea rename", log.Str("old", oldPath), log.Str("new", newPath))
	v, ok := s.store.Get(eaKey(oldPath))
	if !ok {
		return nil
	}
	if err := s.store.Put(eaKey(newPath), v); err != nil {
		return err
	}
	return s.store.Delete(eaKey(oldPath))
}
