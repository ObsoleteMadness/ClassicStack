// Package ipxgw implements the AppleTalk-to-IPX gateway service, the
// AppleTalk-side counterpart of Novell's MACIPXGW.NLM that the Classic Mac OS
// MacIPX client connects to.
//
// The wire format (DDP protocol 0x4E carrying a 1-byte opcode followed by either
// an encapsulated IPX datagram or a short control message) is observation-driven;
// see spec/15-macipx-gateway.md for the decoded format.
//
// Ring: CORE (stdlib only, reflection-free). It rides the AppleTalk router as a
// router.Service on the MacIPX socket; when an IPX mini-router (core/router/ipx)
// is attached, encapsulated IPX from MacIPX clients is injected into it, and
// inbound IPX addressed to an assigned MacIPX node is re-encapsulated over DDP.
package ipxgw

import (
	"context"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	protoipx "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/macipx"
	routeripx "github.com/ObsoleteMadness/ClassicStack/core/router/ipx"

	"github.com/ObsoleteMadness/ClassicStack/core/router"
	"github.com/ObsoleteMadness/ClassicStack/core/service/nbp"
)

const (
	// Socket is the AppleTalk DDP socket the gateway listens on. Both sides of
	// every MacIPX exchange use socket 78 — there is no asymmetric pairing.
	Socket = macipx.Socket // 78

	// NBPType is the NBP type Macs use to discover IPX gateways (BrRq with type
	// "IPX Gateway").
	NBPType = macipx.NBPType

	// DefaultIPXNetwork is the IPX network number the gateway announces by
	// default. 0x00000010 matches what NetWare's MACIPXGW.NLM defaults to in the
	// deployments observed during development.
	DefaultIPXNetwork uint32 = 0x00000010
)

// Name is the component/section key for the IPX gateway service.
const Name = "IPXGW"

// ZoneBinding is one NBP registration the gateway publishes: the object name to
// advertise in a specific AppleTalk zone.
type ZoneBinding struct {
	Object []byte
	Zone   []byte
}

// Config tunes gateway behaviour. Zero values are valid; the constructor
// substitutes defaults that match the source captures.
type Config struct {
	// IPXNetwork is the IPX network number the gateway considers itself attached
	// to. 0 means use DefaultIPXNetwork.
	IPXNetwork uint32
}

// clientEntry remembers the IPX node assigned to a MacIPX client plus the DDP
// address it lives at, so IPX replies route back. listenSockets tracks the IPX
// sockets the client asked us to forward broadcast traffic for (opcode 0x10).
type clientEntry struct {
	IPXNode       [6]byte
	DDPNetwork    uint16
	DDPNode       uint8
	DDPSocket     uint8
	listenSockets map[[2]byte]struct{}
}

// Service is the AppleTalk-side surface of the gateway. It plugs into the
// AppleTalk router as a router.Service on Socket. When an IPX router is attached
// (via SetIPXRouter, before Start), encapsulated IPX is decoded and injected, and
// inbound IPX is re-encapsulated and sent back over DDP.
type Service struct {
	nbp      *nbp.Service
	bindings []ZoneBinding
	cfg      Config
	logger   log.Logger

	rtr router.ServiceRouter

	mu        sync.Mutex
	running   bool
	ipxRouter *routeripx.Router
	clients   map[uint32]clientEntry  // keyed by (ddpNet<<8 | ddpNode)
	byIPXNode map[[6]byte]clientEntry // reverse map for inbound IPX → DDP

	ch   chan item
	stop chan struct{}
	wg   sync.WaitGroup

	// counters published as StatSample (§5).
	statMu     sync.Mutex
	registers  uint64
	dataFrames uint64
	listens    uint64
	tunneledIn uint64 // IPX → DDP (inbound to a Mac client)
}

type item struct {
	d    ddp.Datagram
	from router.RoutedPort
}

// New constructs a gateway service. names is the router's NBP service (used for
// registration); bindings declares one NBP name per zone the gateway appears in.
func New(rtr router.ServiceRouter, names *nbp.Service, bindings []ZoneBinding, logger log.Logger) *Service {
	return NewWithConfig(rtr, names, bindings, Config{}, logger)
}

// NewWithConfig is New plus explicit tuning. Pass Config{} for defaults.
func NewWithConfig(rtr router.ServiceRouter, names *nbp.Service, bindings []ZoneBinding, cfg Config, logger log.Logger) *Service {
	if cfg.IPXNetwork == 0 {
		cfg.IPXNetwork = DefaultIPXNetwork
	}
	copied := make([]ZoneBinding, len(bindings))
	for i, b := range bindings {
		copied[i] = ZoneBinding{
			Object: append([]byte(nil), b.Object...),
			Zone:   append([]byte(nil), b.Zone...),
		}
	}
	return &Service{
		nbp:       names,
		bindings:  copied,
		cfg:       cfg,
		logger:    logger,
		rtr:       rtr,
		clients:   make(map[uint32]clientEntry),
		byIPXNode: make(map[[6]byte]clientEntry),
	}
}

// SetIPXRouter wires the gateway to a native IPX router so encapsulated IPX from
// MacIPX clients is forwarded to native IPX peers (and replies flow back via
// RegisterNode). Must be called before Start. Passing nil keeps the gateway in
// log-only mode for IPX traffic.
func (s *Service) SetIPXRouter(r *routeripx.Router) {
	s.mu.Lock()
	s.ipxRouter = r
	s.mu.Unlock()
	// Register as the broadcast handler so inbound IPX broadcasts fan out to
	// MacIPX clients that listened for them. Ignore the error: it just means
	// somebody else already claimed broadcast on this router.
	if r != nil {
		if err := r.RegisterBroadcast(s); err != nil {
			s.warn("RegisterBroadcast failed", log.Str("err", err.Error()))
		}
	}
}

// Name returns the component name.
func (s *Service) Name() string { return Name }

// Socket reports the DDP socket the router dispatches to this service.
func (s *Service) Socket() uint8 { return Socket }

// IPXNetwork reports the network number this gateway announces.
func (s *Service) IPXNetwork() uint32 { return s.cfg.IPXNetwork }

// Start registers the NBP names and launches the worker goroutine. Idempotent (§3).
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = true
	s.ch = make(chan item, 256)
	s.stop = make(chan struct{})
	s.wg.Add(1)
	bindings := s.resolveBindings()
	s.bindings = bindings
	s.mu.Unlock()

	if s.nbp != nil {
		for _, b := range bindings {
			s.nbp.RegisterName(b.Object, []byte(NBPType), b.Zone, Socket)
		}
	}

	go s.run(ctx, s.ch, s.stop)
	return nil
}

// resolveBindings returns the configured bindings, falling back to one name per
// zone the router currently knows. Caller holds s.mu.
func (s *Service) resolveBindings() []ZoneBinding {
	if len(s.bindings) > 0 {
		return s.bindings
	}
	var out []ZoneBinding
	for _, z := range s.rtr.Zones().Zones() {
		out = append(out, ZoneBinding{
			Object: append([]byte(nil), z...),
			Zone:   append([]byte(nil), z...),
		})
	}
	return out
}

// Stop unregisters NBP names, releases IPX nodes, and stops the worker. Safe
// after a partial Start (§3) and idempotent.
func (s *Service) Stop(ctx context.Context) error {
	_ = ctx
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	close(s.stop)
	bindings := s.bindings
	ipxRouter := s.ipxRouter
	claimed := make([][6]byte, 0, len(s.byIPXNode))
	for node := range s.byIPXNode {
		claimed = append(claimed, node)
	}
	s.mu.Unlock()

	if s.nbp != nil {
		for _, b := range bindings {
			s.nbp.UnregisterName(b.Object, []byte(NBPType), b.Zone)
		}
	}
	if ipxRouter != nil {
		for _, n := range claimed {
			ipxRouter.UnregisterNode(n)
		}
		ipxRouter.UnregisterBroadcast()
	}
	s.wg.Wait()
	return nil
}

// Inbound queues a DDP datagram addressed to Socket; a full queue drops.
func (s *Service) Inbound(d ddp.Datagram, from router.RoutedPort) {
	s.mu.Lock()
	ch := s.ch
	running := s.running
	s.mu.Unlock()
	if !running {
		return
	}
	select {
	case ch <- item{d: d, from: from}:
	default:
	}
}

// Stats publishes gateway counters and live client count (§5).
func (s *Service) Stats() component.Stats {
	s.statMu.Lock()
	defer s.statMu.Unlock()
	s.mu.Lock()
	clients := uint64(len(s.clients))
	s.mu.Unlock()
	return component.Stats{
		Counters: map[string]uint64{
			"registers":   s.registers,
			"data_frames": s.dataFrames,
			"listens":     s.listens,
			"tunneled_in": s.tunneledIn,
		},
		Gauges: map[string]float64{
			"clients": float64(clients),
		},
	}
}

func (s *Service) run(ctx context.Context, ch chan item, stop chan struct{}) {
	defer s.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case it := <-ch:
			s.dispatch(it.d, it.from)
		}
	}
}

func (s *Service) dispatch(d ddp.Datagram, from router.RoutedPort) {
	if d.DDPType != macipx.DDPProtocol {
		return
	}
	op, rest, err := macipx.DecodeFrame(d.Data)
	if err != nil {
		s.warn("decode frame failed", log.Str("err", err.Error()))
		return
	}
	switch op {
	case macipx.OpcodeRegisterReq:
		s.bump(&s.registers)
		s.handleRegisterReq(d, from, rest)
	case macipx.OpcodeData:
		s.bump(&s.dataFrames)
		s.handleEncapsulatedIPX(d, rest)
	case macipx.OpcodeListen:
		s.bump(&s.listens)
		s.handleListen(d, rest)
	default:
		// Unknown opcode — ignore (logged at debug only).
	}
}

// handleRegisterReq answers a NetWare-3.x style opcode-0x20 probe with an
// opcode-0x23 reply. The assigned IPX node is derived from the client's DDP
// address; the reply echoes the 6-byte request blob the client sent.
func (s *Service) handleRegisterReq(d ddp.Datagram, from router.RoutedPort, rest []byte) {
	req, err := macipx.DecodeRegisterRequest(rest)
	if err != nil {
		s.warn("bad register request", log.Str("err", err.Error()))
		return
	}
	entry := s.learnClient(d)
	reply := macipx.EncodeRegisterReply(req, entry.IPXNode)
	s.rtr.Reply(d, from, macipx.DDPProtocol, reply)
}

func (s *Service) handleEncapsulatedIPX(d ddp.Datagram, rest []byte) {
	dg, err := protoipx.Decode(rest)
	if err != nil {
		s.warn("encapsulated IPX decode failed", log.Str("err", err.Error()))
		return
	}
	// Learn the client lazily: normally the 0x20/0x23 handshake comes first, but
	// a data frame is a safe alternate trigger if the handshake was missed.
	s.learnClientFromDatagram(d, dg.SrcNode)

	s.mu.Lock()
	ipxRouter := s.ipxRouter
	s.mu.Unlock()
	if ipxRouter == nil {
		return // log-only mode (no IPX router wired)
	}
	// Do NOT stamp SrcNet — the client knows its own IPX network and the router
	// leaves a non-zero SrcNet alone.
	if err := ipxRouter.Send(dg); err != nil {
		s.warn("forward to IPX router failed", log.Str("err", err.Error()))
	}
}

// learnClient records the DDP→IPX mapping using the canonical assigned node.
func (s *Service) learnClient(d ddp.Datagram) clientEntry {
	return s.recordClient(d, macipx.AssignedNodeForDDP(d.SrcNetwork, d.SrcNode))
}

// learnClientFromDatagram trusts the IPX source node the client picked.
func (s *Service) learnClientFromDatagram(d ddp.Datagram, ipxNode [6]byte) clientEntry {
	return s.recordClient(d, ipxNode)
}

func (s *Service) recordClient(d ddp.Datagram, ipxNode [6]byte) clientEntry {
	s.mu.Lock()
	key := clientKey(d.SrcNetwork, d.SrcNode)
	entry, known := s.clients[key]
	if !known || entry.IPXNode != ipxNode {
		listens := entry.listenSockets
		entry = clientEntry{
			IPXNode:       ipxNode,
			DDPNetwork:    d.SrcNetwork,
			DDPNode:       d.SrcNode,
			DDPSocket:     d.SrcSocket,
			listenSockets: listens,
		}
		s.clients[key] = entry
		s.byIPXNode[ipxNode] = entry
	}
	ipxRouter := s.ipxRouter
	s.mu.Unlock()

	// Claim the IPX node so inbound replies for it land in HandleNodeDatagram.
	if !known && ipxRouter != nil {
		_ = ipxRouter.RegisterNode(ipxNode, s) // duplicate claims are a no-op
	}
	return entry
}

// handleListen records the IPX sockets a MacIPX client wants broadcast IPX
// delivered for. Wire format is a sequence of 8-byte (node, socket) pairs.
func (s *Service) handleListen(d ddp.Datagram, rest []byte) {
	entries, err := macipx.DecodeListen(rest)
	if err != nil {
		s.warn("bad listen", log.Str("err", err.Error()))
		return
	}
	s.learnClient(d)
	s.mu.Lock()
	key := clientKey(d.SrcNetwork, d.SrcNode)
	entry := s.clients[key]
	if entry.listenSockets == nil {
		entry.listenSockets = make(map[[2]byte]struct{})
	}
	for _, e := range entries {
		entry.listenSockets[e.Socket] = struct{}{}
	}
	s.clients[key] = entry
	s.byIPXNode[entry.IPXNode] = entry
	s.mu.Unlock()
}

// HandleNodeDatagram implements routeripx.NodeHandler. The IPX router delivers
// unicast IPX addressed to a MacIPX-assigned node (tunnel to that client) and
// broadcast IPX when this service is the registered broadcast handler (fan out
// to clients whose listen set includes the dst socket).
func (s *Service) HandleNodeDatagram(dg *protoipx.Datagram) {
	if dg.DstNode == routeripx.BroadcastNode {
		s.fanoutBroadcast(dg)
		return
	}
	s.mu.Lock()
	entry, ok := s.byIPXNode[dg.DstNode]
	s.mu.Unlock()
	if !ok {
		return // inbound IPX for unknown node — drop
	}
	s.deliverToClient(entry, dg)
}

// fanoutBroadcast delivers an inbound broadcast IPX datagram to every MacIPX
// client that registered a listen for dg.DstSock. The originating client (if it
// is one of ours) is skipped so we do not echo a client's own broadcast back.
func (s *Service) fanoutBroadcast(dg *protoipx.Datagram) {
	s.mu.Lock()
	originator, originatorIsOurs := s.byIPXNode[dg.SrcNode]
	targets := make([]clientEntry, 0)
	for _, c := range s.clients {
		if _, listening := c.listenSockets[dg.DstSock]; !listening {
			continue
		}
		if originatorIsOurs && c.IPXNode == originator.IPXNode {
			continue // do not reflect to sender
		}
		targets = append(targets, c)
	}
	s.mu.Unlock()
	for _, t := range targets {
		s.deliverToClient(t, dg)
	}
}

func (s *Service) deliverToClient(entry clientEntry, dg *protoipx.Datagram) {
	ipxBytes, err := dg.Encode(nil)
	if err != nil {
		s.warn("encode IPX for client failed", log.Str("err", err.Error()))
		return
	}
	frame := macipx.EncodeData(ipxBytes)
	s.bump(&s.tunneledIn)
	_ = s.rtr.Route(ddp.Datagram{
		DestNetwork: entry.DDPNetwork,
		DestNode:    entry.DDPNode,
		DestSocket:  entry.DDPSocket,
		SrcSocket:   Socket,
		DDPType:     macipx.DDPProtocol,
		Data:        frame,
	}, true)
}

func clientKey(net uint16, node uint8) uint32 {
	return uint32(net)<<8 | uint32(node)
}

func (s *Service) bump(c *uint64) {
	s.statMu.Lock()
	*c++
	s.statMu.Unlock()
}

func (s *Service) warn(msg string, f log.Field) {
	if s.logger == nil || !s.logger.Enabled(log.Warn) {
		return
	}
	s.logger.Log2(log.Warn, msg, log.Str("scope", Name), f)
}

// compile-time assertions.
var (
	_ router.Service        = (*Service)(nil)
	_ component.Component   = (*Service)(nil)
	_ component.Statful     = (*Service)(nil)
	_ routeripx.NodeHandler = (*Service)(nil)
)
