// Package netbios is the NetBIOS name/session layer that SMB rides. It is
// transport-pluggable: NetBEUI, IPX, and NBT transports attach as SOFT bindings
// (component.Attachable, §11d) rather than hard dependencies, so a transport
// whose underlying protocol starts after NetBIOS joins the live service and
// stopping that protocol detaches only its binding. As of M7 the session/datagram
// command dispatch is a thin stub; what lands here is the binding shape.
package netbios

import (
	"context"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
)

// Name is the component name for the NetBIOS service.
const Name = "NetBIOS"

// Transport is the per-link NetBIOS transport contract. A transport carries
// NetBIOS name/datagram/session traffic over one underlying protocol (NBT over
// TCP, NetBEUI over Ethernet, IPX). It is brought up and down by the NetBIOS
// service as a SOFT binding (§11d) — not a hard dependency — so a transport
// whose underlying protocol starts after NetBIOS (e.g. NetBEUI enabled from the
// UI) can attach to the already-running service, and stopping that protocol
// detaches only its binding without tearing down the rest.
type Transport interface {
	// Open brings the transport up. Called when the binding attaches.
	Open(ctx context.Context) error
	// Close brings the transport down. Called when the binding detaches.
	Close() error
	// Announce claims a NetBIOS name on the transport's network.
	Announce(name protocol.Name) error
}

// binding pairs a Transport with the operator-facing name it is bound under
// ("netbeui", "ipx", "nbt") and tracks whether it is currently attached, so the
// service can attach/detach it idempotently as its underlying protocol starts or
// stops. It implements component.Attachable: Attach/Detach are re-runnable side
// effects of the owner's lifecycle, the §11d soft-binding contract.
type binding struct {
	name string
	t    Transport

	mu       sync.Mutex
	attached bool
	names    []protocol.Name // names to (re-)announce on attach
}

// Attach opens the transport and announces the current name set. Idempotent: a
// second Attach on an already-attached binding is a no-op (§3).
func (b *binding) Attach(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.attached {
		return nil
	}
	if err := b.t.Open(ctx); err != nil {
		return err
	}
	for _, n := range b.names {
		if err := b.t.Announce(n); err != nil {
			_ = b.t.Close()
			return err
		}
	}
	b.attached = true
	return nil
}

// Detach closes the transport. Safe to call when not attached (§3).
func (b *binding) Detach(ctx context.Context) error {
	_ = ctx
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.attached {
		return nil
	}
	b.attached = false
	return b.t.Close()
}

// setNames records the names a binding should announce, announcing any new ones
// immediately if the binding is already attached.
func (b *binding) setNames(names []protocol.Name) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.names = append(b.names[:0], names...)
	if !b.attached {
		return nil
	}
	for _, n := range names {
		if err := b.t.Announce(n); err != nil {
			return err
		}
	}
	return nil
}

var _ component.Attachable = (*binding)(nil)

// Service is the NetBIOS name/session layer. It owns a server name and a set of
// soft transport bindings; SMB plugs into the name layer to claim its
// file-server name. The session/datagram command dispatch is a thin stub at this
// milestone — what lands here is the §11d binding shape that lets transports
// attach and detach independently of the service lifecycle.
type Service struct {
	logger     log.Logger
	serverName string

	mu       sync.Mutex
	running  bool
	ctx      context.Context // captured in Start, for late AddTransport
	names    []protocol.Name
	bindings []*binding
}

// New builds a NetBIOS service with no transports and no server name (the
// registry default). Transports attach later via AddTransport.
func New(logger log.Logger) *Service {
	return &Service{logger: logger}
}

// NewService builds a NetBIOS service that claims serverName (as both a
// workstation and a file-server name) over whatever transports later attach.
func NewService(logger log.Logger, serverName string) *Service {
	s := &Service{logger: logger, serverName: serverName}
	if serverName != "" {
		s.names = []protocol.Name{
			protocol.NewName(serverName, protocol.NameTypeFileServer),
			protocol.NewName(serverName, protocol.NameTypeWorkstation),
		}
	}
	return s
}

// Name returns the component name.
func (s *Service) Name() string { return Name }

// Start attaches every bound transport. A transport that fails to open is left
// detached and the error returned; already-attached siblings keep running.
// Idempotent (§3).
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = true
	s.ctx = ctx
	bindings := append([]*binding(nil), s.bindings...)
	s.mu.Unlock()

	for _, b := range bindings {
		if err := b.Attach(ctx); err != nil {
			s.logf("transport attach failed")
			return err
		}
	}
	s.logf("NetBIOS service started (transports attached; command dispatch stub)")
	return nil
}

// Stop detaches every bound transport. Detach errors are swallowed so one
// failing transport does not block teardown of its siblings. Safe after a
// partial Start (§3).
func (s *Service) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	s.ctx = nil
	bindings := append([]*binding(nil), s.bindings...)
	s.mu.Unlock()

	for _, b := range bindings {
		_ = b.Detach(ctx)
	}
	s.logf("NetBIOS service stopped")
	return nil
}

// AddTransport binds t under name as a soft binding. If the service is already
// running the binding attaches immediately (and announces the current names), so
// a transport whose underlying protocol comes up after NetBIOS joins the live
// service. Re-adding an existing name detaches and replaces the prior binding.
func (s *Service) AddTransport(name string, t Transport) error {
	if t == nil {
		return nil
	}
	b := &binding{name: name, t: t}

	s.mu.Lock()
	var replaced *binding
	for i, existing := range s.bindings {
		if existing.name == name {
			replaced = existing
			s.bindings[i] = b
			goto bound
		}
	}
	s.bindings = append(s.bindings, b)
bound:
	_ = b.setNames(s.names)
	running := s.running
	ctx := s.ctx
	s.mu.Unlock()

	if replaced != nil {
		_ = replaced.Detach(context.Background())
	}
	if running {
		return b.Attach(ctx)
	}
	return nil
}

// RemoveTransport detaches and unbinds the transport bound under name. Idempotent:
// removing an unknown name is a no-op. The rest of the service keeps running, so
// stopping one underlying protocol detaches only its binding (§11d).
func (s *Service) RemoveTransport(name string) error {
	s.mu.Lock()
	var found *binding
	kept := s.bindings[:0]
	for _, b := range s.bindings {
		if b.name == name && found == nil {
			found = b
			continue
		}
		kept = append(kept, b)
	}
	s.bindings = kept
	s.mu.Unlock()

	if found == nil {
		return nil
	}
	return found.Detach(context.Background())
}

// Transports returns the names of the currently bound transports, in bind order.
func (s *Service) Transports() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.bindings))
	for _, b := range s.bindings {
		out = append(out, b.name)
	}
	return out
}

// RegisterName claims an additional NetBIOS file-server name, announcing it on
// every attached transport. SMB calls this to register its server name.
func (s *Service) RegisterName(name string) error {
	n := protocol.NewName(name, protocol.NameTypeFileServer)

	s.mu.Lock()
	s.names = append(s.names, n)
	bindings := append([]*binding(nil), s.bindings...)
	names := append([]protocol.Name(nil), s.names...)
	s.mu.Unlock()

	for _, b := range bindings {
		if err := b.setNames(names); err != nil {
			return err
		}
	}
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
)
