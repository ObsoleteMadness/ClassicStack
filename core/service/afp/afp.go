// Package afp is the AppleTalk Filing Protocol file service re-expressed over the
// §9 storage seam. Its Volumes consume only the core/fs (FileSystem + ForkEngine
// + FilenameCodec) and core/metastore (CNIDStore) interfaces, so the service
// holds no storage-layout knowledge: it never imports path/filepath, never
// branches on runtime.GOOS, and never knows which fork container backs a share.
// The wire charset is threaded per request from the AFP path-type byte (§2a).
//
// As of M7 the protocol dispatch (DDP→ATP→ASP→AFP) is wired as a reviewable
// spine: ASPGetStatus, OpenSession/CloseSession/Tickle, and an ASPCommand demux
// to an AFP request dispatcher with a starter command set (FPGetSrvrInfo,
// FPLogin, FPGetSrvrParms, FPOpenVol, FPGetFileDirParms, FPEnumerate) that proves
// the Volume seam end-to-end. Further AFP commands, the two-phase ASPWrite data
// path, and DSI/TCP transport land in follow-up slices.
//
// Security posture: this is a compatibility server, not an authentication
// server. The supported single-step UAMs ("No User Authent", "Cleartxt Passwrd")
// are accepted without credential checking — the intentional weakness that lets
// vintage clients connect. Treat an AFP share as world-readable to anything on
// the AppleTalk segment.
package afp

import (
	"context"
	"slices"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/atp"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

const (
	// Name is the component name for the AFP service.
	Name = "AFP"

	// Socket is the DDP socket the AFP/ASP service listens on: the ASP session
	// listening socket. The spine serves both the GetStatus/OpenSession exchanges
	// and all per-session commands on this one socket, demuxing by ASP session id
	// (the single-socket model netatalk uses), so router.Reply's "reply from the
	// socket the client sent to" routes every response correctly.
	defaultSocket uint8 = 251
)

// Default server-info advertised when the service is built with no overrides.
// AFP 2.2 + the two single-step UAMs the spine accepts.
var (
	defaultAFPVersions = []string{"AFPVersion 2.1", "AFP2.2"}
	defaultUAMs        = []string{"No User Authent", "Cleartxt Passwrd"}
)

// Service is the AFP component. It owns a set of Volumes built over the §9 storage
// seam (fs.ForkFS + metastore.CNIDStore + FilenameCodec) and the ASP session
// layer that drives them. The service holds no storage-layout knowledge itself.
type Service struct {
	logger   log.Logger
	volumes  []*Volume
	info     ServerInfo
	socket   uint8
	sessions *sessionTable

	mu      sync.Mutex
	rtr     router.ServiceRouter
	running bool
}

// New builds the AFP service with no volumes (the registry default — volumes are
// configured separately as the seam wiring matures). Kept for the compose
// registry's zero-config constructor.
func New(logger log.Logger) *Service {
	return &Service{logger: logger, socket: defaultSocket, sessions: newSessionTable()}
}

// NewWithVolumes builds the AFP service over a set of share specs, constructing
// one Volume per spec through the storage seam. A spec whose triple
// (fs_type×fork_backend×filename_codec) is invalid fails the build loudly here
// rather than mangling names at runtime.
func NewWithVolumes(logger log.Logger, specs ...VolumeSpec) (*Service, error) {
	s := New(logger)
	for _, spec := range specs {
		v, err := NewVolume(spec)
		if err != nil {
			return nil, err
		}
		s.volumes = append(s.volumes, v)
	}
	return s, nil
}

// SetRouter binds the AppleTalk router the service replies through. It must be
// called before Start (the compose wiring supplies it). Idempotent.
func (s *Service) SetRouter(rtr router.ServiceRouter) {
	s.mu.Lock()
	s.rtr = rtr
	s.mu.Unlock()
}

// SetServerInfo overrides the advertised server identity (name, machine type,
// version/UAM lists, flags). Empty fields keep their defaults.
func (s *Service) SetServerInfo(info ServerInfo) {
	s.mu.Lock()
	s.info = info
	s.mu.Unlock()
}

// Name returns the component name.
func (s *Service) Name() string { return Name }

// Socket returns the DDP socket the router dispatches AFP/ASP datagrams to.
func (s *Service) Socket() uint8 {
	if s.socket == 0 {
		return defaultSocket
	}
	return s.socket
}

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

// volumeByName returns the volume with the given display name, or nil.
func (s *Service) volumeByName(name string) *Volume {
	for _, v := range s.volumes {
		if v.Name() == name {
			return v
		}
	}
	return nil
}

// serverInfo returns the advertised identity with defaults filled in.
func (s *Service) serverInfo() ServerInfo {
	info := s.info
	if info.ServerName == "" {
		info.ServerName = "ClassicStack"
	}
	if info.MachineType == "" {
		info.MachineType = "ClassicStack"
	}
	if len(info.AFPVersions) == 0 {
		info.AFPVersions = defaultAFPVersions
	}
	if len(info.UAMs) == 0 {
		info.UAMs = defaultUAMs
	}
	return info
}

// supportsVersion reports whether the requested AFP version string is one this
// server advertises.
func (s *Service) supportsVersion(ver string) bool {
	return slices.Contains(s.serverInfo().AFPVersions, ver)
}

// Inbound is the router→service hook for DDP datagrams addressed to the AFP
// socket. It decodes the ATP header and drives the ASP session layer. Non-ATP or
// non-TReq datagrams are ignored (the spine initiates no server transactions).
func (s *Service) Inbound(d ddp.Datagram, from router.RoutedPort) {
	s.mu.Lock()
	running := s.running
	s.mu.Unlock()
	if !running {
		return
	}
	if d.DDPType != atp.DDPType {
		return
	}
	req, ok := parseATPRequest(d, from)
	if !ok {
		return
	}
	s.handleASP(req)
}

// Start brings the service up. Idempotent (§3). The router must be bound first.
func (s *Service) Start(ctx context.Context) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil
	}
	s.running = true
	s.logf("AFP service started (dispatch spine: ASP session + starter AFP commands)")
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
	_ router.Service      = (*Service)(nil)
)
