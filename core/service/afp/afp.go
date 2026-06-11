// Package afp is the AppleTalk Filing Protocol file service re-expressed over the
// §9 storage seam. Its Volumes consume only the core/fs (FileSystem + ForkEngine
// + FilenameCodec) and core/metastore (CNIDStore) interfaces, so the service
// holds no storage-layout knowledge: it never imports path/filepath, never
// branches on runtime.GOOS, and never knows which fork container backs a share.
// The wire charset is threaded per request from the AFP path-type byte (§2a).
//
// As of M7 the protocol dispatch (DDP→ATP→ASP→AFP) is wired as a reviewable
// spine: ASPGetStatus, OpenSession/CloseSession/Tickle, and an ASPCommand demux
// to an AFP request dispatcher. The command set covers connection/catalog
// (FPGetSrvrInfo, FPLogin, FPGetSrvrParms, FPOpenVol, FPGetFileDirParms,
// FPEnumerate), catalog mutation (FPCreateFile, FPCreateDir, FPDelete, FPRename,
// FPOpenDir/FPCloseDir — addressed dirID-relative through the volume's CNID
// store), and fork I/O (FPOpenFork, FPRead, FPWrite, FPCloseFork,
// FPFlush/FPFlushFork, FPGetForkParms) over the §9 fork engine, and the Desktop
// database (FPOpenDT/FPCloseDT, FPGetComment/FPAddComment/FPRemoveComment,
// FPAddIcon/FPGetIcon/FPGetIconInfo, FPAddAPPL/FPRemoveAPPL/FPGetAPPL) — so a
// client can create, rename, and delete catalog objects, round-trip fork bytes,
// and store Finder comments/icons/application mappings without the spine holding
// any AppleDouble/stream/EA knowledge. Comments ride the fork seam (they travel
// with the file's metadata container); icons and APPL mappings are per-volume
// Desktop state. The catalog reads pack
// the full AFP 2.x file/directory parameter bitmaps (attributes, parent dir id,
// create/modify/backup dates, 32-byte Finder info, long/short names, CNID
// file-number/dir-id, data/resource fork lengths, offspring count, owner/group
// and access rights) from the seam — dates on the 2000 GMT epoch (spec/errata
// "AFP catalog date epoch"). Large FPWrites use the two-phase ASPWrite data path
// (spec/10): the server answers an aspWrite by initiating an aspDataWrite TReq to
// the workstation, collects the data from its TResp packets, then replies to the
// original aspWrite. The DSI/TCP transport lands in a follow-up slice.
//
// Security posture: this is a compatibility server, not an authentication
// server. The supported single-step UAMs ("No User Authent", "Cleartxt Passwrd")
// are accepted without credential checking — the intentional weakness that lets
// vintage clients connect. Treat an AFP share as world-readable to anything on
// the AppleTalk segment.
package afp

import (
	"context"
	"errors"
	"slices"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/atp"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
	"github.com/ObsoleteMadness/ClassicStack/core/share"
)

// errVolumeIDsExhausted is returned by AddShare when the 16-bit AFP volume id
// space has no free id left.
var errVolumeIDsExhausted = errors.New("afp: volume id space exhausted")

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
	logger        log.Logger
	volumes       []*Volume
	info          ServerInfo
	socket        uint8
	sessions      *sessionTable
	pendingWrites *pendingWriteTable

	mu      sync.Mutex
	rtr     router.ServiceRouter
	running bool
}

// New builds the AFP service with no volumes (the registry default — volumes are
// configured separately as the seam wiring matures). Kept for the compose
// registry's zero-config constructor.
func New(logger log.Logger) *Service {
	return &Service{
		logger:        logger,
		socket:        defaultSocket,
		sessions:      newSessionTable(),
		pendingWrites: newPendingWriteTable(),
	}
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

// Volumes returns a snapshot of the bound volumes (diagnostics / catalog
// dispatch). The slice is copied under the lock because the share.Manager mutates
// it on a running server.
func (s *Service) Volumes() []*Volume {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*Volume(nil), s.volumes...)
}

// VolumeByID returns the volume with the given AFP id, if bound.
func (s *Service) VolumeByID(id uint16) (*Volume, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.volumes {
		if v.ID() == id {
			return v, true
		}
	}
	return nil, false
}

// volumeByName returns the volume with the given display name, or nil.
func (s *Service) volumeByName(name string) *Volume {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.volumes {
		if v.Name() == name {
			return v
		}
	}
	return nil
}

// --- share.Manager: dynamic add/update/remove on a running server ---

// Shares lists the bound volumes for diagnostics/management.
func (s *Service) Shares() []share.Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]share.Info, 0, len(s.volumes))
	for _, v := range s.volumes {
		out = append(out, share.InfoOf(v.sh))
	}
	return out
}

// AddShare builds and binds a new volume, allocating its AFP id internally. The
// spec is validated by NewVolume (bad triple / missing param fails before binding);
// a duplicate name is rejected.
func (s *Service) AddShare(spec fs.ShareSpec) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.volumes {
		if v.Name() == spec.Name {
			return share.ErrDuplicateShare
		}
	}
	id := s.allocVolIDLocked()
	if id == 0 {
		return errVolumeIDsExhausted
	}
	v, err := NewVolume(VolumeSpec{ID: id, Name: spec.Name, Share: spec})
	if err != nil {
		return err
	}
	s.volumes = append(s.volumes, v)
	return nil
}

// UpdateShare rebuilds a volume's stack (validating first, so a bad spec disrupts
// nothing) and swaps it in, preserving the AFP id. Sessions holding the old volume
// pointer ride it out until they close it.
func (s *Service) UpdateShare(name string, spec fs.ShareSpec) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, v := range s.volumes {
		if v.Name() == name {
			rebuilt, err := NewVolume(VolumeSpec{ID: v.ID(), Name: spec.Name, Share: spec})
			if err != nil {
				return err
			}
			s.volumes[i] = rebuilt
			return nil
		}
	}
	return share.ErrNoSuchShare
}

// RemoveShare unpublishes a volume: a new FPOpenVol can no longer bind it, but a
// session that already opened it keeps its copied *Volume handle until it closes
// the volume (the FS/metastore are reclaimed when the last reference drops).
func (s *Service) RemoveShare(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, v := range s.volumes {
		if v.Name() == name {
			s.volumes = append(s.volumes[:i], s.volumes[i+1:]...)
			return nil
		}
	}
	return share.ErrNoSuchShare
}

// allocVolIDLocked returns the lowest unused AFP volume id ≥ 1, or 0 if the
// 16-bit id space is exhausted. Caller holds s.mu.
func (s *Service) allocVolIDLocked() uint16 {
	used := make(map[uint16]bool, len(s.volumes))
	for _, v := range s.volumes {
		used[v.ID()] = true
	}
	for id := uint16(1); id != 0; id++ {
		if !used[id] {
			return id
		}
	}
	return 0
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
	if req, ok := parseATPRequest(d, from); ok {
		s.handleASP(req)
		return
	}
	// A TResp is the workstation answering the server-initiated aspDataWrite TReq
	// with phase-2b write data; correlate it back to the pending write.
	if resp, ok := parseATPResponse(d); ok {
		s.handleDataResponse(resp)
	}
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
	s.logf("AFP service started (dispatch spine: ASP session + catalog read/mutate + full file/dir bitmaps + fork I/O + two-phase write + desktop DB)")
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
	_ share.Manager       = (*Service)(nil)
)
