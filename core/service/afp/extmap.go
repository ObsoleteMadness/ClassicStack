package afp

// extmap.go is the Netatalk-style extension→type/creator map: when a file has no
// stored Finder info, the volume supplies a DEFAULT classic-Mac type/creator pair
// derived from the filename extension, so a `.txt` reads as TEXT/ttxt rather than as
// 8 zero bytes. Ported from the legacy internal/app/extension_map.go (the parser) +
// service/afp/extension_map.go (the types), re-homed into the AFP service ring.
//
// The on-disk format is Netatalk's: one entry per line, `.ext "TYPE" "CRTR"`, where
// TYPE/CRTR are exactly four characters (the classic OSType / creator codes). Blank
// lines and lines beginning with '#' are ignored. Lookups are case-insensitive on the
// extension.

import (
	"errors"
	"strconv"
	"strings"
)

// DefaultExtMapPath is the process-global Netatalk-style extension map edited from
// Settings → General → File type mappings. A volume with an empty ExtMapPath uses
// this file when it exists.
const DefaultExtMapPath = "extmap.conf"

// ExtensionMapping is one extension's classic-Mac type + creator codes (4 bytes each).
type ExtensionMapping struct {
	FileType [4]byte
	Creator  [4]byte
}

// NewExtensionMapping builds a mapping from the 4-char type and creator strings,
// rejecting any not exactly four bytes (the OSType width).
func NewExtensionMapping(fileType, creator string) (ExtensionMapping, error) {
	if len(fileType) != 4 {
		return ExtensionMapping{}, errors.New("afp: type must be exactly 4 bytes, got " + strconv.Quote(fileType))
	}
	if len(creator) != 4 {
		return ExtensionMapping{}, errors.New("afp: creator must be exactly 4 bytes, got " + strconv.Quote(creator))
	}
	var m ExtensionMapping
	copy(m.FileType[:], fileType)
	copy(m.Creator[:], creator)
	return m, nil
}

// ExtensionMap maps a lowercased extension (without the leading dot) to its mapping.
// A nil *ExtensionMap is valid and matches nothing (Lookup returns ok=false), so a
// volume with no map configured needs no nil guards at the call site.
type ExtensionMap struct {
	entries map[string]ExtensionMapping
}

// NewExtensionMap builds a map from parsed entries (keys already lowercased,
// dot-stripped). Entries may be nil/empty — the resulting map matches nothing.
func NewExtensionMap(entries map[string]ExtensionMapping) (*ExtensionMap, error) {
	return &ExtensionMap{entries: entries}, nil
}

// Lookup returns the mapping for a path's extension, or ok=false when the path has no
// extension or no entry matches. A nil map matches nothing.
func (m *ExtensionMap) Lookup(path string) (ExtensionMapping, bool) {
	if m == nil || len(m.entries) == 0 {
		return ExtensionMapping{}, false
	}
	ext := extensionOf(path)
	if ext == "" {
		return ExtensionMapping{}, false
	}
	mp, ok := m.entries[strings.ToLower(ext)]
	return mp, ok
}

// FinderInfo returns a 32-byte Finder-info record carrying the mapping's type/creator
// in the FInfo (type at bytes 0-3, creator at 4-7), the rest zero — the form the
// catalog packer emits for a defaulted file.
func (mp ExtensionMapping) FinderInfo() [32]byte {
	var info [32]byte
	copy(info[0:4], mp.FileType[:])
	copy(info[4:8], mp.Creator[:])
	return info
}

// Entries returns a copy of the map's entries keyed by lowercased extension, for a UI
// (the extmap grid) or a serialiser to render. Order is unspecified.
func (m *ExtensionMap) Entries() map[string]ExtensionMapping {
	if m == nil {
		return nil
	}
	out := make(map[string]ExtensionMapping, len(m.entries))
	for k, v := range m.entries {
		out[k] = v
	}
	return out
}

// extensionOf returns a path's extension without the leading dot (and without any
// directory), or "" when there is none. Reflection-free, stdlib-light.
func extensionOf(path string) string {
	// Trim any directory so a dot in a parent dir name is not mistaken for an extension.
	if i := strings.LastIndexAny(path, "/\\"); i >= 0 {
		path = path[i+1:]
	}
	dot := strings.LastIndex(path, ".")
	if dot <= 0 || dot == len(path)-1 {
		return "" // no dot, leading-dot dotfile, or trailing dot
	}
	return path[dot+1:]
}
