// Package netbios is the NetBIOS name/session layer that SMB rides. It is
// transport-pluggable: NetBEUI, IPX, and NBT transports attach as SOFT bindings
// (component.Attachable, §11d) rather than hard dependencies, so a transport
// whose underlying protocol starts after NetBIOS joins the live service and
// stopping that protocol detaches only its binding.
//
// As of M7 the session-data path is wired over BOTH session transports through one
// upper-layer seam (session.go: SessionConsumer/SessionCircuit, the §3-bis
// command-core / session-transport split):
//
//   - NewNBFEngine builds the NBF (NetBEUI) virtual-circuit state machine (nbf.go)
//     that compose registers on the core/router/netbeui mini-router as both its
//     NameHandler (session-establishment NAME_QUERY) and SessionHandler
//     (SESSION_*/DATA_* frames). It answers a CALL, brings the circuit up,
//     reassembles each SMB message, and routes it to the installed consumer.
//   - NewIPXEngine builds the NBIPX (NetBIOS-over-IPX / NWLink) state machine
//     (nbipx.go) that compose registers on the core/router/ipx mini-router as the
//     SocketHandler for the NB-IPX session socket (0x0455). It accepts SESSION_INIT,
//     reassembles each SMB message off the NB-IPX session header, and routes it to
//     the same consumer.
//
// Both engines route to the installed SessionConsumer (the SMB command engine, via
// SetSessionConsumer), sending the response back over the circuit. Neither holds
// link-layer or SMB knowledge — each reaches the wire through its own egress seam
// (FrameSender / DatagramSender) and the upper layer through the SessionConsumer
// seam.
//
// Alongside the session path the NBF engine answers the two connectionless
// responder paths: the node-status query (STATUS_QUERY → STATUS_RESPONSE, built
// from the local name set, how nbtstat / browser elections probe a node) and the
// connectionless datagram (mailslot / browser traffic), routed to the optional
// DatagramConsumer (SetDatagramConsumer) — a browser/mailslot service plugs in
// there without touching the transport; until one does, datagrams drop after
// decode. The OUTBOUND mirror is SendDatagram: the service fans a connectionless
// NetBIOS datagram (names + payload) to every attached transport's datagramEgress
// (the NBF engine emits a CmdDatagram[Broadcast] UI frame), so the browser sends
// its HostAnnounce / election / backup-list traffic over one seam, transport-blind.
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

// Service is the NetBIOS name/session layer. It owns a server name, a set of
// soft transport bindings, an upper-layer SessionConsumer (SMB), and the NBF
// session engines built per transport. SMB plugs into the name layer to claim its
// file-server name and into the session layer (via SetSessionConsumer) to receive
// the SMB messages the NBF engine reassembles off the circuits.
type Service struct {
	logger     log.Logger
	serverName string

	mu            sync.Mutex
	running       bool
	ctx           context.Context // captured in Start, for late AddTransport
	names         []protocol.Name
	bindings      []*binding
	consumer      SessionConsumer  // upper-layer session sink (SMB); set by compose
	dgramConsumer DatagramConsumer // connectionless datagram sink (browser/mailslot); set by compose
	closers       []circuitCloser  // NBF/NBIPX session engines, one per transport; torn down on Stop
	egresses      []datagramEgress // per-transport outbound-datagram emitters (browser sends fan to these)
}

// circuitCloser is the per-transport session engine surface the service holds for
// teardown: every NBF/NBIPX engine closes its open circuits on Stop so no
// upper-layer (SMB) handles leak. Both *Engine and *IPXEngine satisfy it.
type circuitCloser interface{ closeCircuits() }

// datagramEgress is the per-transport outbound-datagram emitter: send one decoded
// NetBIOS datagram (names + payload) on this transport's wire. The browser's
// SendDatagram fans to every registered egress so one browser serves NetBEUI AND
// IPX at once. The NBF engine satisfies it (a CmdDatagram[Broadcast] UI frame) and
// the NBIPX engine does too (an NMPI MailslotSend IPX type-20 broadcast). It is the
// outbound mirror of DatagramConsumer (the inbound seam).
type datagramEgress interface {
	emitDatagram(d Datagram) error
}

// SendDatagram emits a connectionless NetBIOS datagram on every attached transport
// that can carry one (the browser uses this for HostAnnounce / election / backup-
// list traffic). A directed reply (Datagram.Broadcast false) and a group broadcast
// (true) are distinguished by the transports. With no egress attached the datagram
// is dropped. Errors from individual transports are collected but do not stop the
// fan-out — a failing transport must not silence the others.
func (s *Service) SendDatagram(d Datagram) error {
	s.mu.Lock()
	egresses := append([]datagramEgress(nil), s.egresses...)
	s.mu.Unlock()
	var firstErr error
	for _, e := range egresses {
		if err := e.emitDatagram(d); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
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
	s.logf("NetBIOS service started (transports attached; NBF session engine carries SMB)")
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
	closers := append([]circuitCloser(nil), s.closers...)
	s.mu.Unlock()

	for _, b := range bindings {
		_ = b.Detach(ctx)
	}
	for _, eng := range closers {
		eng.closeCircuits()
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
