package afp

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// extMapLinePattern matches one Netatalk extension-map line: an extension token, then
// a quoted 4-char TYPE and a quoted 4-char CRTR. Ported from the legacy parser.
var extMapLinePattern = regexp.MustCompile(`^(\S+)\s+"([^"]*)"\s+"([^"]*)"`)

// ParseExtensionMap parses Netatalk-style extension-map bytes into an ExtensionMap.
// Blank lines and '#' comments are skipped; a malformed line is a hard error naming the
// line number, so a typo cannot silently drop a mapping (the management plane validates
// an edited map with this before saving). Extension keys are lowercased and
// dot-stripped so lookups are case- and dot-insensitive.
func ParseExtensionMap(data []byte) (*ExtensionMap, error) {
	entries := make(map[string]ExtensionMapping)
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		match := extMapLinePattern.FindStringSubmatch(line)
		if len(match) != 4 {
			return nil, fmt.Errorf("afp: invalid extension map line %d: %q", i+1, raw)
		}
		mapping, err := NewExtensionMapping(match[2], match[3])
		if err != nil {
			return nil, fmt.Errorf("afp: invalid extension map line %d: %w", i+1, err)
		}
		key := strings.ToLower(strings.TrimPrefix(match[1], "."))
		entries[key] = mapping
	}
	return NewExtensionMap(entries)
}

// ValidateExtensionMap reports whether data is a parseable extension-map file (a thin
// wrapper the control plane calls before persisting an edited map).
func ValidateExtensionMap(data []byte) error {
	_, err := ParseExtensionMap(data)
	return err
}

// Marshal renders the map back to the Netatalk on-disk format (`.ext "TYPE" "CRTR"`),
// one entry per line, sorted by extension for deterministic output — the inverse of
// ParseExtensionMap, used when the UI grid saves an edited map.
func (m *ExtensionMap) Marshal() []byte {
	if m == nil || len(m.entries) == 0 {
		return nil
	}
	exts := make([]string, 0, len(m.entries))
	for ext := range m.entries {
		exts = append(exts, ext)
	}
	sort.Strings(exts)
	var b strings.Builder
	for _, ext := range exts {
		mp := m.entries[ext]
		fmt.Fprintf(&b, ".%s \"%s\" \"%s\"\n", ext, string(mp.FileType[:]), string(mp.Creator[:]))
	}
	return []byte(b.String())
}
