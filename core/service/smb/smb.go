// Package smb is the SMB/CIFS file service re-expressed over the §9 storage seam.
// Its Shares consume only the core/fs (FileSystem + ForkEngine + FilenameCodec)
// interfaces, so the service holds no storage-layout knowledge. The filename wire
// charset is threaded per request from the FLAGS2 Unicode bit (UTF-16 vs the
// OEM/ANSI page, §2a) — keyed off the flag, not the dialect, so SMB 1.0 clients
// that set SMB_FLAGS2_UNICODE get UTF-16. A same-fs_type AFP volume and
// SMB share see the same forks and FinderInfo through the same ForkEngine. As of
// M7 the protocol dispatch is still a thin stub; what lands here is the service
// shape that drives the seam.
package smb

import (
	"context"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/share"
)

// Name is the component name for the SMB service.
const Name = "SMB"

// Service is the SMB component. As of M7 it owns a set of Shares built over the
// §9 storage seam (fs.ForkFS + FilenameCodec) and holds no storage-layout
// knowledge itself. The protocol dispatch (NBT/NetBEUI/IPX → SMB command engine)
// is still a thin stub at this milestone; the service shape that drives the seam
// is what lands here.
type Service struct {
	logger log.Logger
	shares []*Share

	mu      sync.Mutex
	running bool
}

// New builds the SMB service with no shares (the registry default).
func New(logger log.Logger) *Service {
	return &Service{logger: logger}
}

// NewWithShares builds the SMB service over a set of share specs, constructing
// one Share per spec through the storage seam. An invalid triple fails the build
// loudly here rather than mangling names at runtime.
func NewWithShares(logger log.Logger, specs ...ShareSpec) (*Service, error) {
	s := &Service{logger: logger}
	for _, spec := range specs {
		sh, err := NewShare(spec)
		if err != nil {
			return nil, err
		}
		s.shares = append(s.shares, sh)
	}
	return s, nil
}

// Name returns the component name.
func (s *Service) Name() string { return Name }

// ShareByName returns the share with the given tree name, if bound. Used by the
// tree-connect dispatch; guarded because the Manager mutates the slice at runtime.
func (s *Service) ShareByName(name string) (*Share, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.findShareLocked(name)
}

// findShareLocked returns the share of that name; caller holds s.mu.
func (s *Service) findShareLocked(name string) (*Share, bool) {
	for _, sh := range s.shares {
		if sh.Name() == name {
			return sh, true
		}
	}
	return nil, false
}

// --- share.Manager: dynamic add/update/remove on a running server ---

// Shares lists the bound shares for diagnostics/management.
func (s *Service) Shares() []share.Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]share.Info, 0, len(s.shares))
	for _, sh := range s.shares {
		out = append(out, share.InfoOf(sh.sh))
	}
	return out
}

// AddShare builds and binds a new share. The spec is validated by share.Build
// (bad triple / missing param fails before binding); a duplicate name is rejected.
func (s *Service) AddShare(spec fs.ShareSpec) error {
	built, err := share.Build(spec, nil)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.findShareLocked(spec.Name); ok {
		return share.ErrDuplicateShare
	}
	s.shares = append(s.shares, newFromShare(built))
	return nil
}

// UpdateShare rebuilds a share's stack (validating first, so a bad spec disrupts
// nothing) and swaps it in. In-flight tree connects holding the old handle ride
// it out until they disconnect.
func (s *Service) UpdateShare(name string, spec fs.ShareSpec) error {
	built, err := share.Build(spec, nil)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, sh := range s.shares {
		if sh.Name() == name {
			s.shares[i] = newFromShare(built)
			return nil
		}
	}
	return share.ErrNoSuchShare
}

// RemoveShare unpublishes a share: new tree connects can no longer bind it, but
// in-flight sessions keep their copied handle until they disconnect (the FS is
// reclaimed when the last reference drops).
func (s *Service) RemoveShare(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, sh := range s.shares {
		if sh.Name() == name {
			s.shares = append(s.shares[:i], s.shares[i+1:]...)
			return nil
		}
	}
	return share.ErrNoSuchShare
}

// Start brings the service up. Idempotent (§3).
func (s *Service) Start(ctx context.Context) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil
	}
	s.running = true
	s.logf("SMB service started (shares bound; protocol dispatch stub)")
	return nil
}

// Stop brings the service down. Safe after failed/partial Start (§3).
func (s *Service) Stop(ctx context.Context) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return nil
	}
	s.running = false
	s.logf("SMB service stopped")
	return nil
}

// logf emits one info line through the logger if configured.
func (s *Service) logf(msg string) {
	if s.logger == nil || !s.logger.Enabled(log.Info) {
		return
	}
	s.logger.Log1(log.Info, msg, log.Str("scope", Name))
}

// compile-time assertions.
var (
	_ component.Component = (*Service)(nil)
	_ share.Manager       = (*Service)(nil)
)
