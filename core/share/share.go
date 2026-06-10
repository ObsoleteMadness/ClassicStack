// Package share is the protocol-neutral share/volume seam both file services
// (AFP, SMB) build on. A Share is a thin descriptor — a named, bound filesystem
// plus the config that built it — not a catalog façade: it EXPOSES the
// fs.ForkFS via FS() rather than mirroring its operations, so callers do
// share.FS().Stat(p), share.FS().OpenFork(...), etc. The metadata-carrying
// Rename/Remove live on the FS (core/fs §9), so neither the Share nor the
// protocol layer re-pairs them.
//
// The package imports core/fs ONLY: no metastore (CNID tracking is an AFP
// concern layered on top), and nothing net/reflect/sqlite — it stays clean for
// embedded/TinyGo targets. AFP's Volume and SMB's Share each HOLD a *Share and
// add only their protocol-specific concerns (wire path parsing; for AFP, the
// CNID rebind after an FS Rename/Remove).
package share

import (
	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// Share is one bound filesystem with the config that produced it. It is a
// descriptor, not a façade: everything a protocol needs from the filesystem is
// reached through FS().
type Share struct {
	name        string
	fsys        fs.ForkFS
	config      fs.ShareSpec
	description string
	perms       Permissions
}

// Build assembles a Share from a spec, validating the
// fs_type×fork_backend×filename_codec triple and the backend's required params
// via fs.BuildShare. A bad or under-specified spec fails here, loudly, rather
// than at first request.
func Build(spec fs.ShareSpec, b bus.Bus) (*Share, error) {
	built, err := fs.BuildShare(spec, b)
	if err != nil {
		return nil, err
	}
	return New(spec, built), nil
}

// New wraps an already-built ForkFS as a Share (used where the caller has
// assembled the stack itself, e.g. in tests or a custom backend path).
func New(spec fs.ShareSpec, built fs.ForkFS) *Share {
	return &Share{name: spec.Name, fsys: built, config: spec}
}

// Name returns the share's display/tree name.
func (s *Share) Name() string { return s.name }

// FS returns the bound filesystem. Catalog operations live here: a protocol
// service calls s.FS().Stat(p), s.FS().OpenFork(p, fork, flag), s.FS().Rename
// (which carries metadata), etc. The Share never re-wraps these.
func (s *Share) FS() fs.ForkFS { return s.fsys }

// Config returns the spec the share was built from (fs_type, Path, Extra params,
// fork backend, codec…), for diagnostics and the management UI. Secret params in
// Extra must be redacted by the caller before display/logging.
func (s *Share) Config() fs.ShareSpec { return s.config }

// ReadOnly reports whether the share rejects writes.
func (s *Share) ReadOnly() bool { return s.config.ReadOnly }

// Description returns the operator-supplied human description (may be empty).
func (s *Share) Description() string { return s.description }

// SetDescription sets the human description.
func (s *Share) SetDescription(d string) { s.description = d }

// Permissions returns the share's access policy. This is a stub today (no
// enforcement) — a share is reachable by anything that can connect, matching the
// compatibility-server posture. The field exists so the descriptor and config
// model carry it ahead of real enforcement.
func (s *Share) Permissions() Permissions { return s.perms }

// Codec exposes the share's FilenameCodec so the protocol layer can thread its
// per-request wire charset through Decode/Encode. The built ForkFS carries the
// codec via fs.Coded; if it doesn't (shouldn't happen for a BuildShare result),
// the identity codec — which advertises every wire charset — is used.
func (s *Share) Codec() fs.FilenameCodec {
	if c, ok := s.fsys.(fs.Coded); ok {
		if codec := c.Codec(); codec != nil {
			return codec
		}
	}
	return fs.NewIdentityFilenameCodec()
}
