package ncp

import (
	"errors"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// VolumesKey is the repeated-section schema key for NCP volumes. Each instance is
// one NetWare volume (one tree the NCP service exports, e.g. SYS:); the codec
// writes them as repeated named sections (UCI `config volume 'sys'`, TOML
// `[[ncpvolumes]]`).
const VolumesKey = "NCPVolumes"

// ErrVolumeNameRequired is returned by VolumeSection.Validate when a configured
// volume carries no name.
var ErrVolumeNameRequired = errors.New("ncp: volume name is required")

// VolumeSection is one NCP volume's config — a flat, codec-friendly view of an
// fs.ShareSpec plus the NetWare volume name. It is a NamedSection (one instance
// per volume); the service builds a Volume per instance via Spec.
//
// It mirrors smb.ShareSection / afp.VolumeSection (same field shape, same
// options→Extra mapping) so all three file services configure shares the same
// way. Backend-specific params ride the Options list as "key=value" entries (Extra
// is a map a flat reflect-marshalled section cannot hold directly).
type VolumeSection struct {
	// VName is the NetWare volume name and the per-instance section name. Always
	// set; the codec writes it as the named-section instance key. NetWare volume
	// names are matched case-insensitively and conventionally upper-cased (SYS,
	// VOL1) — the trailing colon clients use (SYS:) is not part of the name.
	VName string `toml:"name" display:"Volume name" desc:"NetWare volume name (SYS, VOL1, …)."`
	// FSType selects the FileSystem factory ("local_fs", "memfs", …).
	FSType string `toml:"fs_type,omitempty" display:"Filesystem type" desc:"Storage backend (local_fs, memfs, …)." widget:"fs_type"`
	// ForkBackend selects the fork engine ("appledouble"|"ads"|"xattr"|"native"|"auto").
	ForkBackend string `toml:"fork_backend,omitempty" display:"Fork backend" desc:"How resource forks / Finder info are stored (appledouble · ads · xattr · native · auto)." widget:"fork_backend"`
	// FilenameCodec selects the wire↔store name codec.
	FilenameCodec string `toml:"filename_codec,omitempty" display:"Filename codec" desc:"Wire↔store filename translation. Empty = default." widget:"filename_codec"`
	// Metastore selects the CNID/shortname store kind ("mem" default).
	Metastore string `toml:"metastore,omitempty" display:"Metastore" desc:"Where IDs/short-name mappings persist (mem default; sqlite for a durable store)." widget:"metastore"`
	// MetaBackend selects the share's MetaEngine (derived names, CNIDs, DOS
	// attributes/dates): "metastore"|"xattr"|"ads" (empty = per-platform default).
	// See fs.ShareSpec.MetaBackend.
	MetaBackend string `toml:"meta_backend,omitempty" display:"Meta backend" desc:"Where derived names, CNIDs, and DOS attributes live (metastore · xattr · ads). Empty = platform default." widget:"meta_backend"`
	// Path is the backend location (host directory for local_fs, …).
	Path string `toml:"path,omitempty" display:"Path" desc:"Host directory backing this volume."`
	// ReadOnly makes the whole volume read-only (volume-wide, not per-user).
	ReadOnly bool `toml:"read_only,omitempty" display:"Read-only" desc:"Export the whole volume read-only."`
	// AllowedUsers is the access allow-list (empty = guest/world). Not secret.
	AllowedUsers []string `toml:"allowed_users,omitempty" display:"Allowed users" desc:"Access allow-list. Guest checked alone = world access; otherwise only the selected accounts." widget:"allowed_users"`
	// Options carries backend-specific params as "key=value" entries → ShareSpec.Extra.
	Options []string `toml:"options,omitempty" display:"Options" desc:"Backend-specific key=value parameters."`
}

// compile-time assertions: *VolumeSection is a NamedSection and a SecretMasker.
var (
	_ config.Section      = (*VolumeSection)(nil)
	_ config.NamedSection = (*VolumeSection)(nil)
	_ config.SecretMasker = (*VolumeSection)(nil)
)

// Key returns the shared repeated-section schema key.
func (s *VolumeSection) Key() string { return VolumesKey }

// InstanceName returns the per-volume instance name (the section name the codec writes).
func (s *VolumeSection) InstanceName() string { return s.VName }

// HostPath returns the volume's backing host directory (config.HostPathProvider),
// for the §10e host watcher; empty for a synthetic backend with no host tree.
func (s *VolumeSection) HostPath() string { return s.Path }

// Clone returns a deep copy. The two slices are copied so staging never aliases
// the live instance's backing arrays.
func (s *VolumeSection) Clone() config.Section {
	cp := *s
	cp.AllowedUsers = append([]string(nil), s.AllowedUsers...)
	cp.Options = append([]string(nil), s.Options...)
	return &cp
}

// MaskedClone returns a deep copy with secret Options redacted (config.SecretMasker).
func (s *VolumeSection) MaskedClone() config.Section {
	cp := s.Clone().(*VolumeSection)
	cp.Options = fs.MaskSecretOptions(cp.FSType, cp.Options, config.RedactedSecret)
	return cp
}

// Unmask returns a deep copy in which any secret Option still holding the
// redaction sentinel is restored from prev (config.SecretMasker).
func (s *VolumeSection) Unmask(prev config.Section) config.Section {
	cp := s.Clone().(*VolumeSection)
	var prior []string
	if pv, ok := prev.(*VolumeSection); ok {
		prior = pv.Options
	}
	cp.Options = fs.UnmaskSecretOptions(cp.FSType, cp.Options, prior, config.RedactedSecret)
	return cp
}

// Validate checks the section in isolation. A volume must have a name; the
// fs_type×fork×codec triple and required backend params are validated later by
// share.Build.
func (s *VolumeSection) Validate() error {
	if strings.TrimSpace(s.VName) == "" {
		return ErrVolumeNameRequired
	}
	return nil
}

// fsSpec maps the section to an fs.ShareSpec (the storage-seam half). Options
// "key=value" entries become Extra entries.
func (s *VolumeSection) fsSpec() fs.ShareSpec {
	spec := fs.ShareSpec{
		Name:          s.VName,
		FSType:        s.FSType,
		ForkBackend:   s.ForkBackend,
		FilenameCodec: s.FilenameCodec,
		Metastore:     s.Metastore,
		MetaBackend:   s.MetaBackend,
		Path:          s.Path,
		ReadOnly:      s.ReadOnly,
		AllowedUsers:  append([]string(nil), s.AllowedUsers...),
	}
	if len(s.Options) > 0 {
		extra := make(map[string]any, len(s.Options))
		for _, opt := range s.Options {
			k, v, _ := strings.Cut(opt, "=")
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			extra[k] = strings.TrimSpace(v)
		}
		if len(extra) > 0 {
			spec.Extra = extra
		}
	}
	return spec
}

// Spec maps the section to the NCP VolumeSpec the service builds a Volume from.
func (s *VolumeSection) Spec() VolumeSpec {
	return VolumeSpec{Name: s.VName, Share: s.fsSpec()}
}

// SpecsFromModel resolves every NCP volume instance in the model to its
// VolumeSpec, in registration order. A model with no NCP volume section yields no
// specs (the service runs with zero volumes — the registry default).
func SpecsFromModel(m *config.Model) []VolumeSpec {
	if m == nil {
		return nil
	}
	list := m.List(VolumesKey)
	if len(list) == 0 {
		return nil
	}
	out := make([]VolumeSpec, 0, len(list))
	for _, sec := range list {
		if vs, ok := sec.(*VolumeSection); ok {
			out = append(out, vs.Spec())
		}
	}
	return out
}

// RegisterVolumes installs the NCP volume repeated-section schema so codecs
// round-trip each volume as a named section. Called from the compose registry
// wiring (kept out of an init() so a build that excludes NCP excludes the section
// too).
func RegisterVolumes() {
	config.Register(config.SectionSchema{
		Key:      VolumesKey,
		Repeated: true,
		New:      func() config.Section { return &VolumeSection{} },
		Validate: func(s config.Section) error {
			if vs, ok := s.(*VolumeSection); ok {
				return vs.Validate()
			}
			return nil
		},
	})
}
