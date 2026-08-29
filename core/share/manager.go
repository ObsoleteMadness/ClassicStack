package share

import (
	"errors"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// Manager is the dynamic-reconfigure contract a file service exposes so the
// supervisor (DESIGN §11) can add, update, remove, and list shares on a running
// server without a restart. Both AFP and SMB implement it.
//
// Semantics implementers must honour:
//   - AddShare validates the spec via Build/fs.BuildShare; a bad triple or a
//     missing required backend param fails before the share is bound. A duplicate
//     name returns ErrDuplicateShare.
//   - RemoveShare unpublishes the share (no NEW open/tree-connect can bind it) but
//     does NOT tear down in-flight sessions — a client mid-session keeps its bound
//     handle until it closes the share. Unknown name returns ErrNoSuchShare.
//   - UpdateShare builds the replacement stack first (so a bad spec disrupts
//     nothing), then swaps it in under the service lock, preserving any
//     protocol-assigned id. Unknown name returns ErrNoSuchShare.
//   - All four are safe under concurrent dispatch: the implementer guards its
//     share/volume collection with its own lock.
type Manager interface {
	Shares() []Info
	AddShare(spec fs.ShareSpec) error
	UpdateShare(name string, spec fs.ShareSpec) error
	RemoveShare(name string) error
}

// Info is the protocol-neutral view of a bound share for listing/diagnostics.
// It deliberately omits backend params (some are secret); a caller that needs the
// full, redacted config reads Share.Config() directly. AllowedUsers IS included —
// it is the access allow-list (not secret) the management UI edits.
type Info struct {
	Name         string
	FSType       string
	Description  string
	ReadOnly     bool
	AllowedUsers []string // empty = guest/anonymous access
}

// InfoOf builds the listing view of a Share.
func InfoOf(s *Share) Info {
	return Info{
		Name:         s.Name(),
		FSType:       s.Config().FSType,
		Description:  s.Description(),
		ReadOnly:     s.ReadOnly(),
		AllowedUsers: append([]string(nil), s.Permissions().AllowedUsers...),
	}
}

var (
	// ErrDuplicateShare is returned by AddShare when a share of that name exists.
	ErrDuplicateShare = errors.New("share: a share with that name already exists")
	// ErrNoSuchShare is returned by UpdateShare/RemoveShare for an unknown name.
	ErrNoSuchShare = errors.New("share: no share with that name")
)
