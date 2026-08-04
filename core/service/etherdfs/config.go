package etherdfs

import (
	"errors"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// DrivesKey is the repeated-section schema key for EtherDFS drives. Each instance
// is one drive (one host directory the EtherDFS service exports under a DOS drive
// letter); the codec writes them as repeated named sections (TOML
// `[[etherdfsdrives.E]]`, UCI `config drive 'E'`).
const DrivesKey = "EtherDFSDrives"

// ErrDriveNameRequired is returned by DriveSection.Validate when a configured
// drive carries no name (its DOS drive letter).
var ErrDriveNameRequired = errors.New("etherdfs: drive name is required")

// DriveSection is one EtherDFS drive's config — a flat, codec-friendly view of an
// fs.ShareSpec plus the DOS drive letter. It mirrors smb.ShareSection (same field
// shape, same options→Extra mapping) so EtherDFS and the other file services
// configure their exports the same way, minus the SMB-specific Description remark
// (EtherDFS has no share-comment surface on the wire).
type DriveSection struct {
	// DName is the DOS drive letter ("E", "F") and the per-instance section name.
	// Always set; the codec writes it as the named-section instance key.
	DName string `toml:"name" display:"Drive letter" desc:"The DOS drive letter (A–Z) this export is addressed by; EtherDFS clients map a local letter to it." example:"E"`
	// FSType selects the FileSystem factory ("local_fs", "memfs", …).
	FSType string `toml:"fs_type,omitempty" display:"Filesystem type" desc:"Storage backend (local_fs, memfs, …)." widget:"fs_type" example:"local_fs"`
	// ForkBackend selects the fork engine ("appledouble"|"ads"|"xattr"|"native"|"auto").
	ForkBackend string `toml:"fork_backend,omitempty" display:"Fork backend" desc:"How resource forks / Finder info are stored (appledouble · ads · xattr · native · auto)." widget:"fork_backend" example:"ads"`
	// FilenameCodec selects the wire↔store name codec.
	FilenameCodec string `toml:"filename_codec,omitempty" display:"Filename codec" desc:"Wire↔store filename translation. Empty = default." widget:"filename_codec" example:"cp437-utf8"`
	// Metastore selects the CNID/shortname store kind ("mem" default).
	Metastore string `toml:"metastore,omitempty" display:"Metastore" desc:"Where IDs/short-name mappings persist (mem default; sqlite for a durable store)." widget:"metastore" example:"sqlite"`
	// MetaBackend selects the share's MetaEngine (derived names, CNIDs, DOS
	// attributes RO/HID/SYS/ARCH): "metastore"|"xattr"|"ads" (empty = per-platform
	// default). EtherDFS serves these to DOS clients. See fs.ShareSpec.MetaBackend.
	MetaBackend string `toml:"meta_backend,omitempty" display:"Meta backend" desc:"Where derived names, CNIDs, and DOS attributes live (metastore · xattr · ads). Empty = platform default." widget:"meta_backend" example:"metastore"`
	// Path is the backend location (host directory for local_fs, …).
	Path string `toml:"path,omitempty" display:"Path" desc:"Host directory backing this drive." example:"/srv/dos/e"`
	// ReadOnly makes the whole drive read-only (drive-wide, not per-user).
	ReadOnly bool `toml:"read_only,omitempty" display:"Read-only" desc:"Export the whole drive read-only."`
	// AllowedUsers is retained for ShareSpec shape parity with AFP/SMB but is unused
	// on the wire — EtherDFS has no login. The web UI hides this field.
	AllowedUsers []string `toml:"allowed_users,omitempty" display:"Allowed users" desc:"Unused: EtherDFS has no user authentication."`
	// Options carries backend-specific params as "key=value" entries → ShareSpec.Extra.
	Options []string `toml:"options,omitempty" display:"Options" desc:"Backend-specific key=value parameters."`
}

// compile-time assertions: *DriveSection is a NamedSection and a SecretMasker.
var (
	_ config.Section      = (*DriveSection)(nil)
	_ config.NamedSection = (*DriveSection)(nil)
	_ config.SecretMasker = (*DriveSection)(nil)
)

// Key returns the shared repeated-section schema key.
func (d *DriveSection) Key() string { return DrivesKey }

// InstanceName returns the per-drive instance name (the section name the codec writes).
func (d *DriveSection) InstanceName() string { return d.DName }

// HostPath returns the drive's backing host directory (config.HostPathProvider),
// for the §10e host watcher; empty for a synthetic backend with no host tree.
func (d *DriveSection) HostPath() string { return d.Path }

// Clone returns a deep copy. The two slices are copied so staging never aliases
// the live instance's backing arrays.
func (d *DriveSection) Clone() config.Section {
	cp := *d
	cp.AllowedUsers = append([]string(nil), d.AllowedUsers...)
	cp.Options = append([]string(nil), d.Options...)
	return &cp
}

// MaskedClone returns a deep copy with secret Options redacted (config.SecretMasker),
// per the fs_type's fs.Param schema. Mirrors smb.ShareSection.
func (d *DriveSection) MaskedClone() config.Section {
	cp := d.Clone().(*DriveSection)
	cp.Options = fs.MaskSecretOptions(cp.FSType, cp.Options, config.RedactedSecret)
	return cp
}

// Unmask returns a deep copy in which any secret Option still holding the
// redaction sentinel is restored from prev (config.SecretMasker).
func (d *DriveSection) Unmask(prev config.Section) config.Section {
	cp := d.Clone().(*DriveSection)
	var prior []string
	if pv, ok := prev.(*DriveSection); ok {
		prior = pv.Options
	}
	cp.Options = fs.UnmaskSecretOptions(cp.FSType, cp.Options, prior, config.RedactedSecret)
	return cp
}

// Validate checks the section in isolation. A drive must have a name; the
// fs_type×fork×codec triple and required backend params are validated later by
// share.Build, so they are not re-checked here.
func (d *DriveSection) Validate() error {
	if strings.TrimSpace(d.DName) == "" {
		return ErrDriveNameRequired
	}
	return nil
}

// fsSpec maps the section to an fs.ShareSpec (the storage-seam half). Options
// "key=value" entries become Extra entries; a bare "" key is dropped.
func (d *DriveSection) fsSpec() fs.ShareSpec {
	spec := fs.ShareSpec{
		Name:          d.DName,
		FSType:        d.FSType,
		ForkBackend:   d.ForkBackend,
		FilenameCodec: d.FilenameCodec,
		Metastore:     d.Metastore,
		MetaBackend:   d.MetaBackend,
		Path:          d.Path,
		ReadOnly:      d.ReadOnly,
		AllowedUsers:  append([]string(nil), d.AllowedUsers...),
	}
	if len(d.Options) > 0 {
		extra := make(map[string]any, len(d.Options))
		for _, opt := range d.Options {
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

// Spec maps the section to the EtherDFS DriveSpec the service builds a Drive from.
func (d *DriveSection) Spec() DriveSpec {
	return DriveSpec{Name: d.DName, Share: d.fsSpec()}
}

// SpecsFromModel resolves every EtherDFS drive instance in the model to its
// DriveSpec, in registration order. A model with no drive section yields no specs
// (the service runs with zero drives — the registry default).
func SpecsFromModel(m *config.Model) []DriveSpec {
	if m == nil {
		return nil
	}
	list := m.List(DrivesKey)
	if len(list) == 0 {
		return nil
	}
	out := make([]DriveSpec, 0, len(list))
	for _, sec := range list {
		if ds, ok := sec.(*DriveSection); ok {
			out = append(out, ds.Spec())
		}
	}
	return out
}

// RegisterDrives installs the EtherDFS drive repeated-section schema so codecs
// round-trip each configured drive as a named section. Kept out of an init() so a
// build excluding EtherDFS excludes the section too (called from the compose
// registry wiring, like smb.RegisterShares).
func RegisterDrives() {
	config.Register(config.SectionSchema{
		Key:      DrivesKey,
		Repeated: true,
		New:      func() config.Section { return &DriveSection{} },
		Validate: func(s config.Section) error {
			if ds, ok := s.(*DriveSection); ok {
				return ds.Validate()
			}
			return nil
		},
	})
}
