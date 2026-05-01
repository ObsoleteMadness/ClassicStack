//go:build afp || all

package afp

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ParseAppleDoubleMode parses an "appledouble_mode" config value.
func ParseAppleDoubleMode(value string) (AppleDoubleMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "modern", string(AppleDoubleModeModern):
		return AppleDoubleModeModern, nil
	case "legacy", string(AppleDoubleModeLegacy):
		return AppleDoubleModeLegacy, nil
	default:
		return "", fmt.Errorf("appledouble_mode must be modern or legacy, got %q", value)
	}
}

// DefaultMacGardenVolumePath derives a filesystem-safe default path for a
// MacGarden-backed volume that did not specify one.
func DefaultMacGardenVolumePath(name string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_':
			return r
		case r == ' ':
			return '_'
		default:
			return -1
		}
	}, strings.TrimSpace(name))
	if safe == "" {
		safe = "MacGarden"
	}
	return filepath.Join(".macgarden", safe)
}
