// Package afp is the AppleTalk Filing Protocol file service re-expressed over the
// §9 storage seam. Its Volumes consume only the core/fs (FileSystem + ForkEngine
// + FilenameCodec) and core/metastore (CNIDStore) interfaces, so the service
// holds no storage-layout knowledge: it never imports path/filepath, never
// branches on runtime.GOOS, and never knows which fork container backs a share.
// The wire charset is threaded per request from the AFP path-type byte (§2a). As
// of M7 the protocol dispatch (DDP→ATP→ASP→AFP) is still a thin stub; what lands
// here is the service shape that drives the seam.
package afp

import (
	"context"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

// Name is the component name for the AFP service.
const Name = "AFP"

// Service is the AFP component. As of M7 it owns a set of Volumes built over the
// §9 storage seam (fs.ForkFS + metastore.CNIDStore + FilenameCodec) and holds no
// storage-layout knowledge itself. The protocol dispatch (DDP→ATP→ASP→AFP) is
// still a thin stub at this milestone; the service shape that drives the seam is
// what lands here. Volumes are populated once at construction and read-only
// thereafter, so they need no synchronisation on the call path.
type Service struct {
	logger  log.Logger
	volumes []*Volume

	mu      sync.Mutex
	running bool
}

// New builds the AFP service with no volumes (the registry default — volumes are
// configured separately as the seam wiring matures). Kept for the compose
// registry's zero-config constructor.
func New(logger log.Logger) *Service {
	return &Service{logger: logger}
}

// NewWithVolumes builds the AFP service over a set of share specs, constructing
// one Volume per spec through the storage seam. A spec whose triple
// (fs_type×fork_backend×filename_codec) is invalid fails the build loudly here
// rather than mangling names at runtime.
func NewWithVolumes(logger log.Logger, specs ...VolumeSpec) (*Service, error) {
	s := &Service{logger: logger}
	for _, spec := range specs {
		v, err := NewVolume(spec)
		if err != nil {
			return nil, err
		}
		s.volumes = append(s.volumes, v)
	}
	return s, nil
}

// Name returns the component name.
func (s *Service) Name() string { return Name }

// Volumes returns the bound volumes (diagnostics / catalog dispatch).
func (s *Service) Volumes() []*Volume { return s.volumes }

// VolumeByID returns the volume with the given AFP id, if bound.
func (s *Service) VolumeByID(id uint16) (*Volume, bool) {
	for _, v := range s.volumes {
		if v.ID() == id {
			return v, true
		}
	}
	return nil, false
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
	s.logf("AFP service started (volumes bound; protocol dispatch stub)")
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
	s.logf("AFP service stopped")
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
)
