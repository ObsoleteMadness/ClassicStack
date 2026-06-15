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
	logger  log.Logger
	shares  []*Share
	wg      string         // workgroup/domain advertised in NEGOTIATE (default WORKGROUP)
	browser BrowseProvider // browse-list source for IPC$ NetServerEnum2 (the browser service); optional
	auth    Authenticator  // credential validator consulted at SESSION_SETUP; nil = guest-only

	mu      sync.Mutex
	running bool
	closers []circuitCloser // SMB-owned session transports (e.g. direct-IPX); torn down on Stop
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

// circuitCloser is the per-transport surface the SMB service holds for teardown:
// a transport SMB owns directly (the direct-hosted-over-IPX transport, which is
// not a NetBIOS transport and so is not torn down by the NetBIOS service) closes
// its open circuits on Stop so no file handles leak. *DirectIPX satisfies it.
type circuitCloser interface{ closeCircuits() }

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

// SetWorkgroup sets the workgroup/domain advertised in the NEGOTIATE response.
// The compose/config layer calls it during wiring; unset defaults to WORKGROUP.
func (s *Service) SetWorkgroup(wg string) {
	s.mu.Lock()
	s.wg = wg
	s.mu.Unlock()
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
	s.logf("SMB service started (shares bound; session-establishment dispatch: negotiate/setup/treeconnect)")
	return nil
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
	s.mu.Unlock()

	for _, c := range closers {
		c.closeCircuits()
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
	_ component.Component = (*Service)(nil)
	_ share.Manager       = (*Service)(nil)
)
