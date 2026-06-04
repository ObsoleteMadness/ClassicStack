package config

import (
	"maps"

	"github.com/pelletier/go-toml/v2"
)

// ToTOML serialises the model to TOML bytes. Comments and the original key
// ordering of any source file are not preserved; callers warn operators
// before overwriting a hand-edited file.
func (m *Model) ToTOML() ([]byte, error) {
	return toml.Marshal(m)
}

// Clone returns a deep copy of the model so edits can be staged without
// mutating the live configuration. The map-valued sections (AFP/SMB
// volumes, IPXGW/NetBIOS slices) are copied element-by-element.
func (m *Model) Clone() *Model {
	if m == nil {
		return nil
	}
	cp := *m // shallow copy of all value fields

	cp.IPXGW.Bindings = cloneStrings(m.IPXGW.Bindings)
	cp.NetBIOS.Transports = cloneStrings(m.NetBIOS.Transports)

	cp.SMB.Volumes = cloneShareMap(m.SMB.Volumes)
	cp.AFP.Volumes = cloneVolumeMap(m.AFP.Volumes)

	return &cp
}

func cloneStrings(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func cloneShareMap(in map[string]ShareModel) map[string]ShareModel {
	if in == nil {
		return nil
	}
	out := make(map[string]ShareModel, len(in))
	maps.Copy(out, in)
	return out
}

func cloneVolumeMap(in map[string]VolumeModel) map[string]VolumeModel {
	if in == nil {
		return nil
	}
	out := make(map[string]VolumeModel, len(in))
	maps.Copy(out, in)
	return out
}
