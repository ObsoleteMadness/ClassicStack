//go:build afp || all

package afp

import (
	"fmt"
	"strings"
)

const (
	FSTypeLocalFS   = "local_fs"
	FSTypeMacGarden = "macgarden"
)

// Config is AFP's user-facing configuration. It is populated by koanf
// (or any source) before being handed to NewService. Runtime objects
// like transports, FileSystem, and ExtensionMap are constructor args,
// not config.
type Config struct {
	Enabled bool   `koanf:"enabled"`
	Name    string `koanf:"name"`
	Zone    string `koanf:"zone"`
	// Protocols is a comma-separated list: "tcp", "ddp", or "tcp,ddp".
	Protocols string `koanf:"protocols"`
	// Binding is the AFP-over-TCP listen address (e.g. ":548").
	Binding string `koanf:"binding"`
	// ExtensionMap is the path to a netatalk-style type/creator file.
	// Resolved by the caller against the config-file directory if relative.
	ExtensionMap        string `koanf:"extension_map"`
	UseDecomposedNames  bool   `koanf:"use_decomposed_names"`
	CNIDBackend         string `koanf:"cnid_backend"`
	DesktopBackend      string `koanf:"desktop_backend"`
	AppleDoubleMode     string `koanf:"appledouble_mode"`
	PersistentVolumeIDs bool   `koanf:"persistent_volume_ids"`

	// Volumes is a name-keyed map; the key is used as the default volume
	// Name when the section omits one.
	Volumes map[string]VolumeConfig `koanf:"volumes"`
}

// DefaultConfig returns AFP's built-in defaults. These are also used as
// the seed values for koanf unmarshalling so unset keys keep their
// defaults rather than being zeroed.
func DefaultConfig() Config {
	return Config{
		Enabled:             true,
		Name:                "Go File Server",
		Protocols:           "tcp,ddp",
		Binding:             ":548",
		UseDecomposedNames:  true,
		CNIDBackend:         "sqlite",
		DesktopBackend:      "sqlite",
		AppleDoubleMode:     string(defaultAppleDoubleMode),
		PersistentVolumeIDs: true,
	}
}

// Validate checks the config for logical consistency. Syntactic decoding
// errors are caught earlier by the unmarshaller; this method enforces
// rules that the type system can't express.
func (c *Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("AFP.name must not be empty")
	}
	for _, p := range strings.Split(c.Protocols, ",") {
		p = strings.TrimSpace(strings.ToLower(p))
		switch p {
		case "", "tcp", "ddp":
		default:
			return fmt.Errorf("AFP.protocols entry %q must be tcp or ddp", p)
		}
	}
	if _, err := ParseAppleDoubleMode(c.AppleDoubleMode); err != nil {
		return fmt.Errorf("AFP.%w", err)
	}
	for key, v := range c.Volumes {
		section := "AFP.volumes." + key
		fsType, err := NormalizeFSType(v.FSType)
		if err != nil {
			return fmt.Errorf("[%s] %w", section, err)
		}
		if strings.TrimSpace(v.Path) == "" && fsType != FSTypeMacGarden {
			return fmt.Errorf("[%s] path is required", section)
		}
		if v.AppleDoubleMode != "" {
			if _, err := ParseAppleDoubleMode(string(v.AppleDoubleMode)); err != nil {
				return fmt.Errorf("[%s] %w", section, err)
			}
		}
	}
	return nil
}

// ResolvedVolumes returns Volumes as a flat slice, with map keys folded
// into Name where the section did not set one and FSType normalized.
// MacGarden volumes without a path get a default derived from Name.
func (c *Config) ResolvedVolumes() ([]VolumeConfig, error) {
	out := make([]VolumeConfig, 0, len(c.Volumes))
	for key, v := range c.Volumes {
		if strings.TrimSpace(v.Name) == "" {
			v.Name = key
		}
		fsType, err := NormalizeFSType(v.FSType)
		if err != nil {
			return nil, fmt.Errorf("[AFP.volumes.%s] %w", key, err)
		}
		v.FSType = fsType
		if strings.TrimSpace(v.Path) == "" && fsType == FSTypeMacGarden {
			v.Path = DefaultMacGardenVolumePath(v.Name)
		}
		if v.AppleDoubleMode != "" {
			mode, err := ParseAppleDoubleMode(string(v.AppleDoubleMode))
			if err != nil {
				return nil, fmt.Errorf("[AFP.volumes.%s] %w", key, err)
			}
			v.AppleDoubleMode = mode
		}
		out = append(out, v)
	}
	return out, nil
}

// VolumeConfig holds the configuration for a single AFP-shared volume.
type VolumeConfig struct {
	Name             string          `koanf:"name"`
	Path             string          `koanf:"path"`
	FSType           string          `koanf:"fs_type"`
	Password         string          `koanf:"password"`
	ReadOnly         bool            `koanf:"read_only"`
	RebuildDesktopDB bool            `koanf:"rebuild_desktop_db"`
	AppleDoubleMode  AppleDoubleMode `koanf:"appledouble_mode"`
}

func NormalizeFSType(s string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(s))
	if v == "" {
		return FSTypeLocalFS, nil
	}
	fsRegistryMu.RLock()
	_, ok := fsRegistry[v]
	fsRegistryMu.RUnlock()
	if !ok {
		return "", fmt.Errorf("invalid fs_type %q (registered: %v)", s, registeredFSNames())
	}
	return v, nil
}

// ParseVolumeFlag parses an -afp-volume flag value of the form "Name:Path".
// The name may contain spaces; the first colon separates name from path.
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
