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
const defaultServerName = "OMNITALK"

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
	auth    Authenticator

	conns *connTable

	mu        sync.Mutex
	running   bool
	closers   []circuitCloser              // NCP-owned transports (over-IPX); torn down on Stop
	resolver  func() ([]VolumeSpec, error) // re-resolves the desired volume set from the model; set at wire time for hot-apply
	busFor    func(fs.ShareSpec) bus.Bus   // resolves the shared FS-mutation bus for a volume's host path (§10d); nil = isolated
	reactor   *share.Reactor               // §10d coordination consumer
	statsSink func(component.Stats)        // §5 push sink; nil = poll-only
	rxObs     func(rx, tx int)             // §5 traffic observer; nil = unmetered

	counters counters // monotonic protocol counters (guarded by mu)
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
	s := &Service{logging: logger, conns: newConnTable()}
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
// identity hostname, upper-cased). Unset defaults to OMNITALK. Idempotent.
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

// serverName returns the configured server name, defaulting to OMNITALK.
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

// ApplyConfig hot-applies a changed volume set (§11b): the NCP "config" is the set
// of repeated volume sections, so the passed section payload is ignored —
// ApplyConfig re-resolves the whole desired set from the model and reconciles it
// against the live volumes. When no resolver is wired it returns ErrNeedsRestart.
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
	_ component.Configurable = (*Service)(nil)
	_ share.Manager          = (*Service)(nil)
)
