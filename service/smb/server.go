// Package smb is the SMB 1.0 file-server stub. It is not an AppleTalk
// service and does not consume DDP datagrams; it rides NetBIOS (today
// NBT only — see service/netbios/over_tcp) and exposes file shares
// backed by the shared pkg/vfs registry.
//
// The package is a stub: NewService produces a Service whose Start
// runs a no-op lifecycle, dispatch returns STATUS_NOT_SUPPORTED for
// every SMB command, and the authenticator is a permissive guest stub.
package smb

import (
	"context"
	"errors"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/pkg/shortname"
	"github.com/ObsoleteMadness/ClassicStack/pkg/vfs"
	netbiosproto "github.com/ObsoleteMadness/ClassicStack/protocol/netbios"
	"github.com/ObsoleteMadness/ClassicStack/service/netbios"
)

// ErrNotImplemented is returned by stub call sites that have not
// been filled in.
var ErrNotImplemented = errors.New("smb: not implemented")

// originSMB is the publisher tag used on every vfs.Event the SMB
// server emits, so subscribers (including this one) can filter their
// own events out and avoid feedback loops.
const originSMB = "smb"

// ServerOptions configures the SMB service.
type ServerOptions struct {
	// NBTBinding is the NetBIOS-over-TCP listen address (typically :139).
	NBTBinding string
	// DirectBinding is the SMB-direct (port 445) listen address. Empty
	// disables direct SMB; SMB 1.0 is conventionally NBT-only.
	DirectBinding string
	// GuestOk controls whether unauthenticated sessions are accepted.
	GuestOk bool
	// Workgroup is the announced workgroup/domain name.
	Workgroup string
	// ServerName is the announced NetBIOS server name. Falls back to
	// the NetBIOS service's own name when empty.
	ServerName string
	// Bus, when non-nil, is the VFS event bus the server publishes
	// to and subscribes from. The default is vfs.DefaultBus.
	Bus vfs.Bus
	// Shortname is the optional 8.3 mapper used when responding to
	// legacy DOS/Windows clients. Nil disables shortname mapping.
	Shortname shortname.Mapper
}

// Authenticator validates SMB credentials. The stub permits everyone.
type Authenticator interface {
	Authenticate(user, pass string) error
}

type guestAuth struct{}

func (guestAuth) Authenticate(_, _ string) error { return nil }

// Service is the SMB 1.0 server stub.
type Service struct {
	opts   ServerOptions
	nb     netbios.NameService
	shares []ShareConfig
	auth   Authenticator
	bus    vfs.Bus

	mu          sync.Mutex
	started     bool
	cancelEvent func()
}

// NewService creates a stubbed SMB service. nb may be nil when SMB is
// configured without NetBIOS (e.g. integration tests that drive the
// dispatch path directly). shares may be empty.
func NewService(opts ServerOptions, nb netbios.NameService, shares []ShareConfig) *Service {
	if opts.Bus == nil {
		opts.Bus = vfs.DefaultBus
	}
	return &Service{
		opts:   opts,
		nb:     nb,
		shares: shares,
		auth:   guestAuth{},
		bus:    opts.Bus,
	}
}

// SetAuthenticator overrides the default guest authenticator.
func (s *Service) SetAuthenticator(a Authenticator) {
	if a == nil {
		a = guestAuth{}
	}
	s.mu.Lock()
	s.auth = a
	s.mu.Unlock()
}

// Shares returns the share configs the service was constructed with.
func (s *Service) Shares() []ShareConfig {
	out := make([]ShareConfig, len(s.shares))
	copy(out, s.shares)
	return out
}

// Start brings the SMB service up. It registers a VFS bus subscriber
// so cross-protocol mutations (e.g. an AFP rename inside a shared
// volume) can invalidate SMB-side caches.
func (s *Service) Start(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return nil
	}
	s.cancelEvent = s.bus.Subscribe(&shareEventSubscriber{shares: s.shares})
	s.started = true
	return nil
}

// Stop tears the service down.
func (s *Service) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		return nil
	}
	if s.cancelEvent != nil {
		s.cancelEvent()
		s.cancelEvent = nil
	}
	s.started = false
	return nil
}

// HandleSession implements netbios.CommandHandler. The stub rejects
// every inbound session-layer SMB request as not implemented.
func (s *Service) HandleSession(_ *netbiosproto.SessionPacket) error { return ErrNotImplemented }

// HandleDatagram implements netbios.CommandHandler.
func (s *Service) HandleDatagram(_ *netbiosproto.Datagram) error { return ErrNotImplemented }

// shareEventSubscriber is the VFS bus subscriber installed by Start.
// It will (when implemented) match HostPath against share roots and
// invalidate any open handle whose backing path was renamed/deleted.
type shareEventSubscriber struct {
	shares []ShareConfig
}

// OnVFSEvent implements vfs.Subscriber.
func (s *shareEventSubscriber) OnVFSEvent(ev vfs.Event) {
	if ev.Origin == originSMB {
		return
	}
	// Stub: real invalidation lands with the open-handle map.
	_ = s.shares
}
