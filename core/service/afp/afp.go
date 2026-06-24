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
// Desktop state. FPCatSearch searches the whole catalog (descending through
// subdirectories via the FileSystem seam) for partial/full name matches a page at
// a time — the wire behind the Finder's "Find File". The catalog reads pack
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
// server. "No User Authent" is always a guest login. "Cleartxt Passwrd" is a
// guest login when no user store is wired (SetAuthenticator), preserving the
// historical world-readable default; with a store wired, a non-empty user name is
// validated against it and a per-volume allow-list then gates which volumes the
// resulting identity may enumerate (FPGetSrvrParms) and open (FPOpenVol) —
// login-time gating, since a client logs in once and opens volumes under one
// identity. The weak single-step UAMs (no challenge/response) are the intentional
// concession that lets vintage clients connect.
package afp

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
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

// ErrVolumeNameRequired is returned by VolumeSection.Validate when a configured
// volume carries no display name.
var ErrVolumeNameRequired = errors.New("afp: volume name is required")

const (
	// Name is the component name for the AFP service.
	Name = "AFP"

	// OriginAFP tags FS-mutation events this service produces on the shared §10d
	// FS bus, so a same-host-path SMB share's reactor acts on them and AFP's own
	// reactor (fs.SkipOrigin) ignores them.
	OriginAFP = "afp"

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

	mu         sync.Mutex
	rtr        router.ServiceRouter
	auth       Authenticator
	zone       string                       // advertised AppleTalk zone (NBP registration); "" = router default
	transports []string                     // bound transport tokens (ddp/tcp); empty = bind-all (back-compat)
	resolver   func() ([]VolumeSpec, error) // re-resolves the desired volume set from the model; set at wire time for hot-apply
	busFor     func(fs.ShareSpec) bus.Bus   // resolves the shared FS-mutation bus for a share's host path (§10d); nil = isolated
	reactor    *share.Reactor               // §10d coordination consumer; subscribes to same-path buses on Start
	running    bool
}

// Authenticator validates a (username, cleartext password) credential. It is the
// minimal seam the AFP login path needs; the compose wiring hands in the
// configured user store (core/auth). It is a LOCAL interface — structurally
// satisfied by auth.UserStore — so this package does not import core/auth (same
// acyclicity discipline as the SMB BrowseProvider seam). A nil Authenticator
// means guest-only: every login is admitted as guest, exactly as before this seam
// existed.
type Authenticator interface {
	Authenticate(username, password string) (ok bool, err error)
}

// SetAuthenticator installs the credential validator the cleartext UAM consults.
// Passing nil restores guest-only behaviour. Idempotent; safe before Start.
func (s *Service) SetAuthenticator(a Authenticator) {
	s.mu.Lock()
	s.auth = a
	s.mu.Unlock()
}

// New builds the AFP service with no volumes (the registry default — volumes are
// configured separately as the seam wiring matures). Kept for the compose
// registry's zero-config constructor.
func New(logger log.Logger) *Service {
	s := &Service{
		logger:        logger,
		socket:        defaultSocket,
		sessions:      newSessionTable(),
		pendingWrites: newPendingWriteTable(),
	}
	// §10d reactor: observe foreign-origin FS mutations under one of our volumes.
	// AFP is deliberately EXCLUDED from wire notifications — classic AFP has no
	// per-directory change-notify push (a client discovers changes by polling the
	// volume modification date / re-enumerating, and the only server→workstation ASP
	// attention codes are shutdown/crash/message, none of which mean "catalog
	// changed"). So the AFP sink stays nil (no wire frame); the reactor still tracks
	// Delivered() as the observable that coordination reached AFP, and the volume
	// mod-date a polling client reads reflects the underlying FS. The SMB side, which
	// HAS a real async primitive (NT_TRANSACT NOTIFY_CHANGE), does emit frames.
	s.reactor = share.NewReactor(OriginAFP, s.volumeRoots, nil)
	return s
}

// ReactorDelivered reports how many foreign-origin FS mutations the §10d reactor has
// delivered (a same-host-path SMB share's writes this AFP service was notified of).
// Diagnostics / tests; 0 until a cross-service mutation occurs.
func (s *Service) ReactorDelivered() uint64 {
	if s.reactor == nil {
		return 0
	}
	return s.reactor.Delivered()
}

// volumeRoots returns the live volumes as (name, host-root) pairs for the §10d
// reactor's path matching.
func (s *Service) volumeRoots() []share.NamedPath {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]share.NamedPath, 0, len(s.volumes))
	for _, v := range s.volumes {
		out = append(out, share.NamedPath{Name: v.Name(), Root: v.sh.Config().Path})
	}
	return out
}

// NewWithVolumes builds the AFP service over a set of share specs, constructing
// one Volume per spec through the storage seam. A spec whose triple
// (fs_type×fork_backend×filename_codec) is invalid fails the build loudly here
// rather than mangling names at runtime.
func NewWithVolumes(logger log.Logger, specs ...VolumeSpec) (*Service, error) {
	s := New(logger)
	for _, spec := range specs {
		v, err := NewVolumeWithBus(spec, s.busForSpec(spec.Share))
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

// SetServerName overrides only the advertised server name, leaving the rest of the
// ServerInfo (machine type, version/UAM lists, flags) at their defaults. The compose
// wiring calls it from the AFP server section. An empty name keeps the default.
func (s *Service) SetServerName(name string) {
	s.mu.Lock()
	s.info.ServerName = name
	s.mu.Unlock()
}

// SetZone records the AppleTalk zone the service advertises into (NBP registration).
// Empty means the router's default zone. Surfaced via Describable for the dashboard.
func (s *Service) SetZone(zone string) {
	s.mu.Lock()
	s.zone = zone
	s.mu.Unlock()
}

// SetTransports records the bound transport tokens (afp.TransportDDP/TransportTCP) for
// dashboard display. An empty list means bind-all (the back-compat default). It does
// NOT itself gate binding — the classic DDP stack is joined via the router membership
// and the modern DSI/TCP transport (when it lands) reads the same section — this is the
// Describable surface so the operator sees which stacks are active.
func (s *Service) SetTransports(transports []string) {
	s.mu.Lock()
	s.transports = append([]string(nil), transports...)
	s.mu.Unlock()
}

// Kind labels the AFP component for the dashboard (component.Describable).
func (s *Service) Kind() string { return "service" }

// Props surfaces dashboard detail (component.Describable): the advertised zone, the
// bound transports, and the live volume count, so the operator sees AFP's identity and
// binding without opening the config modal.
func (s *Service) Props() map[string]string {
	s.mu.Lock()
	zone := s.zone
	transports := s.transports
	nvols := len(s.volumes)
	s.mu.Unlock()
	props := map[string]string{"volumes": strconv.Itoa(nvols)}
	if zone != "" {
		props["zone"] = zone
	}
	if len(transports) > 0 {
		props["transports"] = strings.Join(transports, ",")
	} else {
		props["transports"] = "ddp,tcp (all)"
	}
	return props
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
	b := bus.Bus(nil)
	if s.busFor != nil {
		b = s.busFor(spec)
	}
	v, err := NewVolumeWithBus(VolumeSpec{ID: id, Name: spec.Name, Share: spec}, b)
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
			b := bus.Bus(nil)
			if s.busFor != nil {
				b = s.busFor(spec)
			}
			rebuilt, err := NewVolumeWithBus(VolumeSpec{ID: v.ID(), Name: spec.Name, Share: spec}, b)
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

// --- component.Configurable: hot-apply a changed volume set without restart ---

// SetVolumeResolver installs the closure the supervisor's Reconfigure consults to
// re-resolve the desired volume set from the (already-updated) shared model. The
// compose registry supplies it (a closure over SpecsFromModel(model)); without it
// ApplyConfig reports ErrNeedsRestart so the supervisor falls back to a full
// rebuild. Idempotent; safe before Start.
func (s *Service) SetVolumeResolver(resolve func() ([]VolumeSpec, error)) {
	s.mu.Lock()
	s.resolver = resolve
	s.mu.Unlock()
}

// SetBusResolver installs the closure that maps a share's spec to the shared
// FS-mutation bus for its host path (§10d). The compose registry supplies it (one
// bus per distinct host path, shared with a same-path SMB share) so a mutation by
// one service reaches the other. A nil resolver (or one returning nil) means each
// volume gets a private bus — no cross-service coordination. Idempotent; safe
// before Start. Volumes already built are not retro-fitted; this affects volumes
// built after it is set (AddShare / a reconcile / a rebuild).
func (s *Service) SetBusResolver(resolve func(fs.ShareSpec) bus.Bus) {
	s.mu.Lock()
	s.busFor = resolve
	s.mu.Unlock()
}

// busForSpec resolves the shared bus for a spec, or nil when no resolver is wired.
// Caller need not hold s.mu (the field is read under it).
func (s *Service) busForSpec(spec fs.ShareSpec) bus.Bus {
	s.mu.Lock()
	resolve := s.busFor
	s.mu.Unlock()
	if resolve == nil {
		return nil
	}
	return resolve(spec)
}

// ApplyConfig hot-applies a changed volume set (§11b): the AFP "config" is the set
// of repeated volume sections (config.Model.Lists[VolumesKey]), not a singleton
// section, so the passed section payload is ignored — ApplyConfig re-resolves the
// whole desired set from the model and reconciles it against the live volumes via
// the share.Manager (Add new, Update changed, Remove dropped). A volume's fs-type /
// backend change is absorbed by UpdateShare rebuilding that one volume's stack — no
// service restart, and unrelated sessions are undisturbed. When no resolver is wired
// (e.g. a unit-level service with no compose root) it returns ErrNeedsRestart so the
// supervisor falls back to the rebuild path.
func (s *Service) ApplyConfig(_ any) error {
	s.mu.Lock()
	resolve := s.resolver
	s.mu.Unlock()
	if resolve == nil {
		return component.ErrNeedsRestart
	}
	desired, err := resolve()
	if err != nil {
		return err
	}
	return s.ReconcileVolumes(desired)
}

// ReconcileVolumes brings the live volume set to match desired, keyed by volume
// name: a name present only in desired is added, one present in both is updated
// (rebuilding its stack, preserving its AFP id), one present only live is removed.
// It assigns ids and builds every volume before mutating, so a bad spec in the set
// aborts the whole reconcile leaving the live volumes untouched (all-or-nothing).
// Order of the surviving volumes follows desired.
func (s *Service) ReconcileVolumes(desired []VolumeSpec) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Map live volumes by name so an updated spec keeps its protocol-assigned id, and
	// seed the used-id set with the ids the surviving volumes keep.
	live := make(map[string]*Volume, len(s.volumes))
	for _, v := range s.volumes {
		live[v.Name()] = v
	}
	used := make(map[uint16]bool, len(desired))
	for _, spec := range desired {
		if existing, ok := live[spec.Name]; ok && spec.ID == 0 {
			used[existing.ID()] = true
		} else if spec.ID != 0 {
			used[spec.ID] = true
		}
	}
	nextID := func() uint16 {
		for id := uint16(1); id != 0; id++ {
			if !used[id] {
				used[id] = true
				return id
			}
		}
		return 0
	}

	// Build the full desired set first (a bad triple/param fails before any swap).
	out := make([]*Volume, 0, len(desired))
	seen := make(map[string]bool, len(desired))
	for _, spec := range desired {
		if seen[spec.Name] {
			return share.ErrDuplicateShare
		}
		seen[spec.Name] = true
		id := spec.ID
		switch {
		case id != 0:
			// caller-pinned id; honoured as-is
		case live[spec.Name] != nil:
			id = live[spec.Name].ID() // preserve the id across an update
		default:
			if id = nextID(); id == 0 {
				return errVolumeIDsExhausted
			}
		}
		b := bus.Bus(nil)
		if s.busFor != nil {
			b = s.busFor(spec.Share)
		}
		v, err := NewVolumeWithBus(VolumeSpec{ID: id, Name: spec.Name, Share: spec.Share}, b)
		if err != nil {
			return err
		}
		v.SetExtensionMap(spec.ExtMap) // default type/creator for files with no Finder info
		out = append(out, v)
	}
	s.volumes = out
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
	s.subscribeReactorLocked()
	s.logf("AFP service started (dispatch spine: ASP session + catalog read/mutate + full file/dir bitmaps + fork I/O + two-phase write + desktop DB + catsearch)")
	return nil
}

// subscribeReactorLocked attaches the §10d reactor to each distinct FS bus among the
// current volumes (compose hands one bus per host path, so two volumes on one path
// resolve to one bus — subscribed once). Caller holds s.mu. A no-op when no bus
// resolver is wired (every volume is isolated).
func (s *Service) subscribeReactorLocked() {
	if s.busFor == nil || s.reactor == nil {
		return
	}
	seen := make(map[bus.Bus]bool, len(s.volumes))
	for _, v := range s.volumes {
		b := s.busFor(v.sh.Config())
		if b == nil || seen[b] {
			continue
		}
		seen[b] = true
		s.reactor.Subscribe(b)
	}
}

// Stop brings the service down. Safe after failed/partial Start (§3).
func (s *Service) Stop(ctx context.Context) error {
	_ = ctx
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	reactor := s.reactor
	// Snapshot the live volumes so their backends can be closed after the lock drops.
	// Stop is definitive teardown (no session can still hold a volume), so closing each
	// volume's FS here releases any GC-invisible backend resource (zipfs handles,
	// macgarden goroutine). A plain backend's Close is a no-op.
	volumes := append([]*Volume(nil), s.volumes...)
	s.mu.Unlock()

	if reactor != nil {
		reactor.Stop()
	}
	for _, v := range volumes {
		_ = v.Close()
	}
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
	_ component.Component    = (*Service)(nil)
	_ component.Configurable = (*Service)(nil)
	_ router.Service         = (*Service)(nil)
	_ share.Manager          = (*Service)(nil)
)
