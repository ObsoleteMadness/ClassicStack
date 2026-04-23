package afp

import (
	"fmt"
	"strings"
)

const (
	FSTypeLocalFS   = "local_fs"
	FSTypeMacGarden = "macgarden"
)

func NormalizeFSType(s string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(s))
	if v == "" {
		return FSTypeLocalFS, nil
	}
	switch v {
	case FSTypeLocalFS, FSTypeMacGarden:
		return v, nil
	default:
		return "", fmt.Errorf("invalid fs_type %q: want %q or %q", s, FSTypeLocalFS, FSTypeMacGarden)
	}
}

// VolumeConfig holds the configuration for a single AFP-shared volume.
type VolumeConfig struct {
	Name             string
	Path             string
	FSType           string
	Password         string
	ReadOnly         bool
	RebuildDesktopDB bool
	AppleDoubleMode  AppleDoubleMode // per-volume override; empty means inherit from AFPOptions
}

// ParseVolumeFlag parses an -afp-volume flag value of the form "Name:Path".
// The name may contain spaces; the first colon separates name from path.
// Example: "Mac Share:c:\mac" or "Mac Stuff:/media/mac/classic"
func ParseVolumeFlag(s string) (VolumeConfig, error) {
	idx := strings.Index(s, ":")
	if idx < 1 {
		return VolumeConfig{}, fmt.Errorf("invalid -afp-volume %q: want \"Name:Path\"", s)
	}
	name := s[:idx]
	path := s[idx+1:]
	if path == "" {
		return VolumeConfig{}, fmt.Errorf("invalid -afp-volume %q: path is empty", s)
	}
	return VolumeConfig{Name: name, Path: path, FSType: FSTypeLocalFS}, nil
}
