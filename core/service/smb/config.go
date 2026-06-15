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
	SName string `toml:"name"`
	// Description is the human remark NetShareEnum reports (the share comment).
	Description string `toml:"description"`
	// FSType selects the FileSystem factory ("local_fs", "memfs", …).
	FSType string `toml:"fs_type"`
	// ForkBackend selects the fork engine ("appledouble"|"ads"|"xattr"|"native"|"auto").
	ForkBackend string `toml:"fork_backend"`
	// FilenameCodec selects the wire↔store name codec.
	FilenameCodec string `toml:"filename_codec"`
	// NameEngine selects the short/medium name engine.
	NameEngine string `toml:"name_engine"`
	// Metastore selects the CNID/shortname store kind ("mem" default).
	Metastore string `toml:"metastore"`
	// Path is the backend location (host directory for local_fs, …).
	Path string `toml:"path"`
	// ReadOnly makes the whole share read-only (share-wide, not per-user).
	ReadOnly bool `toml:"read_only"`
	// AllowedUsers is the access allow-list (empty = guest/world). Not secret.
	AllowedUsers []string `toml:"allowed_users"`
	// Options carries backend-specific params as "key=value" entries → ShareSpec.Extra.
	Options []string `toml:"options"`
}

// compile-time assertions: *ShareSection is a NamedSection.
var (
	_ config.Section      = (*ShareSection)(nil)
	_ config.NamedSection = (*ShareSection)(nil)
)

// Key returns the shared repeated-section schema key.
func (s *ShareSection) Key() string { return SharesKey }

// InstanceName returns the per-share instance name (the section name the codec writes).
func (s *ShareSection) InstanceName() string { return s.SName }

// Clone returns a deep copy. The two slices are copied so staging never aliases the
// live instance's backing arrays.
func (s *ShareSection) Clone() config.Section {
	cp := *s
	cp.AllowedUsers = append([]string(nil), s.AllowedUsers...)
	cp.Options = append([]string(nil), s.Options...)
	return &cp
}

// Validate checks the section in isolation. A share must have a name; the
// fs_type×fork×codec triple and required backend params are validated later by
// share.Build, so they are not re-checked here.
func (s *ShareSection) Validate() error {
	if strings.TrimSpace(s.SName) == "" {
		return ErrShareNameRequired
	}
	return nil
}

// fsSpec maps the section to an fs.ShareSpec (the storage-seam half). Options
// "key=value" entries become Extra entries; a malformed entry (no '=') with a
// non-empty key reads as a present-but-empty value, a bare "" key is dropped.
func (s *ShareSection) fsSpec() fs.ShareSpec {
	spec := fs.ShareSpec{
		Name:          s.SName,
		FSType:        s.FSType,
		ForkBackend:   s.ForkBackend,
		FilenameCodec: s.FilenameCodec,
		NameEngine:    s.NameEngine,
		Metastore:     s.Metastore,
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
