// Package netbios is the NetBIOS session/name layer. It is transport-
// pluggable: any number of Transport implementations (NetBEUI, IPX,
// TCP/NBT) can be wired into a single Service, mirroring AFP's
// multi-transport design.
//
// NetBIOS is not an AppleTalk service: it does not consume DDP
// datagrams and is not registered with the AppleTalk router. The
// lifecycle contract here is a plain Start(ctx)/Stop pair so main.go
// can drive it independently.
package netbios

import (
	"context"
	"errors"
	"fmt"
	"sync"

	protocol "github.com/ObsoleteMadness/ClassicStack/protocol/netbios"
)

// ErrNotImplemented is returned by stub call sites that have not yet
// been filled in.
var ErrNotImplemented = errors.New("netbios: not implemented")

// CommandHandler receives decoded NetBIOS commands from a Transport.
// SMB plugs in here.
type CommandHandler interface {
	HandleSession(packet *protocol.SessionPacket) error
	HandleDatagram(d *protocol.Datagram) error
}

// DatagramEndpoint identifies a transport-level remote endpoint for
// a NetBIOS datagram.
type DatagramEndpoint struct {
	Network [4]byte
	Node    [6]byte
	Socket  [2]byte
}

// DatagramContext carries transport metadata for an inbound NetBIOS
// datagram when the underlying transport can provide it.
type DatagramContext struct {
	Local  DatagramEndpoint
	Remote DatagramEndpoint
}

// SessionContext carries transport metadata for an inbound NetBIOS
// session message when the underlying transport can provide it.
type SessionContext struct {
	Local         DatagramEndpoint
	Remote        DatagramEndpoint
	SourceConnID  uint16
	DestConnID    uint16
	Sequence      uint16
	ConnectionCtl uint8
}

// ContextualDatagramHandler is an optional extension implemented by
// handlers that need transport metadata for reply routing.
type ContextualDatagramHandler interface {
	HandleDatagramContext(d *protocol.Datagram, ctx DatagramContext) error
}

// ContextualSessionHandler is an optional extension implemented by
// handlers that need transport metadata and/or need to return a
// session-layer response packet.
type ContextualSessionHandler interface {
	HandleSessionContext(packet *protocol.SessionPacket, ctx SessionContext) (*protocol.SessionPacket, error)
}

// DirectedDatagramTransport is implemented by transports that can
// route a NetBIOS datagram back to a specific remote endpoint.
type DirectedDatagramTransport interface {
	SendDirectedDatagram(d *protocol.Datagram, remote DatagramEndpoint) error
}

// Transport is the per-link NetBIOS transport contract. A NetBIOS
// service may run multiple transports concurrently (NBT for TCP/IP
// clients, NetBEUI for legacy LAN, IPX for Novell-era clients).
type Transport interface {
	Start(ctx context.Context) error
	Stop() error
	SendName(name protocol.Name) error
	SendDatagram(d *protocol.Datagram) error
	SendSession(s *protocol.SessionPacket) error
	SetCommandHandler(handler CommandHandler)
}

// NameService is the registration/resolution surface SMB consumes to
// claim its server name and to look up remote names for outgoing
// connections.
type NameService interface {
	Register(name string) error
	Resolve(name string) (string, error)
	Release(name string) error
}

// Service composes a set of transports under a common NetBIOS name.
type Service struct {
	serverName string
	scopeID    string
	transports []Transport

	mu      sync.Mutex
	started bool
	handler CommandHandler
}

// NewService creates a NetBIOS service whose name layer is reachable
// over the given transports. transports may be empty for a name-only
// service that does not accept incoming sessions.
func NewService(serverName, scopeID string, transports []Transport) *Service {
	return &Service{
		serverName: serverName,
		scopeID:    scopeID,
		transports: transports,
	}
}

// SetCommandHandler installs an inbound-command handler (typically an
// SMB server). Idempotent; later calls replace earlier ones. Each
// transport receives the handler so it can deliver decoded packets.
func (s *Service) SetCommandHandler(h CommandHandler) {
	s.mu.Lock()
	s.handler = h
	for _, t := range s.transports {
		t.SetCommandHandler(h)
	}
	s.mu.Unlock()
}

// Start brings up every transport. If any transport fails to start
// the already-started ones are torn down before returning the error.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return nil
	}
	s.started = true
	transports := append([]Transport(nil), s.transports...)
	s.mu.Unlock()
	for i, t := range transports {
		if err := t.Start(ctx); err != nil {
			for j := range i {
				_ = transports[j].Stop()
			}
			s.mu.Lock()
			s.started = false
			s.mu.Unlock()
			return err
		}
	}
	return nil
}

// Stop tears down every transport. Errors from individual transports
// are swallowed so a single failing transport does not block teardown
// of its siblings.
func (s *Service) Stop() error {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return nil
	}
	s.started = false
	transports := append([]Transport(nil), s.transports...)
	s.mu.Unlock()
	for _, t := range transports {
		_ = t.Stop()
	}
	return nil
}

// SendDatagram broadcasts a NetBIOS datagram through every active
// transport. If one or more transports fail, the first error is
// returned after attempting all sends.
func (s *Service) SendDatagram(d *protocol.Datagram) error {
	s.mu.Lock()
	transports := append([]Transport(nil), s.transports...)
	s.mu.Unlock()

	var firstErr error
	for _, t := range transports {
		if err := t.SendDatagram(d); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return fmt.Errorf("netbios: send datagram: %w", firstErr)
	}
	return nil
}

// SendDirectedDatagram sends a NetBIOS datagram back to a specific
// remote endpoint through each transport that supports directed
// delivery. ErrNotImplemented is returned when no configured
// transport exposes directed routing.
func (s *Service) SendDirectedDatagram(d *protocol.Datagram, remote DatagramEndpoint) error {
	s.mu.Lock()
	transports := append([]Transport(nil), s.transports...)
	s.mu.Unlock()

	var firstErr error
	attempted := false
	for _, t := range transports {
		dt, ok := t.(DirectedDatagramTransport)
		if !ok {
			continue
		}
		attempted = true
		if err := dt.SendDirectedDatagram(d, remote); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return fmt.Errorf("netbios: send directed datagram: %w", firstErr)
	}
	if !attempted {
		return ErrNotImplemented
	}
	return nil
}

// NameService returns the NameService surface backed by this service.
// The current implementation is a stub.
func (s *Service) NameService() NameService { return s }

// Register implements NameService (stub).
func (s *Service) Register(_ string) error { return ErrNotImplemented }

// Resolve implements NameService (stub).
func (s *Service) Resolve(_ string) (string, error) { return "", ErrNotImplemented }

// Release implements NameService (stub).
func (s *Service) Release(_ string) error { return ErrNotImplemented }
