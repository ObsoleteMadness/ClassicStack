package afp

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ExtensionMapping holds the Macintosh type/creator pair resolved from a file extension.
type ExtensionMapping struct {
	FileType [4]byte
	Creator  [4]byte
}

// ExtensionMap stores netatalk-compatible extension mappings.
type ExtensionMap struct {
	entries        map[string]ExtensionMapping
	defaultMapping ExtensionMapping
	hasDefault     bool
}

// NewExtensionMapping validates and builds a Macintosh file type/creator mapping.
func NewExtensionMapping(fileType, creator string) (ExtensionMapping, error) {
	if len(fileType) != 4 {
		return ExtensionMapping{}, fmt.Errorf("type must be exactly 4 bytes, got %q", fileType)
	}
	if len(creator) != 4 {
		return ExtensionMapping{}, fmt.Errorf("creator must be exactly 4 bytes, got %q", creator)
	}

	var mapping ExtensionMapping
	copy(mapping.FileType[:], fileType)
	copy(mapping.Creator[:], creator)
	return mapping, nil
}

// NewExtensionMap validates and builds an extension map keyed by extension.
// The map must include a default '.' mapping.
func NewExtensionMap(entries map[string]ExtensionMapping) (*ExtensionMap, error) {
	if len(entries) == 0 {
		return nil, fmt.Errorf("extension map is empty")
	}

	normalizedEntries := make(map[string]ExtensionMapping, len(entries))
	var defaultMapping ExtensionMapping
	hasDefault := false

	for ext, mapping := range entries {
		normalizedExt := strings.ToLower(strings.TrimSpace(ext))
		if normalizedExt == "" {
			return nil, fmt.Errorf("extension map contains empty extension key")
		}
		normalizedEntries[normalizedExt] = mapping
		if normalizedExt == "." {
			defaultMapping = mapping
			hasDefault = true
		}
	}

	if !hasDefault {
		return nil, fmt.Errorf("extension map is missing default '.' mapping")
	}

	return &ExtensionMap{
		entries:        normalizedEntries,
		defaultMapping: defaultMapping,
		hasDefault:     true,
	}, nil
}

// Lookup returns the mapping for the file extension in path, or the default '.' mapping.
func (m *ExtensionMap) Lookup(path string) (ExtensionMapping, bool) {
	if m == nil {
		return ExtensionMapping{}, false
	}

	ext := strings.ToLower(filepath.Ext(path))
	if ext != "" {
		if mapping, ok := m.entries[ext]; ok {
			return mapping, true
		}
	}
	if m.hasDefault {
		return m.defaultMapping, true
	}
	return ExtensionMapping{}, false
}

func hasFinderTypeCreator(finderInfo [32]byte) bool {
	for i := 0; i < 8; i++ {
		if finderInfo[i] != 0 {
			return true
		}
	}
	return false
}

func applyExtensionMapping(finderInfo [32]byte, mapping ExtensionMapping) [32]byte {
	copy(finderInfo[0:4], mapping.FileType[:])
	copy(finderInfo[4:8], mapping.Creator[:])
	return finderInfo
}
