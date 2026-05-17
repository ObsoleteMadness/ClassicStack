//go:build ipxgw || all

// Package ipxgw implements an AppleTalk-to-IPX gateway service, the
// AppleTalk-side counterpart of Novell's MACIPXGW.NLM that the Classic
// Mac OS MacIPX client connects to.
//
// The wire format (DDP protocol 0x4E carrying a 1-byte opcode followed
// by either an encapsulated IPX datagram or a short control message) is
// observation-driven; see spec/15-macipx-gateway.md for the decoded
// format.
package ipxgw

import (
	"context"
	"fmt"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/port"
	"github.com/ObsoleteMadness/ClassicStack/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/protocol/ipx"
	"github.com/ObsoleteMadness/ClassicStack/protocol/macipx"
	routeripx "github.com/ObsoleteMadness/ClassicStack/router/ipx"
	"github.com/ObsoleteMadness/ClassicStack/service"
	"github.com/ObsoleteMadness/ClassicStack/service/zip"
)

const (
	// Socket is the AppleTalk DDP socket the gateway listens on. Both
	// sides of every MacIPX exchange use socket 78 — there is no
	// asymmetric pairing.
	Socket = macipx.Socket // 78

	// NBPType is the NBP type Macs use to discover IPX gateways
	// (BrRq with type "IPX Gateway").
	NBPType = macipx.NBPType

	// DefaultIPXNetwork is the IPX network number the gateway
	// announces by default when the operator has not configured one.
	// `0x00000010` matches what NetWare's MACIPXGW.NLM defaults to in
	// the deployments observed during development.
	DefaultIPXNetwork uint32 = 0x00000010
)

// ZoneBinding is one NBP registration this gateway will publish: the object
// name to advertise in a specific AppleTalk zone. A typical deployment binds
// one name per AppleTalk-facing network (e.g. object="EtherTalk Network" in
// the EtherTalk zone, object="LToUDP Network" in the LToUDP zone).
type ZoneBinding struct {
	Object []byte
	Zone   []byte
}

// Config tunes gateway behaviour. Zero values are valid; the constructor
// substitutes defaults that match the source captures.
type Config struct {
	// IPXNetwork is the IPX network number the gateway considers
	// itself attached to. Used today only for logging and as the
	// implicit source network when the operator's IPX deployment
	// has not assigned one. 0 means use DefaultIPXNetwork.
	IPXNetwork uint32
}

// clientEntry remembers the IPX node we assigned to a MacIPX client plus
// the DDP address it lives at, so we can route IPX replies back later.
// listenSockets tracks the IPX sockets the client asked us to forward
// broadcast traffic for (opcode 0x10 registrations).
type clientEntry struct {
	IPXNode       [6]byte
	DDPNetwork    uint16
	DDPNode       uint8
	DDPSocket     uint8
	listenSockets map[[2]byte]struct{}
}

// Service is the AppleTalk-side surface of the gateway. It plugs into the
// AppleTalk router as a normal service.Service on Socket. When an IPX
// router is attached (via SetIPXRouter, before Start), encapsulated IPX
// from MacIPX clients is decoded and injected into the IPX router, and
// inbound IPX addressed to an assigned MacIPX node is re-encapsulated
// and sent back over DDP.
type Service struct {
	nbp      *zip.NameInformationService
	bindings []ZoneBinding
	cfg      Config

	mu        sync.Mutex
	router    service.DatagramRouter
	ipxRouter routeripx.Router
	clients   map[uint32]clientEntry  // keyed by (ddpNet<<8 | ddpNode)
	byIPXNode map[[6]byte]clientEntry // reverse map for inbound IPX → DDP
}

// New constructs a gateway service. bindings declares one NBP name per zone
// the gateway should appear in. nbp is the router's NameInformationService;
// the gateway uses it for both registration and (later) for ARP-style lookups
// of MacIPX clients.
func New(nbp *zip.NameInformationService, bindings []ZoneBinding) *Service {
	return NewWithConfig(nbp, bindings, Config{})
}

// NewWithConfig is New plus explicit tuning. Pass Config{} for defaults.
func NewWithConfig(nbp *zip.NameInformationService, bindings []ZoneBinding, cfg Config) *Service {
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
		nbp:       nbp,
		bindings:  copied,
		cfg:       cfg,
		clients:   make(map[uint32]clientEntry),
		byIPXNode: make(map[[6]byte]clientEntry),
	}
}

// SetIPXRouter wires the gateway to a native IPX router so encapsulated
// IPX from MacIPX clients is forwarded to native IPX peers (and replies
// flow back via RegisterNode). Must be called before Start. Passing nil
// (the default) keeps the gateway in log-only mode for IPX traffic.
func (s *Service) SetIPXRouter(r routeripx.Router) {
	s.mu.Lock()
	s.ipxRouter = r
	s.mu.Unlock()
	// Register as the broadcast handler so we can fan inbound IPX
	// broadcasts out to MacIPX clients that listened for them (e.g.
	// Duke3D's 0xDEAD socket). Ignore the error: it just means
	// somebody else already claimed broadcast on this router.
	if r != nil {
		if err := r.RegisterBroadcast(s); err != nil {
			netlog.Warn("ipxgw: RegisterBroadcast: %v", err)
		}
	}
}

// Socket reports the DDP socket the router should dispatch to this service.
func (s *Service) Socket() uint8 { return Socket }

// Start registers the NBP names. The gateway has no goroutines of its own —
// Inbound() is called synchronously from the router's dispatch path.
func (s *Service) Start(ctx context.Context, r service.Router) error {
	s.mu.Lock()
	s.router = r
	s.mu.Unlock()

	// If no explicit bindings were provided, fall back to registering one name
	// per zone the router currently knows about. The object name we publish in
	// that case matches what real MACIPXGW deployments use: the zone name
	// itself, treated as a human-readable network label.
	bindings := s.bindings
	if len(bindings) == 0 {
		for _, z := range r.Zones() {
			zoneCopy := append([]byte(nil), z...)
			bindings = append(bindings, ZoneBinding{
				Object: append([]byte(nil), z...),
				Zone:   zoneCopy,
			})
		}
	}

	for _, b := range bindings {
		s.nbp.RegisterName(b.Object, []byte(NBPType), b.Zone, Socket)
		netlog.Info("ipxgw: NBP registered %q:%s@%q on socket %d",
			b.Object, NBPType, b.Zone, Socket)
	}

	// Remember the resolved bindings so Stop() can unregister exactly what
	// Start() registered, even when we filled them in from r.Zones().
	s.mu.Lock()
	s.bindings = bindings
	s.mu.Unlock()

	netlog.Info("ipxgw: gateway started (ipx-net=0x%08x)", s.cfg.IPXNetwork)
	return nil
}

// Stop unregisters NBP names and releases any IPX nodes claimed for
// MacIPX clients.
func (s *Service) Stop() error {
	s.mu.Lock()
	bindings := s.bindings
	ipxRouter := s.ipxRouter
	claimed := make([][6]byte, 0, len(s.byIPXNode))
	for node := range s.byIPXNode {
		claimed = append(claimed, node)
	}
	s.mu.Unlock()
	for _, b := range bindings {
		s.nbp.UnregisterName(b.Object, []byte(NBPType), b.Zone)
	}
	if ipxRouter != nil {
		for _, n := range claimed {
			ipxRouter.UnregisterNode(n)
		}
		ipxRouter.UnregisterBroadcast()
	}
	return nil
}

// Inbound is invoked by the router for every DDP datagram addressed to
// Socket.
func (s *Service) Inbound(d ddp.Datagram, rxPort port.Port) {
	if d.DDPType != macipx.DDPProtocol {
		netlog.Debug("ipxgw: dropping non-MacIPX DDP type %d on socket %d", d.DDPType, Socket)
		return
	}
	op, rest, err := macipx.DecodeFrame(d.Data)
	if err != nil {
		netlog.Warn("ipxgw: decode frame from %d.%d: %v", d.SourceNetwork, d.SourceNode, err)
		return
	}
	switch op {
	case macipx.OpcodeRegisterReq:
		s.handleRegisterReq(d, rxPort, rest)
	case macipx.OpcodeData:
		s.handleEncapsulatedIPX(d, rest)
	case macipx.OpcodeListen:
		s.handleListen(d, rest)
	default:
		netlog.Info("ipxgw: unknown opcode 0x%02x from %d.%d payload=%x",
			byte(op), d.SourceNetwork, d.SourceNode, rest)
	}
}

// handleRegisterReq answers a NetWare-3.x style opcode-0x20 probe with
// an opcode-0x23 reply. The assigned IPX node is derived from the
// client's DDP address (MacIPX clients on later gateways skip this
// handshake and synthesize the same node themselves — see
// macipx.AssignedNodeForDDP). The reply echoes the 6-byte request blob
// the client sent.
func (s *Service) handleRegisterReq(d ddp.Datagram, rxPort port.Port, rest []byte) {
	req, err := macipx.DecodeRegisterRequest(rest)
	if err != nil {
		netlog.Warn("ipxgw: bad register request from %d.%d: %v",
			d.SourceNetwork, d.SourceNode, err)
		return
	}
	entry := s.learnClient(d)
	s.mu.Lock()
	router := s.router
	s.mu.Unlock()
	if router == nil {
		netlog.Warn("ipxgw: no router available to reply to %d.%d",
			d.SourceNetwork, d.SourceNode)
		return
	}
	reply := macipx.EncodeRegisterReply(req, entry.IPXNode)
	router.Reply(d, rxPort, macipx.DDPProtocol, reply)
	netlog.Info("ipxgw: register: DDP %d.%d → IPX %s (req=%x)",
		d.SourceNetwork, d.SourceNode, formatNode(entry.IPXNode), req)
}

func (s *Service) handleEncapsulatedIPX(d ddp.Datagram, rest []byte) {
	dg, err := ipx.Decode(rest)
	if err != nil {
		netlog.Warn("ipxgw: encapsulated IPX from %d.%d failed to decode: %v",
			d.SourceNetwork, d.SourceNode, err)
		return
	}
	// Learn the client lazily. We normally see the 0x20/0x23 handshake
	// first, but if frames are reordered or the handshake was missed
	// (e.g. capture started mid-conversation) a data frame is a safe
	// alternate trigger. The IPX source node the client picked must
	// agree with what AssignedNodeForDDP would synthesize from its DDP
	// address; we trust the client's choice and use it as the routing
	// key.
	s.learnClientFromDatagram(d, dg.SrcNode)

	netlog.Debug("ipxgw: encapsulated IPX from DDP %d.%d: src=%s.%s:%04x dst=%s.%s:%04x type=%d len=%d",
		d.SourceNetwork, d.SourceNode,
		formatNet(dg.SrcNet), formatNode(dg.SrcNode), uint16(dg.SrcSock[0])<<8|uint16(dg.SrcSock[1]),
		formatNet(dg.DstNet), formatNode(dg.DstNode), uint16(dg.DstSock[0])<<8|uint16(dg.DstSock[1]),
		dg.Type, dg.Length)

	s.mu.Lock()
	ipxRouter := s.ipxRouter
	s.mu.Unlock()
	if ipxRouter == nil {
		return // log-only mode (no IPX router wired)
	}

	// Do NOT stamp SrcNet — the client knows its own IPX network
	// (it learns it from the gateway's RIP replies, after which it
	// sets the field explicitly) and overwriting it would break the
	// conversation. The IPX router leaves SrcNet alone when it is
	// already non-zero.
	if err := ipxRouter.Send(dg); err != nil {
		netlog.Warn("ipxgw: forward to IPX router: %v", err)
	}
}

// learnClient records the DDP-to-IPX mapping for a freshly-seen MacIPX
// peer and claims the IPX node on the native IPX router so inbound
// replies are dispatched here. Returns the (possibly already-known)
// entry. Used by the 0x20 register path where we assign the canonical
// IPX node ourselves.
func (s *Service) learnClient(d ddp.Datagram) clientEntry {
	ipxNode := macipx.AssignedNodeForDDP(d.SourceNetwork, d.SourceNode)
	return s.recordClient(d, ipxNode)
}

// learnClientFromDatagram is learnClient's data-frame variant: it
// trusts the IPX source node the client picked rather than synthesizing
// one. In practice the two agree because real MacIPX clients use the
// same AssignedNodeForDDP encoding the gateway hands out, but trusting
// the client keeps us robust against future variations.
func (s *Service) learnClientFromDatagram(d ddp.Datagram, ipxNode [6]byte) clientEntry {
	return s.recordClient(d, ipxNode)
}

func (s *Service) recordClient(d ddp.Datagram, ipxNode [6]byte) clientEntry {
	s.mu.Lock()
	key := clientKey(d.SourceNetwork, d.SourceNode)
	entry, known := s.clients[key]
	if !known || entry.IPXNode != ipxNode {
		// Preserve any listen-socket subscriptions from a prior
		// entry (e.g. a listen frame that arrived before the
		// register frame on a slow link).
		listens := entry.listenSockets
		entry = clientEntry{
			IPXNode:       ipxNode,
			DDPNetwork:    d.SourceNetwork,
			DDPNode:       d.SourceNode,
			DDPSocket:     d.SourceSocket,
			listenSockets: listens,
		}
		s.clients[key] = entry
		s.byIPXNode[ipxNode] = entry
	}
	ipxRouter := s.ipxRouter
	s.mu.Unlock()

	// Claim the IPX node on the IPX router so inbound replies for
	// it land in HandleNodeDatagram. Duplicate claims are a no-op
	// (the router rejects them; we deliberately ignore the error).
	if !known && ipxRouter != nil {
		if err := ipxRouter.RegisterNode(ipxNode, s); err != nil {
			netlog.Debug("ipxgw: RegisterNode %s: %v (already claimed?)",
				formatNode(ipxNode), err)
		} else {
			netlog.Info("ipxgw: learned client DDP %d.%d → IPX %s",
				d.SourceNetwork, d.SourceNode, formatNode(ipxNode))
		}
	}
	return entry
}

// handleListen records the IPX sockets a MacIPX client wants broadcast
// IPX delivered for. The wire format is a sequence of 8-byte
// (node, socket) pairs; the node is always the broadcast address in
// observed captures, so we key off socket only.
func (s *Service) handleListen(d ddp.Datagram, rest []byte) {
	entries, err := macipx.DecodeListen(rest)
	if err != nil {
		netlog.Warn("ipxgw: bad listen from %d.%d: %v (payload=%x)",
			d.SourceNetwork, d.SourceNode, err, rest)
		return
	}
	entry := s.learnClient(d)
	s.mu.Lock()
	c, ok := s.clients[clientKey(d.SourceNetwork, d.SourceNode)]
	if ok {
		entry = c
	}
	if entry.listenSockets == nil {
		entry.listenSockets = make(map[[2]byte]struct{})
	}
	for _, e := range entries {
		entry.listenSockets[e.Socket] = struct{}{}
	}
	s.clients[clientKey(d.SourceNetwork, d.SourceNode)] = entry
	s.byIPXNode[entry.IPXNode] = entry
	s.mu.Unlock()

	socks := make([]string, 0, len(entries))
	for _, e := range entries {
		socks = append(socks, fmt.Sprintf("0x%02x%02x", e.Socket[0], e.Socket[1]))
	}
	netlog.Info("ipxgw: listen: DDP %d.%d → IPX %s adds sockets %v",
		d.SourceNetwork, d.SourceNode, formatNode(entry.IPXNode), socks)
}

// HandleNodeDatagram implements routeripx.NodeHandler. The IPX router
// delivers two kinds of frames here:
//   - Unicast IPX addressed to a MacIPX-assigned node (dispatched by
//     the router's per-node map). We look up the client and tunnel.
//   - Broadcast IPX (dst node = ff:ff:ff:ff:ff:ff) when this service
//     is the registered broadcast handler. We fan out to every client
//     whose opcode-0x10 listen set includes the dst socket.
func (s *Service) HandleNodeDatagram(dg *ipx.Datagram) {
	if dg.DstNode == routeripx.BroadcastNode {
		s.fanoutBroadcast(dg)
		return
	}
	s.mu.Lock()
	entry, ok := s.byIPXNode[dg.DstNode]
	router := s.router
	s.mu.Unlock()
	if !ok {
		netlog.Debug("ipxgw: inbound IPX for unknown node %s — dropping", formatNode(dg.DstNode))
		return
	}
	if router == nil {
		netlog.Warn("ipxgw: no AT router to deliver IPX to %s", formatNode(dg.DstNode))
		return
	}
	s.deliverToClient(entry, dg, router)
}

// fanoutBroadcast delivers an inbound broadcast IPX datagram to every
// MacIPX client that has registered a listen for dg.DstSock. The
// originating client (if it is itself one of ours) is skipped so we
// do not echo a client's own broadcast back to it.
func (s *Service) fanoutBroadcast(dg *ipx.Datagram) {
	s.mu.Lock()
	router := s.router
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
	if router == nil || len(targets) == 0 {
		netlog.Debug("ipxgw: broadcast on sock=%02x%02x dropped (router=%v targets=%d)",
			dg.DstSock[0], dg.DstSock[1], router != nil, len(targets))
		return
	}
	for _, t := range targets {
		s.deliverToClient(t, dg, router)
	}
	netlog.Debug("ipxgw: broadcast on sock=%02x%02x fanned out to %d MacIPX client(s)",
		dg.DstSock[0], dg.DstSock[1], len(targets))
}

func (s *Service) deliverToClient(entry clientEntry, dg *ipx.Datagram, router service.DatagramRouter) {
	ipxBytes, err := dg.Encode()
	if err != nil {
		netlog.Warn("ipxgw: encode IPX for %s: %v", formatNode(entry.IPXNode), err)
		return
	}
	frame := macipx.EncodeData(ipxBytes)
	out := ddp.Datagram{
		DestinationNetwork: entry.DDPNetwork,
		DestinationNode:    entry.DDPNode,
		DestinationSocket:  entry.DDPSocket,
		SourceSocket:       Socket,
		DDPType:            macipx.DDPProtocol,
		Data:               frame,
	}
	if err := router.Route(out, true); err != nil {
		netlog.Warn("ipxgw: route IPX to DDP %d.%d:%d: %v",
			entry.DDPNetwork, entry.DDPNode, entry.DDPSocket, err)
	}
}

// IPXNetwork reports the network number this gateway announces.
func (s *Service) IPXNetwork() uint32 { return s.cfg.IPXNetwork }

func clientKey(net uint16, node uint8) uint32 {
	return uint32(net)<<8 | uint32(node)
}

func formatNode(n [6]byte) string {
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", n[0], n[1], n[2], n[3], n[4], n[5])
}

func formatNet(n [4]byte) string {
	return fmt.Sprintf("%02x%02x%02x%02x", n[0], n[1], n[2], n[3])
}
