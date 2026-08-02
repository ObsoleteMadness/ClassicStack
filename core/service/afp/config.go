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
	VName string `toml:"name" display:"Volume name" desc:"Display name shown to AFP clients."`
	// FSType selects the FileSystem factory ("local_fs", "memfs", …). Empty leaves
	// share.Build to apply its default.
	FSType string `toml:"fs_type,omitempty" display:"Filesystem type" desc:"Storage backend (local_fs, memfs, …)." widget:"fs_type"`
	// ForkBackend selects the fork engine ("appledouble"|"ads"|"xattr"|"native"|"auto").
	ForkBackend string `toml:"fork_backend,omitempty" display:"Fork backend" desc:"How resource forks / Finder info are stored (appledouble · ads · xattr · native · auto)." widget:"fork_backend"`
	// FilenameCodec selects the wire↔store name codec ("macroman-utf8"|…).
	FilenameCodec string `toml:"filename_codec,omitempty" display:"Filename codec" desc:"Wire↔store filename translation. Empty = default." widget:"filename_codec"`
	// Metastore selects the CNID/shortname store kind ("mem" default, "sqlite" tagged).
	Metastore string `toml:"metastore,omitempty" display:"Metastore" desc:"Where IDs/short-name mappings persist (mem default; sqlite for a durable store)." widget:"metastore"`
	// MetaBackend selects the share's MetaEngine (derived names, CNIDs, DOS
	// attributes/dates): "metastore"|"xattr"|"ads" (empty = per-platform default).
	// AFP does not serve DOS attributes itself, but a same-host-path SMB/EtherDFS
	// share does, so the volume config carries it for consistency. See
	// fs.ShareSpec.MetaBackend.
	MetaBackend string `toml:"meta_backend,omitempty" display:"Meta backend" desc:"Where derived names, CNIDs, and DOS attributes live (metastore · xattr · ads). Empty = platform default." widget:"meta_backend"`
	// Path is the backend location (the host directory for local_fs, the image file
	// for hfs-image, …). Maps to the typed fs.ShareSpec.Path.
	Path string `toml:"path,omitempty" display:"Path" desc:"Host directory backing this share."`
	// ReadOnly makes the whole volume read-only (share-wide, not per-user).
	ReadOnly bool `toml:"read_only,omitempty" display:"Read-only" desc:"Export the whole share read-only."`
	// AllowedUsers is the access allow-list (empty = guest/world). Not secret;
	// protocol-layer policy lifted into the share's Permissions.
	AllowedUsers []string `toml:"allowed_users,omitempty" display:"Allowed users" desc:"Access allow-list. Guest checked alone = world access; otherwise only the selected accounts." widget:"allowed_users"`
	// Options carries backend-specific params as "key=value" entries → ShareSpec.Extra.
	Options []string `toml:"options,omitempty" display:"Options" desc:"Backend-specific key=value parameters."`
	// ExtMapPath names a Netatalk-style extension→type/creator map file the volume
	// consults to DEFAULT Finder type/creator for files with no stored classic
	// metadata. Empty = no defaulting (a file without stored Finder info reads as 32
	// zero bytes). The file is read at the cmd/compose edge (core does no file I/O for
	// config) and parsed via afp.ParseExtensionMap.
	ExtMapPath string `toml:"extmap_path,omitempty" display:"Extension map file" desc:"Netatalk-style type/creator map for files with no stored Finder info."`
	// SizeLimitMB is the volume size REPORTED to AFP clients, in MiB (netatalk's
	// volsizelimit). 0 = the classic-friendly 512 MiB default. Classic clients
	// derive their HFS allocation-block size from the reported size with 16-bit
	// block math, so this sets the Finder's per-file "size on disk" granularity
	// (512 MiB → 8 KiB blocks; the 2 GiB wire cap → 32 KiB). Presentation only —
	// it does not limit what the host stores.
	SizeLimitMB int64 `toml:"size_limit,omitempty" display:"Size limit (MiB)" desc:"Volume size reported to AFP clients in MiB (0 = 512 MiB classic default). Presentation only."`
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
		DisplayName: "AFP volumes",
		Description: "Repeated AFP volume exports (name, filesystem backend, path).",
	})
}
