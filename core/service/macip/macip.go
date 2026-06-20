// Package macip implements the AppleTalk-facing half of a MacIP gateway as a
// core router service: the IP-over-AppleTalk transport macipgw provides.
//
//   - ATP (DDP type 3) on socket 72 for IP address assignment (TReq → TResp)
//   - DDP type 22 on socket 72 for IP-in-DDP data transport
//
// The IP-side network (raw Ethernet, NAT, DHCP relay, proxy ARP) is an adapter
// concern injected through the IPEgress interface — core never opens a socket or
// links libpcap. The service owns the AppleTalk protocol, the lease pool, and the
// stats; the egress moves IP packets to/from the physical network.
//
// Ring: CORE (stdlib only, reflection-free — IPv4 is [4]byte, no net package).
package macip

import (
	"context"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
	"github.com/ObsoleteMadness/ClassicStack/core/service/nbp"
)

const (
	// Name is the component/section key for the MacIP service.
	Name = "MacIP"

	// Socket is the AppleTalk socket used by MacIP (both ATP config and data).
	Socket = 72

	// DDP types used by MacIP.
	ddpTypeATP   = 3
	ddpTypeMacIP = 22

	// MacIP config function codes.
	macIPFuncAssign = 1 // Mac requests an IP address
	macIPFuncServer = 3 // Mac checks the server is still alive

	// macIPVersion is the protocol version sent in TResp (matches macipgw).
	macIPVersion = 1

	// ATP control byte values.
	atpFuncTReq  = 0x40
	atpFuncTResp = 0x80
	atpEOM       = 0x10

	// macIPCtrlLen is the minimum MacIP user-data size: version(2)+pad(2)+function(4).
	macIPCtrlLen = 8
	// configDataLen is the full MacIP config payload size used in responses.
	configDataLen = 28

	// expiryInterval is how often stale static leases are evicted.
	expiryInterval = 30 * time.Second
)

// IPEgress is the IP-side network seam (adapter-provided). The service hands it
// outbound IP packets from Mac clients and receives inbound IP packets destined
// for them. A nil egress runs the service in AppleTalk-only mode (config replies
// still work; data has nowhere to go). It is NOT a Component — its lifecycle is
// owned by the adapter wiring, not the router.
type IPEgress interface {
	// SendIP forwards one IPv4 packet from a Mac client toward the IP network.
	SendIP(packet []byte) error
	// SetInbound installs the callback the egress calls with each inbound IPv4
	// packet captured from the IP network. The service routes it to the owning
	// Mac client. Called once before Start.
	SetInbound(func(packet []byte))
}

// Config carries the gateway's IP-side identity, advertised to MacIP clients.
type Config struct {
	GatewayIP  IPv4 // gateway IP advertised to clients (pool index 0)
	Network    IPv4 // subnet network base
	Nameserver IPv4 // nameserver advertised to clients
	Broadcast  IPv4 // subnet broadcast
	SubnetMask IPv4 // subnet mask
	HostCount  int  // pool host slots (incl. reserved gateway slot)
	Zone       []byte
	NATEnabled bool
}

// Service is the AppleTalk-facing MacIP gateway component.
type Service struct {
	cfg    Config
	rtr    router.ServiceRouter
	nbp    *nbp.Service
	egress IPEgress
	logger log.Logger

	pool *ipPool

	mu      sync.Mutex
	enabled bool // configured-enabled flag (component.Enableable); set by the factory
	running bool
	ch      chan item
	stop    chan struct{}
	wg      sync.WaitGroup

	// counters published as StatSample (§5).
	statMu  sync.Mutex
	assigns uint64
	dataOut uint64 // DDP-22 → IP egress
	dataIn  uint64 // IP egress → AppleTalk
	dropped uint64
}

type item struct {
	d    ddp.Datagram
	from router.RoutedPort
}

// New builds a MacIP service. rtr is the AppleTalk router it replies/routes
// through; names is the router's NBP service (for the IPGATEWAY registration);
// egress is the IP-side seam (may be nil for AppleTalk-only mode).
func New(rtr router.ServiceRouter, names *nbp.Service, egress IPEgress, cfg Config, logger log.Logger) *Service {
	if cfg.HostCount < 1 {
		cfg.HostCount = 254
	}
	return &Service{
		cfg:    cfg,
		rtr:    rtr,
		nbp:    names,
		egress: egress,
		logger: logger,
		pool:   newIPPool(cfg.Network, cfg.HostCount),
	}
}

// Name returns the component name.
func (s *Service) Name() string { return Name }

// SetNBP installs the NBP name-information service used for the IPGATEWAY registration,
// after construction. The compose cross-wire calls it once NBP is resolved (the
// registry builds MacIP before it can reach the NBP component). Must be called before
// Start; a nil service skips the registration. Idempotent.
func (s *Service) SetNBP(names *nbp.Service) {
	s.mu.Lock()
	s.nbp = names
	s.mu.Unlock()
}

// SetEgress installs the IP-side network seam after construction (the adapter that
// moves IP packets to/from the physical network). Must be called before Start; a nil
// egress leaves the service in AppleTalk-only mode (config/assignment work; IP data has
// nowhere to go). Idempotent.
func (s *Service) SetEgress(egress IPEgress) {
	s.mu.Lock()
	s.egress = egress
	s.mu.Unlock()
}

// Socket returns the MacIP socket so the router dispatches MacIP datagrams here.
func (s *Service) Socket() uint8 { return Socket }

// SetEnabled records the configured-enabled flag (component.Enableable). The compose
// factory sets it from the section; the dashboard shows Disabled rather than omitting
// the gateway, and the supervisor can skip starting a disabled unit.
func (s *Service) SetEnabled(enabled bool) {
	s.mu.Lock()
	s.enabled = enabled
	s.mu.Unlock()
}

// Enabled reports the configured-enabled flag (component.Enableable). A service built
// with no section defaults to disabled.
func (s *Service) Enabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enabled
}

// Kind labels MacIP a gateway service for the dashboard (component.Describable).
func (s *Service) Kind() string { return "gateway" }

// Props surfaces the MacIP mode for the dashboard: whether NAT is enabled and whether
// an IP egress is wired (so an operator can see if data transport is live vs
// AppleTalk-only).
func (s *Service) Props() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	mode := "bridge"
	if s.cfg.NATEnabled {
		mode = "nat"
	}
	egress := "none (AppleTalk-only)"
	if s.egress != nil {
		egress = "wired"
	}
	return map[string]string{"mode": mode, "egress": egress}
}

// Start registers the NBP name, wires the egress inbound callback, and launches
// the worker goroutines. Idempotent (§3).
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = true
	s.ch = make(chan item, 256)
	s.stop = make(chan struct{})

	// Resolve zone from the router if unset.
	if len(s.cfg.Zone) == 0 {
		if zones := s.rtr.Zones().Zones(); len(zones) > 0 {
			s.cfg.Zone = append([]byte(nil), zones[0]...)
		}
	}
	s.wg.Add(2)
	stop := s.stop
	s.mu.Unlock()

	if s.egress != nil {
		s.egress.SetInbound(s.onInboundIP)
	}
	if s.nbp != nil {
		s.nbp.RegisterName(ipv4String(s.cfg.GatewayIP), []byte("IPGATEWAY"), s.cfg.Zone, Socket)
	}

	go s.inboundLoop(ctx, stop)
	go s.expiryLoop(stop)
	return nil
}

// Stop unregisters NBP and stops the workers. Safe after a partial Start (§3).
func (s *Service) Stop(ctx context.Context) error {
	_ = ctx
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	close(s.stop)
	zone := s.cfg.Zone
	s.mu.Unlock()

	if s.nbp != nil {
		s.nbp.UnregisterName(ipv4String(s.cfg.GatewayIP), []byte("IPGATEWAY"), zone)
	}
	s.wg.Wait()
	return nil
}

// Inbound queues a DDP datagram addressed to socket 72; a full queue drops.
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
		s.bump(&s.dropped)
	}
}

// Stats publishes assignment/data counters and the active lease gauge (§5).
func (s *Service) Stats() component.Stats {
	s.statMu.Lock()
	defer s.statMu.Unlock()
	ps := s.pool.stats()
	return component.Stats{
		Counters: map[string]uint64{
			"assigns":  s.assigns,
			"data_out": s.dataOut,
			"data_in":  s.dataIn,
			"dropped":  s.dropped,
		},
		Gauges: map[string]float64{
			"active_leases": float64(ps.activeLeases),
		},
	}
}

// Leases returns a point-in-time copy of all current leases (diagnostics).
func (s *Service) Leases() []LeaseInfo { return s.pool.leases() }

// RegisterExternalLease records an adapter-assigned (e.g. DHCP-relayed) lease so
// inbound IP for it routes to the right Mac client. The IP-side egress calls
// this when it obtains an address outside the static pool.
func (s *Service) RegisterExternalLease(ip IPv4, atNetwork uint16, atNode uint8) {
	s.pool.RegisterExternal(ip, atNetwork, atNode)
}

func (s *Service) inboundLoop(ctx context.Context, stop chan struct{}) {
	defer s.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case it := <-s.ch:
			switch it.d.DDPType {
			case ddpTypeATP:
				s.handleATPConfig(it.d, it.from)
			case ddpTypeMacIP:
				s.handleMacIPData(it.d)
			}
		}
	}
}

func (s *Service) expiryLoop(stop chan struct{}) {
	defer s.wg.Done()
	t := time.NewTicker(expiryInterval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			s.pool.expire()
		}
	}
}

// handleATPConfig processes an ATP TReq on socket 72: an IP address request.
func (s *Service) handleATPConfig(d ddp.Datagram, rx router.RoutedPort) {
	atNet, atNode := normalizeATSource(d, rx)
	if !validATEndpoint(atNet, atNode) {
		return
	}
	// ATP frame: ctrl(1) bitmap(1) tid(2) + MacIP control struct.
	if len(d.Data) < 4+macIPCtrlLen {
		return
	}
	if d.Data[0]&0xC0 != atpFuncTReq {
		return
	}
	tid := uint16(d.Data[2])<<8 | uint16(d.Data[3])
	userData := d.Data[4:]
	// mipr_function is at user_bytes[4:8].
	function := uint32(userData[4])<<24 | uint32(userData[5])<<16 | uint32(userData[6])<<8 | uint32(userData[7])

	var requestedIP IPv4
	if len(userData) >= 12 {
		copy(requestedIP[:], userData[8:12])
	}

	// Server-check (func=3) reuses an existing lease where one exists.
	if function == macIPFuncServer {
		if ip, ok := s.pool.lookupIPByAT(atNet, atNode); ok {
			s.sendATPConfigResp(d, rx, tid, ip)
			return
		}
	}

	assignedIP, ok := s.pool.assign(requestedIP, atNet, atNode)
	if !ok {
		assignedIP = IPv4{}
	} else {
		s.bump(&s.assigns)
	}
	s.sendATPConfigResp(d, rx, tid, assignedIP)
}

// sendATPConfigResp builds and sends an ATP TResp with the IP configuration.
func (s *Service) sendATPConfigResp(d ddp.Datagram, rx router.RoutedPort, tid uint16, assignedIP IPv4) {
	resp := make([]byte, 4+configDataLen)
	resp[0] = atpFuncTResp | atpEOM
	resp[1] = 0 // seq 0
	resp[2] = byte(tid >> 8)
	resp[3] = byte(tid)
	resp[4] = byte(macIPVersion >> 8)
	resp[5] = byte(macIPVersion)
	// resp[6:8] = pad
	resp[8] = byte(macIPFuncAssign >> 24)
	resp[9] = byte(macIPFuncAssign >> 16)
	resp[10] = byte(macIPFuncAssign >> 8)
	resp[11] = byte(macIPFuncAssign)
	copy(resp[12:16], assignedIP[:])
	copy(resp[16:20], s.cfg.Nameserver[:])
	copy(resp[20:24], s.cfg.Broadcast[:])
	// resp[24:28] = pad2
	copy(resp[28:32], s.cfg.SubnetMask[:])
	s.rtr.Reply(d, rx, ddpTypeATP, resp)
}

// handleMacIPData processes a DDP type 22 packet: a raw IP packet from a Mac.
func (s *Service) handleMacIPData(d ddp.Datagram) {
	if len(d.Data) < 20 {
		s.bump(&s.dropped)
		return
	}
	var dstIP IPv4
	copy(dstIP[:], d.Data[16:20])
	s.pool.updateSeen(d.SrcNetwork, d.SrcNode)

	// If the destination is another pool client, deliver directly over AppleTalk.
	if atNet, atNode, ok := s.pool.lookupByIP(dstIP); ok {
		s.routeIPToMac(atNet, atNode, d.Data)
		return
	}
	// Otherwise hand it to the IP egress.
	if s.egress != nil {
		if err := s.egress.SendIP(d.Data); err != nil {
			s.bump(&s.dropped)
		} else {
			s.bump(&s.dataOut)
		}
		return
	}
	s.bump(&s.dropped)
}

// onInboundIP is the egress→service callback: route an inbound IP packet to the
// owning Mac client.
func (s *Service) onInboundIP(packet []byte) {
	if len(packet) < 20 {
		return
	}
	var dstIP IPv4
	copy(dstIP[:], packet[16:20])
	atNet, atNode, ok := s.pool.lookupByIP(dstIP)
	if !ok {
		return
	}
	s.routeIPToMac(atNet, atNode, packet)
}

// routeIPToMac wraps an IP packet in DDP type 22 and routes it to a Mac client.
// Fragmentation (MaxIPPerDDP) is an egress concern when the link MTU demands it;
// the core service forwards the packet as received.
func (s *Service) routeIPToMac(atNet uint16, atNode uint8, pkt []byte) {
	if !validATEndpoint(atNet, atNode) {
		return
	}
	err := s.rtr.Route(ddp.Datagram{
		DestNetwork: atNet,
		DestNode:    atNode,
		DestSocket:  Socket,
		SrcSocket:   Socket,
		DDPType:     ddpTypeMacIP,
		Data:        append([]byte(nil), pkt...),
	}, true)
	if err == nil {
		s.bump(&s.dataIn)
	} else {
		s.bump(&s.dropped)
	}
}

func normalizeATSource(d ddp.Datagram, rx router.RoutedPort) (uint16, uint8) {
	atNet := d.SrcNetwork
	if atNet == 0 && rx != nil && rx.Network() != 0 {
		atNet = rx.Network()
	}
	return atNet, d.SrcNode
}

func (s *Service) bump(c *uint64) {
	s.statMu.Lock()
	*c++
	s.statMu.Unlock()
}

// ipv4String renders an IPv4 as a dotted-decimal byte slice (for NBP object name).
func ipv4String(a IPv4) []byte {
	out := make([]byte, 0, 15)
	for i, oct := range a {
		if i > 0 {
			out = append(out, '.')
		}
		out = appendUint(out, oct)
	}
	return out
}

// appendUint appends the decimal form of a byte (0-255) without fmt.
func appendUint(dst []byte, v byte) []byte {
	if v >= 100 {
		dst = append(dst, '0'+v/100)
		dst = append(dst, '0'+(v/10)%10)
		dst = append(dst, '0'+v%10)
	} else if v >= 10 {
		dst = append(dst, '0'+v/10)
		dst = append(dst, '0'+v%10)
	} else {
		dst = append(dst, '0'+v)
	}
	return dst
}

// compile-time assertions.
var (
	_ router.Service        = (*Service)(nil)
	_ component.Component   = (*Service)(nil)
	_ component.Statful     = (*Service)(nil)
	_ component.Describable = (*Service)(nil)
	_ component.Enableable  = (*Service)(nil)
)
