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

// namedTransport pairs a Transport with the operator-facing name the
// supervisor binds it under (e.g. "ipx", "netbeui"), so transports can be
// added and removed at runtime as their underlying protocol is started or
// stopped from the UI.
type namedTransport struct {
	name string
	t    Transport
}

// Service composes a set of transports under a common NetBIOS name.
type Service struct {
	serverName string
	scopeID    string
	transports []namedTransport
	names      map[protocol.Name]struct{}

	mu      sync.Mutex
	started bool
	ctx     context.Context // start context, captured in Start for late AddTransport
	handler CommandHandler
}

// NewService creates a NetBIOS service whose name layer is reachable
// over the given transports. transports may be empty for a name-only
// service that does not accept incoming sessions. Transports passed here
// are bound under positional names ("t0", "t1", …); callers that need
// removable, named transports should pass nil and use AddTransport.
func NewService(serverName, scopeID string, transports []Transport) *Service {
	defaultNames := map[protocol.Name]struct{}{}
	if serverName != "" {
		defaultNames[protocol.NewName(serverName, protocol.NameTypeFileServer)] = struct{}{}
		defaultNames[protocol.NewName(serverName, protocol.NameTypeWorkstation)] = struct{}{}
	}
	named := make([]namedTransport, 0, len(transports))
	for i, t := range transports {
		named = append(named, namedTransport{name: fmt.Sprintf("t%d", i), t: t})
	}
	return &Service{
		serverName: serverName,
		scopeID:    scopeID,
		transports: named,
		names:      defaultNames,
	}
}

// transportList returns a snapshot of the current Transport values, dropping
// the names. Callers must not hold s.mu (it takes the lock).
func (s *Service) transportList() []Transport {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Transport, 0, len(s.transports))
	for _, nt := range s.transports {
		out = append(out, nt.t)
	}
	return out
}

// snapshotNames returns the registered NetBIOS names. Callers must hold s.mu.
func (s *Service) snapshotNamesLocked() []protocol.Name {
	names := make([]protocol.Name, 0, len(s.names))
	for n := range s.names {
		names = append(names, n)
	}
	return names
}

// SetCommandHandler installs an inbound-command handler (typically an
// SMB server). Idempotent; later calls replace earlier ones. Each
// transport receives the handler so it can deliver decoded packets.
func (s *Service) SetCommandHandler(h CommandHandler) {
	s.mu.Lock()
	s.handler = h
	for _, nt := range s.transports {
		nt.t.SetCommandHandler(h)
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
	s.ctx = ctx
	transports := make([]Transport, 0, len(s.transports))
	for _, nt := range s.transports {
		transports = append(transports, nt.t)
	}
	names := s.snapshotNamesLocked()
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
		for _, n := range names {
			if err := t.SendName(n); err != nil && !errors.Is(err, ErrNotImplemented) {
				for j := range i + 1 {
					_ = transports[j].Stop()
				}
				s.mu.Lock()
				s.started = false
				s.mu.Unlock()
				return fmt.Errorf("netbios: register name %q: %w", n.String(), err)
			}
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
	transports := make([]Transport, 0, len(s.transports))
	for _, nt := range s.transports {
		transports = append(transports, nt.t)
	}
	s.mu.Unlock()
	for _, t := range transports {
		_ = t.Stop()
	}
	return nil
}

// AddTransport binds t under name. If the service is already started, t is
// wired with the current command handler, started, and given the registered
// names — so a transport whose underlying protocol comes up after NetBIOS
// (e.g. NetBEUI started from the UI) joins the live service. Re-adding an
// existing name replaces the prior transport (the old one is left as-is;
// callers RemoveTransport first if they need it stopped).
func (s *Service) AddTransport(name string, t Transport) error {
	if t == nil {
		return fmt.Errorf("netbios: nil transport for %q", name)
	}
	s.mu.Lock()
	// Replace any existing binding with the same name, stopping the old
	// transport so it does not leak its goroutine/socket registrations.
	var replaced Transport
	for i, nt := range s.transports {
		if nt.name == name {
			replaced = nt.t
			s.transports[i].t = t
			goto bind
		}
	}
	s.transports = append(s.transports, namedTransport{name: name, t: t})
bind:
	handler := s.handler
	started := s.started
	ctx := s.ctx
	names := s.snapshotNamesLocked()
	s.mu.Unlock()

	if replaced != nil && replaced != t {
		_ = replaced.Stop()
	}

	if handler != nil {
		t.SetCommandHandler(handler)
	}
	if !started {
		return nil
	}
	if err := t.Start(ctx); err != nil {
		return fmt.Errorf("netbios: start transport %q: %w", name, err)
	}
	for _, n := range names {
		if err := t.SendName(n); err != nil && !errors.Is(err, ErrNotImplemented) {
			return fmt.Errorf("netbios: register name %q on %q: %w", n.String(), name, err)
		}
	}
	return nil
}

// RemoveTransport stops and unbinds the transport registered under name.
// It is idempotent: removing an unknown name is a no-op. The rest of the
// service (other transports, the name layer) keeps running, so stopping one
// underlying protocol detaches only its binding.
func (s *Service) RemoveTransport(name string) error {
	s.mu.Lock()
	var found Transport
	kept := s.transports[:0]
	for _, nt := range s.transports {
		if nt.name == name && found == nil {
			found = nt.t
			continue
		}
		kept = append(kept, nt)
	}
	s.transports = kept
	s.mu.Unlock()

	if found == nil {
		return nil
	}
	return found.Stop()
}

// Transports returns the names of the currently bound transports, in bind
// order, for status reporting.
func (s *Service) Transports() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.transports))
	for _, nt := range s.transports {
		out = append(out, nt.name)
	}
	return out
}

// SendDatagram broadcasts a NetBIOS datagram through every active
// transport. If one or more transports fail, the first error is
// returned after attempting all sends.
func (s *Service) SendDatagram(d *protocol.Datagram) error {
	transports := s.transportList()

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
	transports := s.transportList()

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

// Register implements NameService by registering the given name as a
// file-server NetBIOS name on all transports.
func (s *Service) Register(name string) error {
	n := protocol.NewName(name, protocol.NameTypeFileServer)

	s.mu.Lock()
	if s.names == nil {
		s.names = map[protocol.Name]struct{}{}
	}
	s.names[n] = struct{}{}
	started := s.started
	transports := make([]Transport, 0, len(s.transports))
	for _, nt := range s.transports {
		transports = append(transports, nt.t)
	}
	s.mu.Unlock()

	if !started {
		return nil
	}
	for _, t := range transports {
		if err := t.SendName(n); err != nil && !errors.Is(err, ErrNotImplemented) {
			return fmt.Errorf("netbios: register name %q: %w", n.String(), err)
		}
	}
	return nil
}

// Resolve implements NameService (stub).
func (s *Service) Resolve(_ string) (string, error) { return "", ErrNotImplemented }

// Release removes the name from this service's local registration set.
// Transport-level remove/release is not yet implemented.
func (s *Service) Release(name string) error {
	n := protocol.NewName(name, protocol.NameTypeFileServer)
	s.mu.Lock()
	delete(s.names, n)
	s.mu.Unlock()
	return nil
}
