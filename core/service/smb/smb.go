// Package smb is the SMB/CIFS file service re-expressed over the §9 storage seam.
// Its Shares consume only the core/fs (FileSystem + ForkEngine + FilenameCodec)
// interfaces, so the service holds no storage-layout knowledge. The filename wire
// charset is threaded per request from the FLAGS2 Unicode bit (UTF-16 vs the
// OEM/ANSI page, §2a) — keyed off the flag, not the dialect, so SMB 1.0 clients
// that set SMB_FLAGS2_UNICODE get UTF-16. A same-fs_type AFP volume and
// SMB share see the same forks and FinderInfo through the same ForkEngine.
//
// As of M7 the SMB1 dispatch is wired as a transport-independent spine plus an FS
// command engine: Service.Dispatch decodes one SMB message and handles the
// session-establishment commands — NEGOTIATE (accepting NT LM 0.12),
// SESSION_SETUP_ANDX (granting a guest session), TREE_CONNECT[_ANDX] (binding a
// TID to a *Share or the virtual IPC$ pipe), TREE_DISCONNECT, LOGOFF_ANDX, ECHO —
// and the filesystem commands over the bound *Share's FS: OPEN[_ANDX]/CREATE,
// READ[_ANDX]/WRITE[_ANDX], CLOSE/FLUSH, DELETE/RENAME,
// CREATE_DIRECTORY/DELETE_DIRECTORY/CHECK_DIRECTORY, QUERY_INFORMATION[_DISK], and
// the TRANS2 FIND_FIRST2/FIND_NEXT2/FIND_CLOSE2 + QUERY_PATH/FILE_INFO
// subcommands. Each FS command resolves its wire path through the share codec and
// acts via sh.FS(), so the engine holds no storage-layout knowledge; RENAME and
// DELETE ride the metadata-carrying FS().Rename/Remove. NT_CREATE_ANDX serves the
// NT/2000/XP open-or-create path (files and directories) over the same seam. The
// remaining recognised commands (the byte-range locking/MPX/raw paths) answer
// STATUS_NOT_SUPPORTED.
// The dispatch is driven by a transport-agnostic session seam: the SMB Service
// exposes one virtual circuit per session through NewConn (conn.go), and a session
// transport hands it each whole SMB message via Conn.ServeMessage (which wraps
// Dispatch over a per-circuit smbSession), Conn.Close on teardown. Transports come
// in two families and SMB does not distinguish them: NetBIOS-based (NBF/NBIPX/NBT
// — the NetBIOS engines reassemble off the session circuit) and DIRECT/NetBIOS-less
// (SMB direct-hosted over IPX socket 0x0550 — directipx.go, registered on the IPX
// mini-router; direct-TCP :445 — an adapter). The spine itself holds no transport
// knowledge, so it is unit-tested directly over raw SMB frames.
//
// Security posture: this is a compatibility server, not an authentication
// server. With no user store wired, SESSION_SETUP_ANDX grants a guest session
// without checking credentials (the intentional weakness that lets vintage
// clients connect) and every share is world-accessible. With a user store wired
// (SetAuthenticator), a non-empty AccountName with a CLEARTEXT password is
// validated; a per-share allow-list then gates which shares the resulting
// identity may enumerate and bind (login-time gating — legacy clients log in once
// and bind shares under one identity, they do not re-authenticate per share). A
// legacy client sending an LM/NTLM hash we cannot reverse is accepted as guest
// (spec/errata "SMB hashed-credential accept-as-guest").
package smb

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

// Name is the component name for the SMB service.
const Name = "SMB"

// OriginSMB tags FS-mutation events this service produces on the shared §10d FS
// bus, so a same-host-path AFP volume's reactor acts on them and SMB's own reactor
// (fs.SkipOrigin) ignores them.
const OriginSMB = "smb"

// Service is the SMB component. As of M7 it owns a set of Shares built over the
// §9 storage seam (fs.ForkFS + FilenameCodec) and holds no storage-layout
// knowledge itself. The protocol dispatch (NBT/NetBEUI/IPX → SMB command engine)
// is still a thin stub at this milestone; the service shape that drives the seam
// is what lands here.
type Service struct {
	logger  log.Logger
	shares  []*Share
	server  string         // server name (the §4-bis identity hostname); default CLASSICSTACK
	desc    string         // server comment/remark (the §4-bis identity description); optional
	wg      string         // workgroup/domain advertised in NEGOTIATE (default WORKGROUP)
	browser BrowseProvider // browse-list source for IPC$ NetServerEnum2 (the browser service); optional
	auth    Authenticator  // credential validator consulted at SESSION_SETUP; nil = guest-only

	mu       sync.Mutex
	running  bool
	closers  []circuitCloser             // SMB-owned session transports (e.g. direct-IPX); torn down on Stop
	resolver func() ([]ShareSpec, error) // re-resolves the desired share set from the model; set at wire time for hot-apply
	busFor   func(fs.ShareSpec) bus.Bus  // resolves the shared FS-mutation bus for a share's host path (§10d); nil = isolated
	reactor  *share.Reactor              // §10d coordination consumer; subscribes to same-path buses on Start
	sessions map[*smbSession]struct{}    // live circuits, for delivering async NOTIFY_CHANGE completions (§10d push)
	// bound is the transport families the operator bound (from the SMB server section),
	// stored so the service DECLARES its own transport intent (BoundTransports) and
	// dependency edges (Dependencies) — the compose root asks the service instead of
	// re-reading the model. Empty means "bind every built transport" (the historical
	// implicit default), matching ServerSection.Binds.
	bound []string
	// tcpAddr is the explicit direct-TCP (:445) listen address from the SMB server
	// section, held so the compose root reads the TCP transport's address from the
	// SERVICE (§B) rather than the section. Empty = do not bind (never an implicit :445 —
	// Windows owns it). The NBT (:139) address is NOT here: NBT is a NetBIOS transport,
	// so its address lives on the NetBIOS service (netbios.Service.NBTListenAddr).
	tcpAddr string
}

// Authenticator validates a (username, cleartext password) credential. It is the
// minimal seam the SMB session-setup path needs; the compose wiring hands in the
// configured user store (core/auth). It is a LOCAL interface — structurally
// satisfied by auth.UserStore — so this package does not import core/auth (the
// same acyclicity discipline as the BrowseProvider seam). A nil Authenticator
// means guest-only: every session is granted as guest, exactly as before this
// seam existed.
type Authenticator interface {
	Authenticate(username, password string) (ok bool, err error)
}

// SetAuthenticator installs the credential validator SESSION_SETUP_ANDX consults.
// Passing nil restores guest-only behaviour. Idempotent; safe before Start.
func (s *Service) SetAuthenticator(a Authenticator) {
	s.mu.Lock()
	s.auth = a
	s.mu.Unlock()
}

// SetBoundTransports records the transport families the operator bound (the SMB server
// section's list), so the service can DECLARE its own transport intent and dependency
// edges. Empty (or nil) keeps the implicit "bind every built transport" default. The
// compose root sets this from the section once at build time; idempotent, safe before
// Start.
func (s *Service) SetBoundTransports(transports []string) {
	s.mu.Lock()
	s.bound = append([]string(nil), transports...)
	s.mu.Unlock()
}

// BoundTransports returns the transport families this service wants bound
// (component.TransportBinder), so the compose root wires only those without re-reading
// the SMB server section. An empty result means "every built transport" (implicit
// default).
func (s *Service) BoundTransports() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.bound...)
}

// SetDirectTCPListenAddr records the explicit direct-TCP (:445) listen address from the
// server section, so the compose root reads it from the service (§B). Empty means "do not
// bind" (never an implicit :445 — Windows owns it). The NBT (:139) address is NOT an SMB
// concern: it lives on the NetBIOS service. Idempotent, safe before Start.
func (s *Service) SetDirectTCPListenAddr(tcpAddr string) {
	s.mu.Lock()
	s.tcpAddr = tcpAddr
	s.mu.Unlock()
}

// DirectTCPListenAddr returns the explicit direct-TCP (:445) listen address, or "" when
// none was configured (the transport then stays inert — no implicit default).
func (s *Service) DirectTCPListenAddr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tcpAddr
}

// Binds reports whether transport is bound: an empty bound list binds everything (the
// historical default), else the list must name it. Mirrors ServerSection.Binds so the
// service and the section agree; the compose transport cross-wire gates each family by
// asking the SERVICE this, not by re-reading the section (§B).
func (s *Service) Binds(transport string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.bound) == 0 {
		return true
	}
	for _, t := range s.bound {
		if t == transport {
			return true
		}
	}
	return false
}

// Dependencies declares SMB's start-order edges. SMB depends on NetBEUI ONLY when the
// NetBEUI transport is bound — the config-varying edge the static composition-root map
// could not express (it listed the edge unconditionally and relied on the built-both-
// ends filter). The edge still drops when NetBEUI was not built.
func (s *Service) Dependencies() []string {
	if s.Binds(TransportNetBEUI) {
		return []string{netbeuiComponentName}
	}
	return nil
}

// netbeuiComponentName is the component name of the NetBEUI port family SMB orders
// after. Matched by string (not an import) to avoid a service→port dependency, the same
// discipline the compose root and control plane use for cross-component name references.
const netbeuiComponentName = "NetBEUI"

// circuitCloser is the per-transport surface the SMB service holds for teardown:
// a transport SMB owns directly (the direct-hosted-over-IPX transport, which is
// not a NetBIOS transport and so is not torn down by the NetBIOS service) closes
// its open circuits on Stop so no file handles leak. *DirectIPX satisfies it.
type circuitCloser interface{ closeCircuits() }

// New builds the SMB service with no shares (the registry default).
func New(logger log.Logger) *Service {
	s := &Service{logger: logger, sessions: make(map[*smbSession]struct{})}
	// §10d reactor: deliver foreign-origin FS mutations under one of our shares to
	// the SMB wire-push sink (notifyFSChange), which completes any held NT_TRANSACT
	// NOTIFY_CHANGE for that share. shareRoots() re-reads the live share set per event
	// so a reconcile is reflected without re-subscribing.
	s.reactor = share.NewReactor(OriginSMB, s.shareRoots, s.notifyFSChange)
	return s
}

// ReactorDelivered reports how many foreign-origin FS mutations the §10d reactor has
// delivered (a same-host-path AFP volume's writes this SMB service was notified of).
// Diagnostics / tests; 0 until a cross-service mutation occurs.
func (s *Service) ReactorDelivered() uint64 {
	if s.reactor == nil {
		return 0
	}
	return s.reactor.Delivered()
}

// SessionInfo is a point-in-time snapshot of one live SMB circuit for the
// management/diagnostics view: which client (transport remote endpoint) opened it,
// the authenticated identity ("" = guest), the SMB dialect it negotiated, when it
// negotiated, and how many trees/files it currently holds open.
type SessionInfo struct {
	Client       string    // transport remote-endpoint label; "" when the transport supplied none
	User         string    // authenticated identity from SESSION_SETUP; "" = guest
	Dialect      string    // negotiated SMB dialect string; "" before NEGOTIATE
	NegotiatedAt time.Time // when NEGOTIATE completed; zero before then
	OpenTrees    int       // bound tree connects (TREE_CONNECT)
	OpenFiles    int       // open file handles (FID)
}

// Sessions snapshots every live SMB circuit for the management view, grouped
// implicitly by SessionInfo.Client (a client with two circuits appears twice, one
// per circuit). Order is unspecified (map iteration).
func (s *Service) Sessions() []SessionInfo {
	sessions := s.liveSessions()
	out := make([]SessionInfo, 0, len(sessions))
	for _, sess := range sessions {
		out = append(out, sess.info())
	}
	return out
}

// registerSession adds a live circuit's session to the push set (called from
// NewConn) so an async NOTIFY_CHANGE completion can reach it.
func (s *Service) registerSession(sess *smbSession) {
	s.mu.Lock()
	if s.sessions == nil {
		s.sessions = make(map[*smbSession]struct{})
	}
	s.sessions[sess] = struct{}{}
	s.mu.Unlock()
}

// unregisterSession drops a session from the push set (called from Conn.Close).
func (s *Service) unregisterSession(sess *smbSession) {
	s.mu.Lock()
	delete(s.sessions, sess)
	s.mu.Unlock()
}

// liveSessions snapshots the registered sessions for a §10d push fan-out.
func (s *Service) liveSessions() []*smbSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*smbSession, 0, len(s.sessions))
	for sess := range s.sessions {
		out = append(out, sess)
	}
	return out
}

// shareRoots returns the live shares as (name, host-root) pairs for the §10d
// reactor's path matching.
func (s *Service) shareRoots() []share.NamedPath {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]share.NamedPath, 0, len(s.shares))
	for _, sh := range s.shares {
		out = append(out, share.NamedPath{Name: sh.Name(), Root: sh.sh.Config().Path, FS: sh.FS()})
	}
	return out
}

// NewWithShares builds the SMB service over a set of share specs, constructing
// one Share per spec through the storage seam. An invalid triple fails the build
// loudly here rather than mangling names at runtime.
func NewWithShares(logger log.Logger, specs ...ShareSpec) (*Service, error) {
	s := New(logger)
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

// SetWorkgroup sets the workgroup/domain advertised in the NEGOTIATE response.
// The compose/config layer calls it during wiring; unset defaults to WORKGROUP.
func (s *Service) SetWorkgroup(wg string) {
	s.mu.Lock()
	s.wg = wg
	s.mu.Unlock()
}

// SetServerName sets the server name SMB reports for itself (the §4-bis identity
// hostname). The compose registry hands it the one Identity.Hostname; SMB does not
// own or default it beyond a fallback. Unset defaults to CLASSICSTACK. Idempotent.
func (s *Service) SetServerName(name string) {
	s.mu.Lock()
	s.server = name
	s.mu.Unlock()
}

// SetDescription sets the server comment/remark (the §4-bis identity description) SMB
// reports for itself — the comment a Windows browse list shows next to the server.
// Empty = no comment. Idempotent.
func (s *Service) SetDescription(desc string) {
	s.mu.Lock()
	s.desc = desc
	s.mu.Unlock()
}

// serverName returns the configured server name, defaulting to CLASSICSTACK when
// unset (the same fallback the browser uses, so a NetBIOS-less :445-only deployment
// still reports a name).
func (s *Service) serverName() string {
	s.mu.Lock()
	name := s.server
	s.mu.Unlock()
	if name != "" {
		return name
	}
	return "CLASSICSTACK"
}

// description returns the configured server comment (may be empty).
func (s *Service) description() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.desc
}

// ShareByName returns the share with the given tree name, if bound. Used by the
// tree-connect dispatch; guarded because the Manager mutates the slice at runtime.
func (s *Service) ShareByName(name string) (*Share, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.findShareLocked(name)
}

// findShareLocked returns the share of that name; caller holds s.mu. SMB share
// names are case-insensitive, so the match folds case — a client connecting to
// \\server\SHARED finds the share configured as "Shared".
func (s *Service) findShareLocked(name string) (*Share, bool) {
	for _, sh := range s.shares {
		if strings.EqualFold(sh.Name(), name) {
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
// The share is built over the shared FS-mutation bus for its host path (§10d) when a
// bus resolver is wired.
func (s *Service) AddShare(spec fs.ShareSpec) error {
	built, err := share.Build(spec, s.busForSpec(spec))
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
	built, err := share.Build(spec, s.busForSpec(spec))
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

// --- component.Configurable: hot-apply a changed share set without restart ---

// SetShareResolver installs the closure the supervisor's Reconfigure consults to
// re-resolve the desired share set from the (already-updated) shared model. The
// compose registry supplies it (a closure over SpecsFromModel(model)); without it
// ApplyConfig reports ErrNeedsRestart so the supervisor falls back to a full
// rebuild. Idempotent; safe before Start.
func (s *Service) SetShareResolver(resolve func() ([]ShareSpec, error)) {
	s.mu.Lock()
	s.resolver = resolve
	s.mu.Unlock()
}

// SetBusResolver installs the closure that maps a share's spec to the shared
// FS-mutation bus for its host path (§10d). The compose registry supplies it (one
// bus per distinct host path, shared with a same-path AFP volume) so a mutation by
// one service reaches the other. A nil resolver (or one returning nil) means each
// share gets a private bus — no cross-service coordination. Idempotent; safe before
// Start. Affects shares built after it is set (AddShare / a reconcile / a rebuild).
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

// ApplyConfig hot-applies a changed share set (§11b): the SMB "config" is the set of
// repeated share sections (config.Model.Lists[SharesKey]), not a singleton section,
// so the passed section payload is ignored — ApplyConfig re-resolves the whole
// desired set from the model and reconciles it against the live shares via the
// share.Manager (Add new, Update changed, Remove dropped). A share's fs-type/backend
// change is absorbed by UpdateShare rebuilding that one share's stack — no service
// restart, and in-flight tree connects are undisturbed. When no resolver is wired it
// returns ErrNeedsRestart so the supervisor falls back to the rebuild path.
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
	return s.ReconcileShares(desired)
}

// ReconcileShares brings the live share set to match desired, keyed (case-insensitively,
// as tree-connect matches) by share name: a name present only in desired is added, one
// present in both is updated (rebuilding its stack), one present only live is removed.
// It builds every share before mutating, so a bad spec in the set aborts the whole
// reconcile leaving the live shares untouched (all-or-nothing). Order of the surviving
// shares follows desired.
func (s *Service) ReconcileShares(desired []ShareSpec) error {
	// Build the full desired set first (outside the service lock) so a bad
	// triple/param fails before anything is swapped in.
	built := make([]*Share, 0, len(desired))
	seen := make(map[string]bool, len(desired))
	for _, spec := range desired {
		key := strings.ToLower(spec.Name)
		if seen[key] {
			return share.ErrDuplicateShare
		}
		seen[key] = true
		sh, err := NewShareWithBus(spec, s.busForSpec(spec.Share))
		if err != nil {
			return err
		}
		built = append(built, sh)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shares = built
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
	s.logf("SMB service started (shares bound; session-establishment dispatch: negotiate/setup/treeconnect)")
	return nil
}

// subscribeReactorLocked attaches the §10d reactor to each distinct FS bus among the
// current shares (compose hands one bus per host path, so two shares on one path
// resolve to one bus — subscribed once). Caller holds s.mu. A no-op when no bus
// resolver is wired (every share is isolated).
func (s *Service) subscribeReactorLocked() {
	if s.busFor == nil || s.reactor == nil {
		return
	}
	seen := make(map[bus.Bus]bool, len(s.shares))
	for _, sh := range s.shares {
		b := s.busFor(sh.sh.Config())
		if b == nil || seen[b] {
			continue
		}
		seen[b] = true
		s.reactor.Subscribe(b)
	}
}

// Stop brings the service down, tearing down any SMB-owned session transports
// (the direct-IPX transport) so their open circuits release file handles. Safe
// after failed/partial Start (§3).
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
	// Snapshot the live shares so their backends can be closed after the lock drops.
	// Stop is definitive teardown (no session can still hold a share), so closing each
	// share's FS here releases any GC-invisible backend resource (zipfs handles,
	// macgarden goroutine). A plain backend's Close is a no-op.
	shares := append([]*Share(nil), s.shares...)
	s.mu.Unlock()

	if reactor != nil {
		reactor.Stop()
	}
	for _, c := range closers {
		c.closeCircuits()
	}
	for _, sh := range shares {
		_ = sh.Close()
	}
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
	_ component.Component       = (*Service)(nil)
	_ component.Configurable    = (*Service)(nil)
	_ component.DependsOn       = (*Service)(nil)
	_ component.TransportBinder = (*Service)(nil)
	_ share.Manager             = (*Service)(nil)
)
