package smb

import (
	"errors"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// SharesKey is the repeated-section schema key for SMB shares. Each instance is one
// share (one tree the SMB service exports); the codec writes them as repeated named
// sections (UCI `config share 'public'`, TOML `[[smbshares.share]]`).
const SharesKey = "SMBShares"

// ErrShareNameRequired is returned by ShareSection.Validate when a configured share
// carries no tree name.
var ErrShareNameRequired = errors.New("smb: share name is required")

// ShareSection is one SMB share's config — a flat, codec-friendly view of an
// fs.ShareSpec plus the share tree name and the NetShareEnum remark. It is a
// NamedSection (one instance per share); the service builds a Share per instance via
// Spec.
//
// It mirrors afp.VolumeSection (same field shape, same options→Extra mapping) so the
// two file services configure shares the same way, with one SMB-specific addition:
// Description, the operator remark NetShareEnum reports (the AFP volume has no
// equivalent). Backend-specific params ride the Options list as "key=value" entries
// (Extra is a map a flat reflect-marshalled section cannot hold directly).
type ShareSection struct {
	// SName is the share's tree name and the per-instance section name. Always set;
	// the codec writes it as the named-section instance key. SMB tree names are
	// matched case-insensitively at tree-connect.
	SName string `toml:"name" display:"Share name" desc:"Display name shown to SMB clients." example:"PUBLIC"`
	// Description is the human remark NetShareEnum reports (the share comment).
	Description string `toml:"description,omitempty" display:"Description" desc:"Share comment shown by NetShareEnum." example:"Shared files"`
	// FSType selects the FileSystem factory ("local_fs", "memfs", …).
	FSType string `toml:"fs_type,omitempty" display:"Filesystem type" desc:"Storage backend (local_fs, memfs, …)." widget:"fs_type" example:"local_fs"`
	// ForkBackend selects the fork engine ("appledouble"|"ads"|"xattr"|"native"|"auto").
	ForkBackend string `toml:"fork_backend,omitempty" display:"Fork backend" desc:"How resource forks / Finder info are stored (appledouble · ads · xattr · native · auto)." widget:"fork_backend" example:"ads"`
	// FilenameCodec selects the wire↔store name codec.
	FilenameCodec string `toml:"filename_codec,omitempty" display:"Filename codec" desc:"Wire↔store filename translation. Empty = default (windows-safe)." widget:"filename_codec" example:"windows-safe"`
	// Metastore selects the CNID/shortname store kind ("mem" default).
	Metastore string `toml:"metastore,omitempty" display:"Metastore" desc:"Where IDs/short-name mappings persist (mem default; sqlite for a durable store)." widget:"metastore" example:"sqlite"`
	// MetaBackend selects the share's MetaEngine (derived names, CNIDs, DOS
	// attributes RO/HID/SYS/ARCH): "metastore"|"xattr"|"ads" (empty = per-platform
	// default). See fs.ShareSpec.MetaBackend.
	MetaBackend string `toml:"meta_backend,omitempty" display:"Meta backend" desc:"Where derived names, CNIDs, and DOS attributes live (metastore · xattr · ads). Empty = platform default." widget:"meta_backend" example:"metastore"`
	// Path is the backend location (host directory for local_fs, …).
	Path string `toml:"path,omitempty" display:"Path" desc:"Host directory backing this share." example:"/srv/smb/public"`
	// ReadOnly makes the whole share read-only (share-wide, not per-user).
	ReadOnly bool `toml:"read_only,omitempty" display:"Read-only" desc:"Export the whole share read-only."`
	// AllowedUsers is the access allow-list (empty = guest/world). Not secret.
	AllowedUsers []string `toml:"allowed_users,omitempty" display:"Allowed users" desc:"Access allow-list. Guest checked alone = world access; otherwise only the selected accounts." widget:"allowed_users"`
	// Options carries backend-specific params as "key=value" entries → ShareSpec.Extra.
	Options []string `toml:"options,omitempty" display:"Options" desc:"Backend-specific key=value parameters."`
}

// compile-time assertions: *ShareSection is a NamedSection and a SecretMasker.
var (
	_ config.Section      = (*ShareSection)(nil)
	_ config.NamedSection = (*ShareSection)(nil)
	_ config.SecretMasker = (*ShareSection)(nil)
)

// Key returns the shared repeated-section schema key.
func (s *ShareSection) Key() string { return SharesKey }

// InstanceName returns the per-share instance name (the section name the codec writes).
func (s *ShareSection) InstanceName() string { return s.SName }

// HostPath returns the share's backing host directory (config.HostPathProvider), for
// the §10e host watcher; empty for a synthetic backend with no host tree.
func (s *ShareSection) HostPath() string { return s.Path }

// Clone returns a deep copy. The two slices are copied so staging never aliases the
// live instance's backing arrays.
func (s *ShareSection) Clone() config.Section {
	cp := *s
	cp.AllowedUsers = append([]string(nil), s.AllowedUsers...)
	cp.Options = append([]string(nil), s.Options...)
	return &cp
}

// MaskedClone returns a deep copy with secret Options redacted (config.SecretMasker).
// The fs_type's fs.Param schema names which option keys are Secret (a backend
// password); their values become config.RedactedSecret so a config served to a UI
// never carries the cleartext secret. Mirrors afp.VolumeSection.
func (s *ShareSection) MaskedClone() config.Section {
	cp := s.Clone().(*ShareSection)
	cp.Options = fs.MaskSecretOptions(cp.FSType, cp.Options, config.RedactedSecret)
	return cp
}

// Unmask returns a deep copy in which any secret Option still holding the redaction
// sentinel is restored from prev (config.SecretMasker), so a UI round-tripping the
// masked config does not overwrite a stored password with the placeholder. prev that
// is not a *ShareSection (or nil) leaves nothing to restore.
func (s *ShareSection) Unmask(prev config.Section) config.Section {
	cp := s.Clone().(*ShareSection)
	var prior []string
	if pv, ok := prev.(*ShareSection); ok {
		prior = pv.Options
	}
	cp.Options = fs.UnmaskSecretOptions(cp.FSType, cp.Options, prior, config.RedactedSecret)
	return cp
}

// Validate checks the section in isolation. A share must have a name; the
// fs_type × fork × codec triple and required backend params are checked here
// so Save rejects an unbuildable share before it goes live.
func (s *ShareSection) Validate() error {
	if strings.TrimSpace(s.SName) == "" {
		return ErrShareNameRequired
	}
	return fs.ValidateSpec(s.fsSpec())
}

// fsSpec maps the section to an fs.ShareSpec (the storage-seam half). Options
// "key=value" entries become Extra entries; a malformed entry (no '=') with a
// non-empty key reads as a present-but-empty value, a bare "" key is dropped.
//
// An unset FilenameCodec defaults to "windows-safe" rather than falling
// through to fs.withDefaults' generic "identity" — every SMB client is a
// DOS/Windows redirector, which cannot represent an NTFS/FAT-reserved
// character (or a control character) in a filename under any wire charset it
// speaks, so the share's own storage escaping should already assume that
// (core/fs's NewWindowsSafeFilenameCodec: ReservedNTFS in place of
// ReservedPOSIX). This complements, not replaces, the Encode-time DOS-wire
// guard in core/fs's ReservedSet.unescape — that guard is what stops an
// already-escaped control character (e.g. a classic Mac "Icon\r" marker's raw
// CR — always-reserved, so escaped in storage under either set) from being
// restored onto the wire for a share still on "identity"/ReservedPOSIX;
// defaulting to windows-safe here additionally escapes the NTFS punctuation
// set at write time instead of only filtering it back out at read time.
func (s *ShareSection) fsSpec() fs.ShareSpec {
	codec := s.FilenameCodec
	if codec == "" {
		codec = "windows-safe"
	}
	spec := fs.ShareSpec{
		Name:          s.SName,
		FSType:        s.FSType,
		ForkBackend:   s.ForkBackend,
		FilenameCodec: codec,
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

// Spec maps the section to the SMB ShareSpec the service builds a Share from (the
// fs.ShareSpec plus the SMB-specific Description remark).
func (s *ShareSection) Spec() ShareSpec {
	return ShareSpec{Name: s.SName, Description: s.Description, Share: s.fsSpec()}
}

// SpecsFromModel resolves every SMB share instance in the model to its ShareSpec, in
// registration order. A model with no SMB share section yields no specs (the service
// runs with zero shares — the registry default).
func SpecsFromModel(m *config.Model) []ShareSpec {
	if m == nil {
		return nil
	}
	list := m.List(SharesKey)
	if len(list) == 0 {
		return nil
	}
	out := make([]ShareSpec, 0, len(list))
	for _, sec := range list {
		if ss, ok := sec.(*ShareSection); ok {
			out = append(out, ss.Spec())
		}
	}
	return out
}

// RegisterShares installs the SMB share repeated-section schema so codecs round-trip
// each share as a named section. Called from the compose registry wiring (kept out of
// an init() so a build that excludes SMB excludes the section too).
func RegisterShares() {
	config.Register(config.SectionSchema{
		Key:      SharesKey,
		Repeated: true,
		New:      func() config.Section { return &ShareSection{} },
		Validate: func(s config.Section) error {
			if ss, ok := s.(*ShareSection); ok {
				return ss.Validate()
			}
			return nil
		},
	})
}
