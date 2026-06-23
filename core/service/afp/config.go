package afp

import (
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// VolumesKey is the repeated-section schema key for AFP volumes. Each instance is
// one volume (one share the AFP service exports); the codec writes them as repeated
// named sections (UCI `config volume 'public'`, TOML `[[afpvolumes.volume]]`).
const VolumesKey = "AFPVolumes"

// VolumeSection is one AFP volume's config — a flat, codec-friendly view of an
// fs.ShareSpec plus the volume display name. It is a NamedSection (one instance per
// share); the service builds a Volume per instance via SpecFor.
//
// Backend-specific params (the fs.ShareSpec.Extra carrier — e.g. ftp "url",
// hfs-image "partition") ride the Options list as "key=value" entries, because Extra
// is a map[string]any that a flat reflect-marshalled section cannot hold directly.
// The near-universal Path is a typed field (it maps to the reserved ShareSpec.Path).
//
// It carries no secret fields of its own; a backend whose Param set marks a key
// Secret (a password in Options) is the UI's concern to mask, as ParamsFor reports.
type VolumeSection struct {
	// VName is the volume's display name and the per-instance section name. Always
	// set; the codec writes it as the named-section instance key.
	VName string `toml:"name"`
	// FSType selects the FileSystem factory ("local_fs", "memfs", …). Empty leaves
	// share.Build to apply its default.
	FSType string `toml:"fs_type"`
	// ForkBackend selects the fork engine ("appledouble"|"ads"|"xattr"|"native"|"auto").
	ForkBackend string `toml:"fork_backend"`
	// FilenameCodec selects the wire↔store name codec ("macroman-utf8"|…).
	FilenameCodec string `toml:"filename_codec"`
	// NameEngine selects the short/medium name engine.
	NameEngine string `toml:"name_engine"`
	// Metastore selects the CNID/shortname store kind ("mem" default, "sqlite" tagged).
	Metastore string `toml:"metastore"`
	// DOSAttrBackend selects how DOS attributes that the host cannot represent are
	// persisted: auto|metastore|sidecar|native|xattr (empty = auto). AFP does not
	// serve DOS attributes itself, but a same-host-path SMB/EtherDFS share does, so
	// the volume config carries it for consistency. See fs.ShareSpec.DOSAttrBackend.
	DOSAttrBackend string `toml:"dos_attr_backend"`
	// Path is the backend location (the host directory for local_fs, the image file
	// for hfs-image, …). Maps to the typed fs.ShareSpec.Path.
	Path string `toml:"path"`
	// ReadOnly makes the whole volume read-only (share-wide, not per-user).
	ReadOnly bool `toml:"read_only"`
	// AllowedUsers is the access allow-list (empty = guest/world). Not secret;
	// protocol-layer policy lifted into the share's Permissions.
	AllowedUsers []string `toml:"allowed_users"`
	// Options carries backend-specific params as "key=value" entries → ShareSpec.Extra.
	Options []string `toml:"options"`
	// ExtMapPath names a Netatalk-style extension→type/creator map file the volume
	// consults to DEFAULT Finder type/creator for files with no stored classic
	// metadata. Empty = no defaulting (a file without stored Finder info reads as 32
	// zero bytes). The file is read at the cmd/compose edge (core does no file I/O for
	// config) and parsed via afp.ParseExtensionMap.
	ExtMapPath string `toml:"extmap_path"`
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

// HostPath returns the volume's backing host directory (config.HostPathProvider), for
// the §10e host watcher; empty for a synthetic backend with no host tree.
func (s *VolumeSection) HostPath() string { return s.Path }

// Clone returns a deep copy. The two slices are copied so staging never aliases the
// live instance's backing arrays.
func (s *VolumeSection) Clone() config.Section {
	cp := *s
	cp.AllowedUsers = append([]string(nil), s.AllowedUsers...)
	cp.Options = append([]string(nil), s.Options...)
	return &cp
}

// MaskedClone returns a deep copy with secret Options redacted (config.SecretMasker).
// The fs_type's fs.Param schema names which option keys are Secret (a backend
// password); their values become config.RedactedSecret so a config served to a UI
// never carries the cleartext secret.
func (s *VolumeSection) MaskedClone() config.Section {
	cp := s.Clone().(*VolumeSection)
	cp.Options = fs.MaskSecretOptions(cp.FSType, cp.Options, config.RedactedSecret)
	return cp
}

// Unmask returns a deep copy in which any secret Option still holding the redaction
// sentinel is restored from prev (config.SecretMasker), so a UI round-tripping the
// masked config does not overwrite a stored password with the placeholder. prev that
// is not a *VolumeSection (or nil) leaves nothing to restore — a sentinel-valued
// secret is then dropped rather than persisted.
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
// share.Build (which has the registry of FS factories), so they are not re-checked
// here — keeping this a cheap, registry-free check.
func (s *VolumeSection) Validate() error {
	if strings.TrimSpace(s.VName) == "" {
		return ErrVolumeNameRequired
	}
	return nil
}

// Spec maps the section to an fs.ShareSpec the AFP service builds a Volume from.
// Options "key=value" entries become Extra map entries (last value wins for a
// repeated key); a malformed entry (no '=') contributes a present-but-empty value,
// so a bare "flag" Option reads as Extra["flag"] == "". The allow-list and path are
// copied verbatim.
func (s *VolumeSection) Spec() fs.ShareSpec {
	spec := fs.ShareSpec{
		Name:           s.VName,
		FSType:         s.FSType,
		ForkBackend:    s.ForkBackend,
		FilenameCodec:  s.FilenameCodec,
		NameEngine:     s.NameEngine,
		Metastore:      s.Metastore,
		DOSAttrBackend: s.DOSAttrBackend,
		Path:           s.Path,
		ReadOnly:       s.ReadOnly,
		AllowedUsers:   append([]string(nil), s.AllowedUsers...),
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

// SpecsFromModel resolves every AFP volume instance in the model to its fs.ShareSpec,
// in registration order. A model with no AFP volume section yields no specs (the
// service runs with zero volumes — the registry default).
func SpecsFromModel(m *config.Model) []fs.ShareSpec {
	if m == nil {
		return nil
	}
	list := m.List(VolumesKey)
	if len(list) == 0 {
		return nil
	}
	out := make([]fs.ShareSpec, 0, len(list))
	for _, sec := range list {
		if vs, ok := sec.(*VolumeSection); ok {
			out = append(out, vs.Spec())
		}
	}
	return out
}

// VolumesFromModel returns the configured AFP volume SECTIONS (not specs), in
// registration order, so a caller that needs section-level fields the fs.ShareSpec
// does not carry — chiefly ExtMapPath — can read them. Returns nil when none.
func VolumesFromModel(m *config.Model) []*VolumeSection {
	if m == nil {
		return nil
	}
	list := m.List(VolumesKey)
	out := make([]*VolumeSection, 0, len(list))
	for _, sec := range list {
		if vs, ok := sec.(*VolumeSection); ok {
			out = append(out, vs)
		}
	}
	return out
}

// RegisterVolumes installs the AFP volume repeated-section schema so codecs round-trip
// each volume as a named section. Kept out of an init() and called from the compose
// registry wiring, so a build that excludes AFP excludes the section too (mirrors
// auth.Register).
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
