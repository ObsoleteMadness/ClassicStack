package afp

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/knadh/koanf/v2"
)

// LoadVolumes parses the [Volumes.*] subtree of a koanf instance and
// returns the resulting VolumeConfig slice. configDir is reserved for
// future use (resolving relative paths against the config file location)
// and may be passed as "".
//
// Any unknown keys inside a volume section are ignored, leaving room for
// backend-specific extensions without core-package changes.
func LoadVolumes(k *koanf.Koanf, configDir string) (vols []VolumeConfig, decomposedFilenames *bool, cnidBackend string, err error) {
	_ = configDir
	if k == nil {
		return nil, nil, "", nil
	}

	var (
		seenDecomposed  bool
		decomposed      bool
		seenCNIDBackend bool
		cnid            string
	)

	for _, key := range k.MapKeys("Volumes") {
		base := "Volumes." + key
		section := base
		name := stringValue(k, base+".name", key)

		vol := VolumeConfig{Name: name, FSType: FSTypeLocalFS}
		if k.Exists(base + ".fs_type") {
			fsType, parseErr := NormalizeFSType(stringValue(k, base+".fs_type", FSTypeLocalFS))
			if parseErr != nil {
				return nil, nil, "", fmt.Errorf("[%s] %w", section, parseErr)
			}
			vol.FSType = fsType
		}

		pathVal := stringValue(k, base+".path", "")
		if strings.TrimSpace(pathVal) == "" {
			if vol.FSType == FSTypeMacGarden {
				pathVal = DefaultMacGardenVolumePath(name)
			} else {
				return nil, nil, "", fmt.Errorf("[%s] path is required", section)
			}
		}
		vol.Path = pathVal

		if k.Exists(base + ".rebuild_desktop_db") {
			vol.RebuildDesktopDB = k.Bool(base + ".rebuild_desktop_db")
		}
		if k.Exists(base + ".read_only") {
			vol.ReadOnly = k.Bool(base + ".read_only")
		}

		if k.Exists(base + ".use_decomposed_names") {
			v := k.Bool(base + ".use_decomposed_names")
			if seenDecomposed && v != decomposed {
				return nil, nil, "", fmt.Errorf("[%s] use_decomposed_names conflicts with another volume section", section)
			}
			decomposed = v
			seenDecomposed = true
		}

		if k.Exists(base + ".cnid_backend") {
			backendVal := stringValue(k, base+".cnid_backend", "")
			if backendVal == "" {
				continue
			}
			if seenCNIDBackend && !strings.EqualFold(backendVal, cnid) {
				return nil, nil, "", fmt.Errorf("[%s] cnid_backend conflicts with another volume section", section)
			}
			cnid = backendVal
			seenCNIDBackend = true
		}

		if k.Exists(base + ".fork_backend") {
			fb := strings.ToLower(stringValue(k, base+".fork_backend", ""))
			if fb != "" && fb != "appledouble" {
				return nil, nil, "", fmt.Errorf("[%s] fork_backend must be blank or AppleDouble", section)
			}
		}

		if k.Exists(base + ".appledouble_mode") {
			modeVal := stringValue(k, base+".appledouble_mode", "")
			parsedMode, parseErr := ParseAppleDoubleMode(modeVal)
			if parseErr != nil {
				return nil, nil, "", fmt.Errorf("[%s] %w", section, parseErr)
			}
			vol.AppleDoubleMode = parsedMode
		}

		vols = append(vols, vol)
	}

	if seenDecomposed {
		decomposedFilenames = &decomposed
	}
	if seenCNIDBackend {
		cnidBackend = cnid
	}
	return vols, decomposedFilenames, cnidBackend, nil
}

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

func stringValue(k *koanf.Koanf, path, def string) string {
	if !k.Exists(path) {
		return def
	}
	v := strings.TrimSpace(k.String(path))
	if v == "" {
		return def
	}
	return v
}
