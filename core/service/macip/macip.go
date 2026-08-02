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
//
// Attribution: the MacIP wire protocol implemented here — the ATP config exchange
// (struct macip_req layout, MACIP_ASSIGN/SERVER/ERROR functions, the "No Address
// Available."/"Unknown Operation." error strings), the IPADDRESS/IPGATEWAY NBP
// naming, the arp_set() source-IP snooping, and the 586-byte MacIP MTU — follows the
// original C "AppleTalk MacIP Gateway" (macipgw) by Stefan Bethke (© 1997, 2013) and
// Jason King (© 2015), released under the GNU General Public License v2-or-later.
// This is an independent Go reimplementation; macipgw is used as the golden reference
// for wire behaviour. macipgw's GPLv2+ terms are compatible with this project's GPLv3
// licence. See spec/14-macip-gateway.md and the README "Status and attribution".
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

	// MacIP config function codes (macip.c: MACIP_ASSIGN/MACIP_SERVER/MACIP_ERROR).
	macIPFuncAssign = 1  // Mac requests an IP address
	macIPFuncServer = 3  // Mac checks the server is still alive
	macIPFuncError  = -1 // gateway reports failure; error string carried in reply

	// macIPVersion is the protocol version sent in TResp (matches macipgw).
	macIPVersion = 1

	// ATP control byte values.
	atpFuncTReq  = 0x40
	atpFuncTResp = 0x80
	atpEOM       = 0x10

	// macIPCtrlLen is the minimum MacIP user-data size: version(2)+pad(2)+function(4).
	macIPCtrlLen = 8

	// The config-reply user-data mirrors the original macipgw struct macip_req exactly
	// (macip.c) so any MacTCP client that reads its layout by fixed offset is satisfied:
	//
	//   control  version(2) pad(2) function(4)                             = 8 bytes
	//   data     ipaddr(4) nameserver(4) broadcast(4) pad2(4) subnet(4)
	//            pad3(4) pad4(4) pad5(4)                                    = 32 bytes
	//   error    char[22]                                                  = 22 bytes
	//
	// On success macipgw sends sizeof(struct macip_req) - 21 = 62 - 21 = 41 bytes of
	// MacIP user-data: control(8) + the full 32-byte data block + 1 leading NUL of the
	// error field. On failure it appends the NUL-terminated error string. We match those
	// lengths byte-for-byte. (These lengths are the MacIP user-data only; the 4-byte ATP
	// TResp header — ctrl/seq/tid — is prepended separately, so the wire buffer is
	// 4 + configUserLen bytes.)
	configCtrlLen   = 8                                   // version+pad+function
	configFieldsLen = 32                                  // ip/ns/bcast/pad2/subnet/pad3/pad4/pad5
	configUserLen   = configCtrlLen + configFieldsLen + 1 // 41: +1 NUL error byte
	configErrLen    = 22                                  // error[] capacity in struct macip_req_data

	// expiryInterval is how often stale static leases are evicted.
	expiryInterval = 30 * time.Second
)

// MacIP error strings, byte-for-byte from the original macipgw (macip.c error_noip/
// error_noop), sent in the reply's error field when the function is macIPFuncError.
const (
	errNoIP = "No Address Available." // pool exhausted (MACIP_ASSIGN failure)
	errNoOp = "Unknown Operation."    // unrecognised function code
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

// AddressAssigner is an OPTIONAL capability of an IPEgress: an egress that sources
// client addresses from the IP network itself (DHCP relay) implements it so the core
// delegates address assignment to it instead of the static pool. AssignIP may block
// (a DHCP round-trip), so the core calls it from a per-request goroutine. ok=false
// means assignment failed and the core must not reply (the Mac retries). The returned
// AssignedConfig carries the lease plus any DHCP-supplied config; zero-valued fields fall
// back to the service Config. The egress is responsible for any proxy-ARP / gratuitous
// announcement for the assigned IP and for registering inbound routing for it (the core
// records the lease via RegisterExternalLease before replying).
//
// AssignerActive reports whether this egress is CURRENTLY sourcing addresses (i.e. DHCP
// relay is actually enabled). Go interface satisfaction is structural: the NAT/bridge
// egress carries an AssignIP method for all modes but only performs DHCP when relay is
// configured. Without this gate, core would delegate to it in NAT mode too — where
// AssignIP always fails (no DHCP), and the "ok=false ⇒ do not reply" contract would then
// silently swallow EVERY config request, so the Mac never gets an IP and the static pool
// is never consulted. Core only delegates when AssignerActive returns true; otherwise it
// uses the static pool. An egress that implements AddressAssigner MUST implement this.
type AddressAssigner interface {
	AssignIP(atNetwork uint16, atNode uint8, requested IPv4) (AssignedConfig, bool)
	AssignerActive() bool
}

// GatewayReporter is an OPTIONAL capability of an IPEgress: it reports the IP-side
// gateway identity (the real on-subnet upstream/default gateway) the core should
// advertise to MacTCP clients. In bridge mode the client's lease is on the real LAN
// subnet, so its gateway must be a real on-subnet IP rather than the (possibly unset)
// configured GatewayIP. The core adopts this at Start when its own GatewayIP is zero,
// so the IPGATEWAY NBP name and the config reply are never 0.0.0.0 — the "MacTCP shows
// 0.0.0.0 and won't send" failure. A zero return leaves the core's GatewayIP unchanged.
type GatewayReporter interface {
	GatewayIP() IPv4
}

// AssignedConfig is the result of an egress-driven (DHCP) address assignment. Any
// zero-valued IPv4 field is replaced by the service Config default before the TResp.
type AssignedConfig struct {
	IP         IPv4 // the address to hand the client (required; zero ⇒ failure)
	Nameserver IPv4 // DNS server (zero ⇒ Config.Nameserver)
	Broadcast  IPv4 // broadcast address (zero ⇒ Config.Broadcast)
	SubnetMask IPv4 // subnet mask (zero ⇒ Config.SubnetMask)
	// Router is the IP-side default gateway the DHCP server supplied (option 3). In
	// bridge + DHCP-relay mode the client's lease is on the real LAN subnet, so the
	// gateway MacTCP must use is this router (on that same subnet), NOT the gateway's
	// configured GatewayIP — which may be on a different (or unset) subnet and would
	// make MacTCP reject it as off-subnet, breaking all off-net routing. The service
	// adopts it as its advertised IPGATEWAY identity (§NBP). Zero ⇒ keep Config.GatewayIP.
	Router IPv4
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
	enabled bool          // configured-enabled flag (component.Enableable); set by the factory
	egressP *EgressParams // IP-side egress intent from the section; the compose root reads it to build the egress (§B). nil = AppleTalk-only.
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

	// flowMu guards flows: the last receive-window/ACK each Mac TCP flow advertised,
	// learned from Mac→peer segments. Observation only (diagnostics + a record of what
	// the window throttle acts on). Bounded by maxTrackedFlows so a scan/flood cannot
	// grow it without limit.
	flowMu sync.Mutex
	flows  map[flowKey]macFlow
}

// flowKey identifies one Mac TCP flow by the Mac's IP+port and the peer's IP+port,
// in the Mac→peer direction (so the window/ACK we record is always the Mac's).
type flowKey struct {
	macIP    IPv4
	peerIP   IPv4
	macPort  uint16
	peerPort uint16
}

// maxTrackedFlows caps the observed-flow table so a port scan or SYN flood from a Mac
// cannot grow it unbounded. When full, new flows are simply not recorded (observation
// is best-effort; it never affects forwarding).
const maxTrackedFlows = 512

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
	if logger == nil {
		// Keep the logger always-non-nil at the seam (no call-site guards); a sink-less
		// logger discards. Matches the project's logging-injection pattern.
		logger = log.New(Name)
	}
	return &Service{
		cfg:    cfg,
		rtr:    rtr,
		nbp:    names,
		egress: egress,
		logger: logger,
		pool:   newIPPool(cfg.Network, cfg.HostCount),
		flows:  make(map[flowKey]macFlow),
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

// SetEgressParams records the IP-side egress intent from the section, so the service
// DECLARES whether it wants IP egress and with what params — the compose root reads
// this (EgressParams) and builds the pcap/cgo egress adapter, instead of re-reading the
// section itself (§B). A nil params (or an empty Interface) keeps the gateway
// AppleTalk-only. Idempotent, safe before Start.
func (s *Service) SetEgressParams(p *EgressParams) {
	s.mu.Lock()
	s.egressP = p
	s.mu.Unlock()
}

// EgressParams returns the IP-side egress intent the service was configured with, and
// ok=false when it wants no egress (no section, disabled, or no Interface) — the
// compose root then leaves the gateway AppleTalk-only without re-reading the model.
func (s *Service) EgressParams() (EgressParams, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.egressP == nil || s.egressP.Interface == "" {
		return EgressParams{}, false
	}
	return *s.egressP, true
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
		// Before advertising, adopt the egress-reported on-subnet gateway when our own
		// GatewayIP is unset (gateway_ip left blank in bridge mode). Otherwise the NBP
		// IPGATEWAY name and the config reply carry 0.0.0.0 and MacTCP refuses to send.
		if s.cfg.GatewayIP.IsZero() {
			if gr, ok := s.egress.(GatewayReporter); ok {
				if gw := gr.GatewayIP(); !gw.IsZero() {
					s.mu.Lock()
					s.cfg.GatewayIP = gw
					s.mu.Unlock()
				}
			}
		}
	}
	if s.nbp != nil {
		s.nbp.RegisterName(ipv4String(s.cfg.GatewayIP), []byte("IPGATEWAY"), s.cfg.Zone, Socket)
	}

	go s.inboundLoop(ctx, stop)
	go s.expiryLoop(stop)

	s.mu.Lock()
	gw := s.cfg.GatewayIP
	zone := s.cfg.Zone
	network := s.cfg.Network
	hostCount := s.cfg.HostCount
	hasEgress := s.egress != nil
	s.mu.Unlock()
	s.logger.Log(log.Info, "macip: started",
		log.Str("gateway", string(ipv4String(gw))),
		log.Str("network", string(ipv4String(network))),
		log.Int("host_count", int64(hostCount)),
		log.Str("zone", string(zone)),
		log.Bool("egress", hasEgress))
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
	s.logger.Log0(log.Info, "macip: stopped")
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

// Leases returns a point-in-time copy of all current leases (diagnostics). The
// diagnostics adapter (adapter/control/diag) reads this and decodes IP↔AppleTalk for
// display, so the management plane carries no MacIP type.
func (s *Service) Leases() []LeaseInfo { return s.pool.leases() }

// OwnsIP reports whether an IPv4 is currently leased to a MacIP client (static or
// external). The IP-side egress uses it to decide proxy-ARP / inbound filtering
// without owning a copy of the lease table.
func (s *Service) OwnsIP(ip IPv4) bool {
	_, _, ok := s.pool.lookupByIP(ip)
	return ok
}

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

	// Only MACIP_ASSIGN and MACIP_SERVER are defined; anything else gets a MACIP_ERROR
	// reply carrying "Unknown Operation." — matching macipgw's switch default arm.
	if function != macIPFuncAssign && function != macIPFuncServer {
		s.logger.Log1(log.Info, "macip: unknown config function", log.Int("function", int64(function)))
		s.sendATPConfigError(d, rx, tid, errNoOp)
		return
	}

	// Server-check (func=3) reuses an existing lease where one exists.
	if function == macIPFuncServer {
		if ip, ok := s.pool.lookupIPByAT(atNet, atNode); ok {
			s.sendATPConfigResp(d, rx, tid, AssignedConfig{IP: ip})
			return
		}
	}

	// When the egress sources addresses from the IP network (DHCP relay), delegate
	// assignment to it off the inbound loop — a DHCP round-trip can block — and reply
	// once it resolves. Otherwise use the static pool synchronously.
	if as := s.assigner(); as != nil {
		s.mu.Lock()
		if !s.running {
			s.mu.Unlock()
			return
		}
		s.wg.Add(1)
		stop := s.stop
		s.mu.Unlock()
		go s.assignViaEgress(as, d, rx, tid, requestedIP, atNet, atNode, stop)
		return
	}

	assignedIP, fresh, ok := s.pool.assign(requestedIP, atNet, atNode)
	if !ok {
		// Pool exhausted: reply MACIP_ERROR with "No Address Available." rather than a
		// bogus 0.0.0.0 assignment, matching macipgw's lease_ip()-failed path.
		s.logger.Log(log.Warn, "macip: address pool exhausted, no lease available")
		s.sendATPConfigError(d, rx, tid, errNoIP)
		return
	}
	s.bump(&s.assigns)
	if fresh {
		s.logAllocated(assignedIP, atNet, atNode)
	}
	s.sendATPConfigResp(d, rx, tid, AssignedConfig{IP: assignedIP})
}

// assigner returns the egress as an AddressAssigner when it implements the optional
// capability AND is actively sourcing addresses (DHCP relay enabled), else nil
// (static-pool assignment). The AssignerActive gate is essential: the NAT/bridge egress
// structurally satisfies AddressAssigner in every mode but only does DHCP when relay is
// configured — without the gate, NAT mode would delegate to an egress whose AssignIP
// always fails, silently dropping every config request (the Mac never gets an IP).
func (s *Service) assigner() AddressAssigner {
	s.mu.Lock()
	e := s.egress
	s.mu.Unlock()
	if as, ok := e.(AddressAssigner); ok && as.AssignerActive() {
		return as
	}
	return nil
}

// assignViaEgress runs an egress-driven (DHCP) assignment and replies when it
// resolves. Aborts silently if the service stops first or the egress fails (the Mac
// retries). The resolved lease is recorded so inbound IP for it routes back here.
func (s *Service) assignViaEgress(as AddressAssigner, d ddp.Datagram, rx router.RoutedPort, tid uint16, requested IPv4, atNet uint16, atNode uint8, stop chan struct{}) {
	defer s.wg.Done()
	type result struct {
		cfg AssignedConfig
		ok  bool
	}
	done := make(chan result, 1)
	go func() {
		cfg, ok := as.AssignIP(atNet, atNode, requested)
		done <- result{cfg, ok}
	}()
	select {
	case <-stop:
		return
	case r := <-done:
		if !r.ok || r.cfg.IP.IsZero() {
			return // no reply; the Mac retries
		}
		s.pool.RegisterExternal(r.cfg.IP, atNet, atNode)
		s.bump(&s.assigns)
		s.logAllocated(r.cfg.IP, atNet, atNode)
		// In DHCP-relay mode the lease is on the real LAN subnet; adopt the DHCP-supplied
		// router as the advertised IPGATEWAY identity so MacTCP is given a gateway on its
		// own subnet (see AssignedConfig.Router). Done once, when we first learn a router.
		s.adoptGatewayIP(r.cfg.Router)
		s.sendATPConfigResp(d, rx, tid, r.cfg)
	}
}

// logAllocated emits the Info audit line for a freshly assigned MacIP address.
func (s *Service) logAllocated(ip IPv4, atNet uint16, atNode uint8) {
	s.logger.Log(log.Info, "macip: allocated IP",
		log.Str("ip", string(ipv4String(ip))),
		log.Int("at_network", int64(atNet)),
		log.Int("at_node", int64(atNode)))
}

// adoptGatewayIP makes the DHCP-supplied router the gateway's advertised IPGATEWAY
// identity when we do not already advertise it. This is what lets a bridge + DHCP-relay
// gateway hand MacTCP a router on the client's own subnet: the NBP object name is the
// gateway IP as text (spec §2), and MacTCP uses that as its gateway, so it must be
// re-registered under the router IP. A no-op when router is zero or already adopted.
func (s *Service) adoptGatewayIP(router IPv4) {
	if router.IsZero() {
		return
	}
	s.mu.Lock()
	if s.cfg.GatewayIP == router {
		s.mu.Unlock()
		return
	}
	old := s.cfg.GatewayIP
	s.cfg.GatewayIP = router
	names := s.nbp
	zone := s.cfg.Zone
	s.mu.Unlock()

	if names != nil {
		// Swap the NBP registration to the new identity so a Chooser/NBP lookup returns the
		// on-subnet gateway. Unregister the old name only if it was ever registered (non-zero).
		if !old.IsZero() {
			names.UnregisterName(ipv4String(old), []byte("IPGATEWAY"), zone)
		}
		names.RegisterName(ipv4String(router), []byte("IPGATEWAY"), zone, Socket)
	}
	s.logger.Log1(log.Info, "macip: adopted DHCP-supplied gateway as advertised IPGATEWAY",
		log.Str("gateway", string(ipv4String(router))))
}

// sendATPConfigResp builds and sends an ATP TResp with the IP configuration. Zero-valued
// fields in cfg fall back to the service Config defaults. The reply layout and length
// mirror the original macipgw struct macip_req (see configUserLen): an 8-byte control
// (version/pad/function) followed by a 33-byte data block (ip/nameserver/broadcast/
// pad2/subnet/pad3/pad4/pad5 = 32 bytes, then the first NUL of the error field) — the
// exact "sizeof(macip_req) - 21 = 41" success length macipgw emits.
func (s *Service) sendATPConfigResp(d ddp.Datagram, rx router.RoutedPort, tid uint16, cfg AssignedConfig) {
	ns := cfg.Nameserver
	if ns.IsZero() {
		ns = s.cfg.Nameserver
	}
	bc := cfg.Broadcast
	if bc.IsZero() {
		bc = s.cfg.Broadcast
	}
	mask := cfg.SubnetMask
	if mask.IsZero() {
		mask = s.cfg.SubnetMask
	}
	resp := s.newConfigReply(tid, macIPFuncAssign)
	copy(resp[12:16], cfg.IP[:])
	copy(resp[16:20], ns[:])
	copy(resp[20:24], bc[:])
	// resp[24:28] = pad2; resp[28:32] = subnet; resp[32:44] = pad3/4/5; resp[44] = NUL.
	copy(resp[28:32], mask[:])
	s.rtr.Reply(d, rx, ddpTypeATP, resp)
}

// sendATPConfigError sends a MACIP_ERROR reply carrying msg in the error field, matching
// macipgw's failure path (config_input: MACIP_ERROR + error_noip/error_noop, with len
// extended by the NUL-terminated string). The nameserver/broadcast/subnet fields are
// still populated (macipgw always sets them before the switch), only the function code
// and appended error string differ.
func (s *Service) sendATPConfigError(d ddp.Datagram, rx router.RoutedPort, tid uint16, msg string) {
	if len(msg) >= configErrLen {
		msg = msg[:configErrLen-1] // never overrun the 22-byte error[] field
	}
	resp := s.newConfigReply(tid, macIPFuncError)
	copy(resp[16:20], s.cfg.Nameserver[:])
	copy(resp[20:24], s.cfg.Broadcast[:])
	copy(resp[28:32], s.cfg.SubnetMask[:])
	// Error string starts at the error field (data offset 32 → resp offset 44). macipgw
	// copies sizeof(str) bytes (including the terminating NUL) and lengthens the reply
	// by sizeof(str)-1 beyond the 41-byte base, i.e. len(msg) extra bytes.
	errStart := 4 + configCtrlLen + 32 // ATP hdr(4) + control(8) + 32-byte data block
	resp = append(resp, make([]byte, len(msg)+1)...)
	copy(resp[errStart:], msg)
	s.rtr.Reply(d, rx, ddpTypeATP, resp)
}

// newConfigReply allocates a base config reply: 4-byte ATP TResp header (EOM, seq 0,
// tid) + 8-byte control (version, pad, 32-bit function) + the 33-byte success data
// block, all zeroed except the header/version/function. fn is the MacIP function code
// written big-endian (macIPFuncError = -1 → 0xFFFFFFFF, matching htonl(MACIP_ERROR)).
func (s *Service) newConfigReply(tid uint16, fn int32) []byte {
	resp := make([]byte, 4+configUserLen)
	resp[0] = atpFuncTResp | atpEOM
	resp[1] = 0 // seq 0
	resp[2] = byte(tid >> 8)
	resp[3] = byte(tid)
	resp[4] = byte(macIPVersion >> 8)
	resp[5] = byte(macIPVersion)
	// resp[6:8] = pad
	u := uint32(fn)
	resp[8] = byte(u >> 24)
	resp[9] = byte(u >> 16)
	resp[10] = byte(u >> 8)
	resp[11] = byte(u)
	return resp
}

// handleMacIPData processes a DDP type 22 packet: a raw IP packet from a Mac.
func (s *Service) handleMacIPData(d ddp.Datagram) {
	if len(d.Data) < 20 {
		s.bump(&s.dropped)
		return
	}
	var dstIP, srcIP IPv4
	copy(dstIP[:], d.Data[16:20])
	copy(srcIP[:], d.Data[12:16])
	s.pool.updateSeen(d.SrcNetwork, d.SrcNode)
	// Snoop the source IP↔AppleTalk binding so a STATICALLY addressed Mac (one that
	// never leased from our pool) is reachable for return traffic — mirrors the
	// original macipgw arp_set() on every received IP packet (see pool.learnSource).
	if s.pool.learnSource(srcIP, d.SrcNetwork, d.SrcNode) {
		s.logger.Log(log.Info, "macip: learned Mac IP↔AppleTalk binding (address taken)",
			log.Str("ip", string(ipv4String(srcIP))),
			log.Int("at_network", int64(d.SrcNetwork)),
			log.Int("at_node", int64(d.SrcNode)))
	}

	// Learn what this Mac advertises about its own receive capacity (window/ACK) from
	// the segment it is sending. Observation only (diagnostics).
	s.observeMacTCP(d.Data)

	// Forward the Mac's segment to the egress UNMODIFIED — matching the golden reference
	// (macipgw macip_output and the pre-refactor main branch, which never rewrite an
	// egress-bound packet). We used to clamp the Mac's advertised TCP receive window down
	// to a few DDP segments here, believing a classic MacTCP receiver over-advertises and
	// the peer must be throttled. That was WRONG for NAT mode, where the egress is our own
	// OSNAT TCP-terminating proxy (adapter/macipgw/nat): OSNAT already paces itself on the
	// Mac's REAL advertised window (space = macAck + macWindow − ourSeq) and never
	// retransmits, so feeding it a falsified small window starved that loop — a single
	// dropped segment drove space to 0 and the flow DEADLOCKED (capture ltoudp-netboot.pcap:
	// the Mac ACKs only the first of a burst, then the connection wedges and RSTs). The
	// real burst constraint is the LToUDP transport, and the per-node link pace
	// (link.Pace) is the right and only place to address it — not a TCP-window rewrite on
	// the data path. So: no window clamp; let OSNAT own MSS (flow.mss) and window.
	out := d.Data

	// If the destination is another pool client, deliver directly over AppleTalk.
	if atNet, atNode, ok := s.pool.lookupByIP(dstIP); ok {
		s.routeIPToMac(atNet, atNode, out)
		return
	}
	// Otherwise hand it to the IP egress.
	if s.egress != nil {
		if err := s.egress.SendIP(out); err != nil {
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

// routeIPToMac wraps an IP packet in DDP type 22 and routes it to a Mac client,
// forwarding it UNMODIFIED — matching the golden reference (macipgw macip_output and
// the pre-refactor main branch, neither of which rewrites a packet bound for the Mac).
// We used to clamp the inbound TCP MSS option here so no peer segment could exceed one
// DDP packet, but that is unnecessary and off-model: in NAT mode the SYN-ACK toward the
// Mac is synthesised by our own OSNAT proxy (adapter/macipgw/nat), which already sets the
// MSS from flow.mss (capped at osNATMaxSegment); on the pool client→client path both ends
// are classic Macs whose own MSS (≤536) governs. Oversize-to-Mac therefore does not arise,
// and rewriting the segment only risks the kind of throttle-interaction regression that
// deadlocked NAT (see handleMacIPData). The transport burst constraint on LToUDP is the
// per-node link pace's job (link.Pace), not a TCP rewrite. A copy is still taken because
// the DDP datagram needs its own buffer and the caller may pass a shared inbound slice.
func (s *Service) routeIPToMac(atNet uint16, atNode uint8, pkt []byte) {
	if !validATEndpoint(atNet, atNode) {
		return
	}
	data := append([]byte(nil), pkt...)
	err := s.rtr.Route(ddp.Datagram{
		DestNetwork: atNet,
		DestNode:    atNode,
		DestSocket:  Socket,
		SrcSocket:   Socket,
		DDPType:     ddpTypeMacIP,
		Data:        data,
	}, true)
	if err == nil {
		s.bump(&s.dataIn)
	} else {
		s.bump(&s.dropped)
	}
}

// observeMacTCP records the receive-window/ACK a Mac advertised in a segment it sent
// (Mac→peer), keyed by the flow 4-tuple. Observation only: diagnostics plus a record of
// the pre-clamp window the throttle (clampAdvertisedWindow) is acting on. Best-effort
// and bounded by maxTrackedFlows.
func (s *Service) observeMacTCP(pkt []byte) {
	f, ok := observeFromMac(pkt)
	if !ok {
		return
	}
	seg := tcpSegment(pkt)
	if seg == nil {
		return
	}
	var macIP, peerIP IPv4
	copy(macIP[:], pkt[12:16])  // source = the Mac
	copy(peerIP[:], pkt[16:20]) // dest = the peer
	key := flowKey{
		macIP:    macIP,
		peerIP:   peerIP,
		macPort:  uint16(seg[0])<<8 | uint16(seg[1]),
		peerPort: uint16(seg[2])<<8 | uint16(seg[3]),
	}
	s.flowMu.Lock()
	if _, exists := s.flows[key]; exists || len(s.flows) < maxTrackedFlows {
		s.flows[key] = f
	}
	s.flowMu.Unlock()
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

// Dependencies declares MacIP's start-order edges: the AppleTalk router (it is a DDP
// service on socket 72) and NBP (it registers its IPGATEWAY name via NBP). Both edges
// drop automatically when their target is not built.
func (s *Service) Dependencies() []string { return []string{router.Name, nbp.Name} }

// compile-time assertions.
var (
	_ router.Service        = (*Service)(nil)
	_ component.Component   = (*Service)(nil)
	_ component.DependsOn   = (*Service)(nil)
	_ component.Statful     = (*Service)(nil)
	_ component.Describable = (*Service)(nil)
	_ component.Enableable  = (*Service)(nil)
)
