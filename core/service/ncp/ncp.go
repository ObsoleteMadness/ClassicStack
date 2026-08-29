// Package ncp is the Novell NetWare Core Protocol (NCP) file service re-expressed
// over the §9 storage seam — a NetWare 3.x bindery-emulation server that lets the
// large installed base of NETx / VLM / Client32 (DOS, Windows 3.x/9x), Mac
// (MacIPX), and OS/2 NetWare requesters attach and use shares over IPX. It is the
// NetWare analogue of the AFP and SMB services: its Volumes consume only the
// core/fs (FileSystem + ForkEngine + FilenameCodec) interfaces, so the service
// holds no storage-layout knowledge, and a same-fs_type AFP volume / SMB share /
// NCP volume on one host path see the same forks through one ForkEngine (§10d).
//
// Transport: NCP over IPX (connectionless, socket 0x0451) on the core/router/ipx
// mini-router — the same dispatch the SMB direct-hosted-over-IPX transport rides.
// SAP advertising (socket 0x0452) makes the server discoverable to NETx/VLM.
// NCP-over-IP (:524) is out of scope for this milestone.
//
// Security posture: a compatibility server, not an authentication server (the
// same posture as SMB/AFP). With no user store wired (SetAuthenticator), the
// bindery login verbs grant a guest connection without checking credentials (the
// intentional weakness that lets vintage clients connect) and every volume is
// world-accessible. With a user store wired, a bindery login is validated against
// it (cleartext, and the NetWare encrypted-login challenge-response) and a
// per-volume allow-list gates which volumes the identity may use.
//
// Reference & attribution: Novell NCP/SAP/bindery. This implementation was inspired
// by mars_nwe — the MARtin Stover NetWare Emulator, (C) 1993,1995 Martin Stover,
// Marburg, Germany — and by Linux ncpfs (Volker Lendecke et al); both are the
// canonical open-source NCP references (CLAUDE.md #7). The wire behaviour is a clean
// re-implementation over the §9 storage seam, not a code port, but the design owes a
// clear debt to Martin Stover's work.
package ncp

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/share"
)

// Name is the component name for the NCP service.
const Name = "NCP"

// OriginNCP tags FS-mutation events this service produces on the shared §10d FS
// bus, so a same-host-path AFP/SMB reactor acts on them and NCP's own reactor
// (fs.SkipOrigin) ignores them.
const OriginNCP = "ncp"

// defaultServerName is the NetWare server name advertised (via SAP and Get Server
// Info) when no §4-bis identity hostname is configured. NetWare names are
// upper-case.
const defaultServerName = "CLASSICSTACK"

// Authenticator validates a (username, cleartext password) credential. It is a
// LOCAL interface — structurally satisfied by auth.UserStore — so this package
// does not import core/auth (the same acyclicity discipline as SMB). A nil
// Authenticator means guest-only.
type Authenticator interface {
	Authenticate(username, password string) (ok bool, err error)
}

// Service is the NCP component. It owns a set of Volumes built over the §9 storage
// seam and a table of client service-connections; it holds no storage-layout
// knowledge itself.
type Service struct {
	logging log.Logger // established at construction, never nil; sinks own level filtering
	vols    []*Volume
	server  string // NetWare server name (the §4-bis identity hostname); upper-cased
	desc    string // server description (the §4-bis identity description); optional
	// internalNet is the configured NetWare internal IPX network (0 = derive at
	// compose time from the station MAC). Wired into the IPX mini-router when NCP
	// attaches; changing it needs a transport rebuild (ApplyConfig → ErrNeedsRestart).
	internalNet uint32
	auth        Authenticator

	conns *connTable
	pub   Publisher // telemetry-bus seam for connection open/close events; nil = no publishing

	mu        sync.Mutex
	running   bool
	closers   []circuitCloser              // NCP-owned transports (over-IPX); torn down on Stop
	resolver  func() ([]VolumeSpec, error) // re-resolves the desired volume set from the model; set at wire time for hot-apply
	busFor    func(fs.ShareSpec) bus.Bus   // resolves the shared FS-mutation bus for a volume's host path (§10d); nil = isolated
	reactor   *share.Reactor               // §10d coordination consumer
	statsSink func(component.Stats)        // §5 push sink; nil = poll-only
	rxObs     func(rx, tx int)             // §5 traffic observer; nil = unmetered

	counters counters // monotonic protocol counters (guarded by mu)
	enabled  bool     // configured-enabled flag (component.Enableable); default true
}

// circuitCloser is the per-transport teardown surface the service holds: a
// transport NCP owns directly (the over-IPX transport) releases its connections
// on Stop so no file handles leak. *OverIPX satisfies it.
type circuitCloser interface{ closeCircuits() }

// New builds the NCP service with no volumes (the registry default). The logger
// is established here, at configure time; a nil logger becomes a sink-less no-op
// so call sites log unconditionally and the sinks decide what is emitted.
func New(logger log.Logger) *Service {
	if logger == nil {
		logger = log.New(Name)
	}
	s := &Service{logging: logger, conns: newConnTable(), enabled: true}
	// §10d reactor: deliver foreign-origin FS mutations under one of our volumes to
	// the NCP wire-push sink. NCP (like classic AFP) has no per-directory async push
	// on the wire, so the sink is count-only for now — a clean hook a later slice
	// turns into a wire notification. volumeRoots re-reads the live set per event.
	s.reactor = share.NewReactor(OriginNCP, s.volumeRoots, nil)
	return s
}

// Name returns the component name.
func (s *Service) Name() string { return Name }

// SetServerName sets the NetWare server name NCP reports for itself (the §4-bis
// identity hostname, upper-cased). Unset defaults to CLASSICSTACK. Idempotent.
func (s *Service) SetServerName(name string) {
	s.mu.Lock()
	s.server = strings.ToUpper(strings.TrimSpace(name))
	s.mu.Unlock()
}

// SetDescription sets the server description NCP reports. Idempotent.
func (s *Service) SetDescription(desc string) {
	s.mu.Lock()
	s.desc = desc
	s.mu.Unlock()
}

// SetInternalNetwork records the configured NetWare internal IPX network number
// (0 = derive from the station MAC at compose time). The IPX transport cross-wire
// consults ConfiguredInternalNetwork when attaching NCP.
func (s *Service) SetInternalNetwork(network uint32) {
	s.mu.Lock()
	s.internalNet = network
	s.mu.Unlock()
}

// ConfiguredInternalNetwork returns the operator-configured internal network
// (0 = auto-derive). Used by compose when wiring the IPX mini-router for NCP.
func (s *Service) ConfiguredInternalNetwork() uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.internalNet
}

// InternalNetworkBytes returns the configured internal network as a 4-byte
// big-endian IPX network, or ok=false when InternalNetwork is 0 (caller should
// derive from the station MAC).
func (s *Service) InternalNetworkBytes() (net [4]byte, ok bool) {
	n := s.ConfiguredInternalNetwork()
	if n == 0 {
		return net, false
	}
	// Hand-rolled big-endian: core/ bans encoding/binary (pulls in reflect) — see the
	// archtest §1 rule and core/protocol/ddp.
	net[0], net[1], net[2], net[3] = byte(n>>24), byte(n>>16), byte(n>>8), byte(n)
	return net, true
}

// serverName returns the configured server name, defaulting to CLASSICSTACK.
func (s *Service) serverName() string {
	s.mu.Lock()
	name := s.server
	s.mu.Unlock()
	if name != "" {
		return name
	}
	return defaultServerName
}

// SetAuthenticator installs the credential validator the bindery login verbs
// consult. Passing nil restores guest-only behaviour. Idempotent; safe before
// Start.
func (s *Service) SetAuthenticator(a Authenticator) {
	s.mu.Lock()
	s.auth = a
	s.mu.Unlock()
}

// volumeRoots returns the live volumes as (name, host-root) pairs for the §10d
// reactor's path matching.
func (s *Service) volumeRoots() []share.NamedPath {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]share.NamedPath, 0, len(s.vols))
	for _, v := range s.vols {
		out = append(out, share.NamedPath{Name: v.Name(), Root: v.sh.Config().Path, FS: v.FS()})
	}
	return out
}

// volumeByName returns the volume with the given NetWare name (case-insensitive),
// if bound. A trailing colon (SYS:) is tolerated.
func (s *Service) volumeByName(name string) (*Volume, bool) {
	name = strings.TrimSuffix(name, ":")
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.vols {
		if strings.EqualFold(v.Name(), name) {
			return v, true
		}
	}
	return nil, false
}

// volumeByIndex returns the volume at a zero-based index (NetWare volume number),
// if in range.
func (s *Service) volumeByIndex(i int) (*Volume, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i < 0 || i >= len(s.vols) {
		return nil, false
	}
	return s.vols[i], true
}

// volumeIndex returns the zero-based NetWare volume number of a bound volume, or 0
// when it is not found (the name-space replies carry a volume number).
func (s *Service) volumeIndex(vol *Volume) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, v := range s.vols {
		if v == vol {
			return i
		}
	}
	return 0
}

// --- share.Manager: dynamic add/update/remove on a running server ---

// VolumeByName returns the bound volume with the given name, if any. Used by the
// operator Finder (adapter/control/finder) to open a live ForkFS without rebuilding
// the share. Names are matched case-insensitively like NetWare.
func (s *Service) VolumeByName(name string) (*Volume, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.vols {
		if strings.EqualFold(v.Name(), name) {
			return v, true
		}
	}
	return nil, false
}

// Shares lists the bound volumes for diagnostics/management.
func (s *Service) Shares() []share.Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]share.Info, 0, len(s.vols))
	for _, v := range s.vols {
		out = append(out, share.InfoOf(v.sh))
	}
	return out
}

// AddShare builds and binds a new volume. The spec is validated by share.Build;
// a duplicate name is rejected.
func (s *Service) AddShare(spec fs.ShareSpec) error {
	built, err := share.Build(spec, s.busForSpec(spec))
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.vols {
		if strings.EqualFold(v.Name(), spec.Name) {
			return share.ErrDuplicateShare
		}
	}
	s.vols = append(s.vols, newFromShare(built))
	return nil
}

// UpdateShare rebuilds a volume's stack (validating first) and swaps it in.
func (s *Service) UpdateShare(name string, spec fs.ShareSpec) error {
	built, err := share.Build(spec, s.busForSpec(spec))
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, v := range s.vols {
		if v.Name() == name {
			s.vols[i] = newFromShare(built)
			return nil
		}
	}
	return share.ErrNoSuchShare
}

// RemoveShare unpublishes a volume: new opens can no longer bind it, but in-flight
// connections keep their bound handle until they close it.
func (s *Service) RemoveShare(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, v := range s.vols {
		if v.Name() == name {
			s.vols = append(s.vols[:i], s.vols[i+1:]...)
			return nil
		}
	}
	return share.ErrNoSuchShare
}

// --- component.Configurable: hot-apply a changed volume set without restart ---

// SetShareResolver installs the closure the supervisor's Reconfigure consults to
// re-resolve the desired volume set from the (already-updated) shared model.
func (s *Service) SetShareResolver(resolve func() ([]VolumeSpec, error)) {
	s.mu.Lock()
	s.resolver = resolve
	s.mu.Unlock()
}

// SetBusResolver installs the closure that maps a volume's spec to the shared
// FS-mutation bus for its host path (§10d).
func (s *Service) SetBusResolver(resolve func(fs.ShareSpec) bus.Bus) {
	s.mu.Lock()
	s.busFor = resolve
	s.mu.Unlock()
}

// busForSpec resolves the shared bus for a spec, or nil when no resolver is wired.
func (s *Service) busForSpec(spec fs.ShareSpec) bus.Bus {
	s.mu.Lock()
	resolve := s.busFor
	s.mu.Unlock()
	if resolve == nil {
		return nil
	}
	return resolve(spec)
}

// Publisher is the telemetry-bus seam: NCP publishes a bus.SessionChanged when a
// connection is created or destroyed, so a UI can refresh its session table
// without polling. bus.Bus satisfies it; a nil Publisher disables publishing.
// Kept narrow (Publish only), mirroring core/service/messenger's seam.
type Publisher interface {
	Publish(bus.Event)
}

// SetPublisher installs the telemetry-bus seam (compose wires ctx.Telemetry here).
// A nil pub disables publishing. Idempotent; safe before or after Start.
func (s *Service) SetPublisher(pub Publisher) {
	s.mu.Lock()
	s.pub = pub
	s.mu.Unlock()
}

// publishSession emits a bus.SessionChanged for NCP, if a Publisher is wired.
func (s *Service) publishSession() {
	s.mu.Lock()
	pub := s.pub
	s.mu.Unlock()
	if pub == nil {
		return
	}
	pub.Publish(bus.SessionChanged{Component: "NCP"})
}

// SetEnabled records the configured-enabled flag (component.Enableable). The compose
// factory sets it from the NCP server section; missing config keeps the New() default
// of true so existing deployments without enabled= stay on.
func (s *Service) SetEnabled(enabled bool) {
	s.mu.Lock()
	s.enabled = enabled
	s.mu.Unlock()
}

// Enabled reports the configured-enabled flag (component.Enableable).
func (s *Service) Enabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enabled
}

// SessionInfo is a diagnostics snapshot of one NCP service-connection.
type SessionInfo struct {
	Number    uint16
	Endpoint  string
	User      string
	LoggedIn  bool
	OpenFiles int
	LastSeen  time.Time
}

// Sessions snapshots live NCP connections for the Sharing Monitor.
func (s *Service) Sessions() []SessionInfo {
	if s.conns == nil {
		return nil
	}
	all := s.conns.All()
	out := make([]SessionInfo, 0, len(all))
	for _, c := range all {
		c.mu.Lock()
		out = append(out, SessionInfo{
			Number:    c.number,
			Endpoint:  c.ep.String(),
			User:      c.user,
			LoggedIn:  c.loggedIn,
			OpenFiles: len(c.files),
			LastSeen:  c.lastSeen,
		})
		c.mu.Unlock()
	}
	return out
}

// ApplyConfig hot-applies config changes (§11b). A *ServerSection payload updates
// the advertised name/description and asks for a restart so the IPX transport
// cross-wire can re-bind the internal network. A nil / other payload (volume
// cascade notify) re-resolves the volume set from the model. When no resolver is
// wired it returns ErrNeedsRestart.
func (s *Service) ApplyConfig(section any) error {
	if ss, ok := section.(*ServerSection); ok && ss != nil {
		if n := strings.TrimSpace(ss.ServerName); n != "" {
			s.SetServerName(n)
		}
		s.SetDescription(ss.Description)
		s.SetInternalNetwork(ss.InternalNetwork)
		// Internal network is owned by the IPX mini-router (wired at compose time);
		// changing it — or any server-level setting that should rebuild SAP — needs
		// a full restart of the transport cross-wire.
		return component.ErrNeedsRestart
	}
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

// ReconcileVolumes brings the live volume set to match desired, keyed
// (case-insensitively) by name. It builds every volume before mutating, so a bad
// spec aborts the whole reconcile leaving the live volumes untouched
// (all-or-nothing). Order follows desired.
func (s *Service) ReconcileVolumes(desired []VolumeSpec) error {
	built := make([]*Volume, 0, len(desired))
	seen := make(map[string]bool, len(desired))
	for _, spec := range desired {
		key := strings.ToLower(spec.Name)
		if seen[key] {
			return share.ErrDuplicateShare
		}
		seen[key] = true
		v, err := NewVolumeWithBus(spec, s.busForSpec(spec.Share))
		if err != nil {
			return err
		}
		built = append(built, v)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vols = built
	return nil
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
	if !s.enabled {
		s.logging.Log0(log.Info, "NCP service disabled; not advertising")
		return nil
	}
	s.subscribeReactorLocked()
	s.logging.Log0(log.Info, "NCP service started (NetWare 3.x bindery; NCP over IPX socket 0x0451)")
	return nil
}

// subscribeReactorLocked attaches the §10d reactor to each distinct FS bus among
// the current volumes. Caller holds s.mu.
func (s *Service) subscribeReactorLocked() {
	if s.busFor == nil || s.reactor == nil {
		return
	}
	seen := make(map[bus.Bus]bool, len(s.vols))
	for _, v := range s.vols {
		b := s.busFor(v.sh.Config())
		if b == nil || seen[b] {
			continue
		}
		seen[b] = true
		s.reactor.Subscribe(b)
	}
}

// Stop brings the service down, tearing down any NCP-owned transports (the
// over-IPX transport) so their connections release file handles. Safe after
// failed/partial Start (§3).
func (s *Service) Stop(ctx context.Context) error {
	_ = ctx
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	closers := append([]circuitCloser(nil), s.closers...)
	reactor := s.reactor
	// Snapshot the live volumes so their backends can be closed after the lock drops.
	// Stop is definitive teardown, so this releases any GC-invisible backend resource
	// (zipfs handles, macgarden goroutine). A plain backend's Close is a no-op.
	vols := append([]*Volume(nil), s.vols...)
	s.mu.Unlock()

	if reactor != nil {
		reactor.Stop()
	}
	for _, c := range closers {
		c.closeCircuits()
	}
	for _, v := range vols {
		_ = v.Close()
	}
	s.logging.Log0(log.Info, "NCP service stopped")
	return nil
}

// compile-time assertions.
var (
	_ component.Component    = (*Service)(nil)
	_ component.Enableable   = (*Service)(nil)
	_ component.Configurable = (*Service)(nil)
	_ share.Manager          = (*Service)(nil)
)
