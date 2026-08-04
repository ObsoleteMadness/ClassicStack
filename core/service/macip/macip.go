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
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/atp"
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

	// nbpTypeIPGateway is the NBP type the gateway registers its own IP under (§3.2.4.2).
	nbpTypeIPGateway = "IPGATEWAY"
	// nbpTypeIPAddress is the NBP type a MacIP host (and the gateway, for addresses in its
	// range) registers a leased IP under, in dotted-decimal (§3.2.2.4 / §3.2.4.3). The
	// reregistration search (§3.7) looks these up as "=:IPADDRESS@*".
	nbpTypeIPAddress = "IPADDRESS"

	// ATP control byte values.
	atpFuncTReq  = 0x40
	atpFuncTResp = 0x80
	atpEOM       = 0x10

	// atpHeaderLen is the fixed ATP header on the wire (Inside AppleTalk Ch. 9): control(1)
	// + bitmap/seq(1) + transaction-id(2) + 4 ATP user bytes = 8 bytes. The MacIP control
	// struct is carried in the ATP *data* that follows this header — NOT in the user bytes
	// (which macipgw's atp library keeps separate). This matches every other ATP service in
	// core (e.g. ZIP GetZoneList reads its function code from the user bytes at Data[4:8]).
	atpHeaderLen = 8

	// macIPCtrlLen is the minimum MacIP control in the ATP DATA: mipr_function(4). (version/
	// pad ride the ATP user bytes, not the data — see handleATPConfig.)
	macIPCtrlLen = 4

	// The config reply mirrors the original macipgw struct macip_req (macip.c, after
	// njroadfan's "send back a complete config packet" fix). macipgw's struct is:
	//
	//   control  version(2) pad(2) function(4)                             = 8 bytes
	//   data     ipaddr(4) nameserver(4) broadcast(4) pad2(4) subnet(4)
	//            pad3(4) pad4(4) pad5(4)                                    = 32 bytes
	//   error    char[22]                                                  = 22 bytes
	//
	// On the wire the control struct STRADDLES the ATP header/data boundary: version(2)+
	// pad(2) ride the ATP USER bytes (the last 4 of the 8-byte ATP header, echoed by the
	// header), and only function(4) sits at the start of the ATP DATA (wire-verified against
	// a real MacTCP client — see handleATPConfig and errata; a prior reading that put the
	// whole control struct in the data shifted every address +4 and no client could parse
	// the config). So the ATP-DATA the reply carries is function(4) + the 32-byte address
	// block + the leading NUL of error[] = 37 bytes; the 8-byte ATP TResp header is prepended
	// separately, so the wire buffer is atpHeaderLen + configUserLen. macipgw's success length
	// "sizeof(macip_req) - 21 = 41" counts the 4 user bytes too (37 + 4 = 41). On failure the
	// NUL-terminated error string is appended.
	configFuncLen   = 4                                   // mipr_function, at the start of the ATP data
	configFieldsLen = 32                                  // ip/ns/bcast/pad2/subnet/pad3/pad4/pad5
	configUserLen   = configFuncLen + configFieldsLen + 1 // 37 ATP-data bytes: function(4)+addresses(32)+NUL
	configErrLen    = 22                                  // error[] capacity in struct macip_req_data

	// expiryInterval is how often stale external/DHCP leases are evicted (passive aging).
	expiryInterval = 30 * time.Second

	// confirmPeriod is the NBP-ARP Confirm echo interval for active static leases (§3.8.2:
	// "every Confirm Period, 60 seconds if not configurable"). confirmMissLimit is how many
	// consecutive periods a lease may miss before it is reclaimed (§3.8.2: 5 periods → ~300s).
	confirmPeriod    = 60 * time.Second
	confirmMissLimit = 5
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
		s.nbp.RegisterName(ipv4String(s.cfg.GatewayIP), []byte(nbpTypeIPGateway), s.cfg.Zone, Socket)
	}

	go s.inboundLoop(ctx, stop)
	go s.expiryLoop(stop)

	// Reregistration search (§3.7 / draft §3.2.4.4): after a restart or crash the gateway
	// may otherwise reassign an address still held by a live MacIP host. Look up the already
	// -registered IPADDRESS names in the zone and seed the pool with any that fall in our
	// range, so those addresses are not handed out again. NBP has a fixed collection window,
	// so run it off the Start path. The Confirm loop (§3.8.2) then keeps those and all other
	// static leases alive by periodic NBP-ARP echo. Both need NBP wired to probe.
	if s.nbp != nil {
		s.wg.Add(2)
		go s.reregister(stop)
		go s.confirmLoop(stop)
	}

	s.mu.Lock()
	gw := s.cfg.GatewayIP
	zone := s.cfg.Zone
	network := s.cfg.Network
	nameserver := s.cfg.Nameserver
	broadcast := s.cfg.Broadcast
	subnet := s.cfg.SubnetMask
	hostCount := s.cfg.HostCount
	nat := s.cfg.NATEnabled
	hasEgress := s.egress != nil
	s.mu.Unlock()
	s.logger.Log(log.Info, "macip: started",
		log.Str("gateway", string(ipv4String(gw))),
		log.Str("network", string(ipv4String(network))),
		log.Str("subnet_mask", string(ipv4String(subnet))),
		log.Str("nameserver", string(ipv4String(nameserver))),
		log.Str("broadcast", string(ipv4String(broadcast))),
		log.Int("host_count", int64(hostCount)),
		log.Str("zone", string(zone)),
		log.Bool("nat", nat),
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
		s.nbp.UnregisterName(ipv4String(s.cfg.GatewayIP), []byte(nbpTypeIPGateway), zone)
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
			for _, ip := range s.pool.expire() {
				s.unregisterLeaseName(ip) // withdraw the IPADDRESS NBP name for the evicted lease
			}
		}
	}
}

// confirmLoop is the active NBP-ARP Confirm echo (§3.8.2): every confirmPeriod it probes
// each static lease's "<ip>:IPADDRESS@zone". A reply from the lease's own node refreshes it;
// a miss increments its counter, and after confirmMissLimit consecutive misses the lease is
// reclaimed and its IPADDRESS name withdrawn. Only runs when NBP is wired (it needs to
// probe); external/DHCP leases keep ageing passively via expiryLoop. Inbound IP data also
// counts as a liveness signal (updateSeen resets the miss count), so a chatty client is
// never probed to death.
func (s *Service) confirmLoop(stop chan struct{}) {
	defer s.wg.Done()
	t := time.NewTicker(confirmPeriod)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			for _, lease := range s.pool.staticLeases() {
				select {
				case <-stop:
					return
				default:
				}
				// The lease's own node answering "<ip>:IPADDRESS" means it is alive. We reuse
				// ipHeldByOther by asking whether ANYONE holds it and whether that responder is
				// the lease owner: a reply from the owner is a hit, no reply (or only a foreign
				// reply) is a miss for this owner.
				if s.ipConfirmedBy(lease.ip, lease.atNetwork, lease.atNode) {
					s.pool.confirmHit(lease.ip, lease.atNetwork, lease.atNode)
					continue
				}
				if s.pool.confirmMiss(lease.ip, lease.atNetwork, lease.atNode, confirmMissLimit) {
					s.unregisterLeaseName(lease.ip)
					s.logger.Log(log.Info, "macip: lease reclaimed after missed NBP-ARP confirms",
						log.Str("ip", string(ipv4String(lease.ip))),
						log.Int("at_network", int64(lease.atNetwork)),
						log.Int("at_node", int64(lease.atNode)))
				}
			}
		}
	}
}

// ipConfirmedBy runs an NBP-ARP Confirm probe for a lease and reports whether its owning
// node (atNet,atNode) answered — i.e. the client is still alive at that address. A reply
// from a DIFFERENT node is not a confirmation of THIS lease (it is a conflict the assign
// probe handles); no reply at all is likewise unconfirmed. No NBP ⇒ cannot probe ⇒ reports
// true (do not reclaim on a probe we cannot perform). Blocks up to probeWindow.
func (s *Service) ipConfirmedBy(ip IPv4, atNet uint16, atNode uint8) bool {
	s.mu.Lock()
	names := s.nbp
	zone := append([]byte(nil), s.cfg.Zone...)
	s.mu.Unlock()
	if names == nil {
		return true
	}
	for _, e := range names.LookupTimeout(ipv4String(ip), []byte(nbpTypeIPAddress), zone, probeWindow) {
		if e.Network == atNet && e.Node == atNode {
			return true
		}
	}
	return false
}

// handleATPConfig processes an ATP TReq on socket 72: an IP address request.
func (s *Service) handleATPConfig(d ddp.Datagram, rx router.RoutedPort) {
	atNet, atNode := normalizeATSource(d, rx)
	if !validATEndpoint(atNet, atNode) {
		return
	}
	// Decode the ATP header (control, bitmap, tid, 4 user bytes) via the core ATP codec.
	// The MacIP control rides in the ATP *data* that follows the 8-byte header. Wire-verified
	// against a real MacTCP client (see errata): mipr_function is the FIRST 4 bytes of the ATP
	// data (macReq[0:4]) — mipr_version / _mipr_pad1 are carried in the ATP USER bytes (the
	// last 4 of the 8-byte header), NOT re-emitted at the head of the data. An earlier reading
	// that placed function at macReq[4:8] (assuming version(2)+pad(2) prefixed the data)
	// mis-parsed every request as an unknown function (e.g. 0x00010000) so no client could
	// ever get a config. The user bytes are round-tripped into the reply (Apple IP Gateway
	// stamps a version there, Shiva K-STAR a 0x08 in the last byte — issue #17).
	hdr, err := atp.Decode(d.Data)
	if err != nil || hdr.FuncCode() != atp.FuncTReq {
		return
	}
	macReq := d.Data[atp.HeaderSize:]
	if len(macReq) < macIPCtrlLen {
		return
	}
	// mipr_function is the first 4 bytes of the ATP data.
	function := uint32(macReq[0])<<24 | uint32(macReq[1])<<16 | uint32(macReq[2])<<8 | uint32(macReq[3])

	// mipr_ipaddr (the optionally requested IP) follows the function.
	var requestedIP IPv4
	if len(macReq) >= 8 {
		copy(requestedIP[:], macReq[4:8])
	}

	// Only MACIP_ASSIGN and MACIP_SERVER are defined; anything else gets a MACIP_ERROR
	// reply carrying "Unknown Operation." — matching macipgw's switch default arm.
	if function != macIPFuncAssign && function != macIPFuncServer {
		s.logger.Log1(log.Info, "macip: unknown config function", log.Int("function", int64(function)))
		s.sendATPConfigError(d, rx, hdr, errNoOp)
		return
	}

	// Server-check (func=3): the reply is a MACIP_SERVER response whose first IP address is
	// all zeros — the ONLY wire difference from an ASSIGN response (issue #17, confirmed
	// against Shiva Fastpath 5 / K-STAR and Apple IP Gateway, and macipgw after njroadfan's
	// fix, which sets function=MACIP_SERVER and never touches mipr_ipaddr). It still refreshes
	// the client's lease if one exists so passive aging does not reclaim a live address.
	if function == macIPFuncServer {
		s.pool.updateSeen(atNet, atNode) // the probe proves the client is alive; refresh its lease
		s.sendATPConfigResp(d, rx, hdr, macIPFuncServer, AssignedConfig{})
		return
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
		go s.assignViaEgress(as, d, rx, hdr, requestedIP, atNet, atNode, stop)
		return
	}

	// Static-pool assignment. A fresh allocation is NBP-ARP-probed before it is handed out
	// (§3.8.2: assigned addresses must be registered and resolved via NBP ARP), which blocks
	// for a probe window — so run it off the inbound loop, like the DHCP path.
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.wg.Add(1)
	stop := s.stop
	s.mu.Unlock()
	go s.assignStatic(d, rx, hdr, requestedIP, atNet, atNode, stop)
}

// maxAssignProbes bounds how many probe-and-retry rounds a single assign will attempt
// before giving up (each round is a duplicate the NBP probe rejected). Prevents an
// unbounded loop when many pool addresses are occupied by un-snooped live hosts.
const maxAssignProbes = 8

// assignStatic performs a static-pool assignment with a pre-assign NBP-ARP duplicate probe
// (§3.8.2). It reuses an existing lease immediately (no probe — the client already owns it);
// for a fresh candidate it probes "<ip>:IPADDRESS@zone" and, if a live host other than the
// requester answers, records the conflict and retries with a different address. Replies
// MACIP_ERROR/"No Address Available." if the pool is exhausted or every candidate collided.
// Aborts silently if the service stops first.
func (s *Service) assignStatic(d ddp.Datagram, rx router.RoutedPort, hdr atp.Header, requestedIP IPv4, atNet uint16, atNode uint8, stop chan struct{}) {
	defer s.wg.Done()

	for range maxAssignProbes {
		assignedIP, fresh, ok := s.pool.assign(requestedIP, atNet, atNode)
		if !ok {
			s.logger.Log(log.Warn, "macip: address pool exhausted, no lease available")
			s.sendATPConfigError(d, rx, hdr, errNoIP)
			return
		}
		// A reused lease is already the client's — no probe needed.
		if !fresh {
			s.bump(&s.assigns)
			s.sendATPConfigResp(d, rx, hdr, macIPFuncAssign, AssignedConfig{IP: assignedIP})
			return
		}
		// Fresh candidate: verify no live host already holds it (unless we are stopping).
		select {
		case <-stop:
			s.pool.release(assignedIP, atNet, atNode)
			return
		default:
		}
		if s.ipHeldByOther(assignedIP, atNet, atNode) {
			// Duplicate on the wire: mark it taken (frees the tentative slot) and retry with
			// a different address. Do NOT reuse the caller's requestedIP on retry — it just
			// collided — so subsequent rounds allocate a fresh slot.
			s.pool.noteConflict(assignedIP)
			s.logger.Log1(log.Info, "macip: candidate address in use on the wire, trying another",
				log.Str("ip", string(ipv4String(assignedIP))))
			requestedIP = IPv4{}
			continue
		}
		s.bump(&s.assigns)
		s.logAllocated(assignedIP, atNet, atNode)
		s.registerLeaseName(assignedIP) // publish IPADDRESS@zone so the lease is visible to NBP ARP
		s.sendATPConfigResp(d, rx, hdr, macIPFuncAssign, AssignedConfig{IP: assignedIP})
		return
	}
	// Too many collisions in a row — treat as no address available.
	s.logger.Log(log.Warn, "macip: no free address survived NBP-ARP probing")
	s.sendATPConfigError(d, rx, hdr, errNoIP)
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
func (s *Service) assignViaEgress(as AddressAssigner, d ddp.Datagram, rx router.RoutedPort, hdr atp.Header, requested IPv4, atNet uint16, atNode uint8, stop chan struct{}) {
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
		s.registerLeaseName(r.cfg.IP) // publish IPADDRESS@zone for the DHCP-relayed lease
		// In DHCP-relay mode the lease is on the real LAN subnet; adopt the DHCP-supplied
		// router as the advertised IPGATEWAY identity so MacTCP is given a gateway on its
		// own subnet (see AssignedConfig.Router). Done once, when we first learn a router.
		s.adoptGatewayIP(r.cfg.Router)
		s.sendATPConfigResp(d, rx, hdr, macIPFuncAssign, r.cfg)
	}
}

// probeWindow bounds the pre-assign / Confirm NBP-ARP lookups. A live host on the segment
// answers NBP within a few hundred ms; keeping this short bounds how long an assign or the
// Confirm loop blocks. It is deliberately shorter than the discovery window.
const probeWindow = 500 * time.Millisecond

// ipHeldByOther runs an NBP-ARP probe (§3.8.2 "registered and resolved using NBP ARP"): it
// looks up "<ip>:IPADDRESS@zone" and reports true if a live host OTHER than (atNet,atNode)
// answers — i.e. the address is already in use and must not be assigned to this requester.
// A reply from the requester's own node (it still holds a prior registration) is not a
// conflict. With no NBP service wired it cannot probe, so it reports false (best-effort;
// falls back to the pool's own bookkeeping). Blocks up to probeWindow — call off the
// inbound loop.
func (s *Service) ipHeldByOther(ip IPv4, atNet uint16, atNode uint8) bool {
	s.mu.Lock()
	names := s.nbp
	zone := append([]byte(nil), s.cfg.Zone...)
	s.mu.Unlock()
	if names == nil {
		return false
	}
	for _, e := range names.LookupTimeout(ipv4String(ip), []byte(nbpTypeIPAddress), zone, probeWindow) {
		// A responder at a different AppleTalk node holds this IP → genuine conflict.
		if e.Network != atNet || e.Node != atNode {
			return true
		}
	}
	return false
}

// reregister performs the startup reregistration search (§3.7). It issues an NBP lookup
// for "=:IPADDRESS@*", and for each responder whose object name parses to an IP inside our
// pool range, claims that address for the responder's AppleTalk endpoint so a later assign
// never hands it out again. Runs on its own goroutine (the NBP lookup blocks for a
// collection window); aborts if the service stops first.
func (s *Service) reregister(stop chan struct{}) {
	defer s.wg.Done()

	s.mu.Lock()
	names := s.nbp
	zone := append([]byte(nil), s.cfg.Zone...)
	s.mu.Unlock()
	if names == nil {
		return
	}

	// Run the (blocking) lookup on a helper goroutine so we can abort promptly on Stop.
	type reg struct {
		ip     IPv4
		atNet  uint16
		atNode uint8
	}
	done := make(chan []reg, 1)
	go func() {
		ents := names.Lookup([]byte{'='}, []byte(nbpTypeIPAddress), zone)
		var regs []reg
		for _, e := range ents {
			ip, ok := parseDottedIPv4(e.Object)
			if !ok {
				continue
			}
			regs = append(regs, reg{ip: ip, atNet: e.Network, atNode: e.Node})
		}
		done <- regs
	}()

	var regs []reg
	select {
	case <-stop:
		return
	case regs = <-done:
	}

	seeded := 0
	for _, r := range regs {
		if !validATEndpoint(r.atNet, r.atNode) {
			continue
		}
		// Skip our own gateway IP (advertised as IPGATEWAY, not a client lease).
		s.mu.Lock()
		isGateway := r.ip == s.cfg.GatewayIP
		s.mu.Unlock()
		if isGateway {
			continue
		}
		// Only seed addresses inside our assignable pool range; assign() claims the exact
		// slot to the responder's endpoint (a no-op if it is already leased to it).
		if _, fresh, ok := s.pool.assign(r.ip, r.atNet, r.atNode); ok && fresh {
			s.registerLeaseName(r.ip)
			seeded++
			s.logger.Log(log.Info, "macip: reregistered prior lease from NBP",
				log.Str("ip", string(ipv4String(r.ip))),
				log.Int("at_network", int64(r.atNet)),
				log.Int("at_node", int64(r.atNode)))
		}
	}
	if seeded > 0 {
		s.logger.Log1(log.Info, "macip: reregistration seeded prior leases", log.Int("count", int64(seeded)))
	}
}

// registerLeaseName is intentionally a NO-OP: the gateway must NOT register an IPADDRESS
// NBP name for a client's leased address.
//
// Per the MacIP draft §3.2.2.4 the MacIP HOST registers "<ip>:IPADDRESS@*" for its OWN
// address — that registration is the client's, not the gateway's. When the gateway ALSO
// stood up a standing "<ip>:IPADDRESS" name, it shadowed the client: after a Mac reboots
// and re-leases the same address, the Mac's own NBP name-registration conflict check (a
// LkUp for "<ip>:IPADDRESS" before it registers) got answered by OUR stale name, so the
// Mac saw its address as already-in-use, aborted MacTCP init, and looped ASSIGN→SERVER→
// ASSIGN forever (wire-confirmed in ltoudp-netboot.pcap: two "192.168.100.2:IPADDRESS"
// entries — the Mac's and ours). It also violated §3.8's "NBP Proxy ARP MUST NOT respond
// to wildcard IPADDRESS lookups", since a real registered name answers "=:IPADDRESS@*".
//
// The gateway's legitimate NBP-ARP roles do NOT need this registration: the Confirm loop
// (§3.8.2) and the startup reregistration search (§3.7) both PROBE for the HOSTS' own
// registrations, and NBP Proxy ARP answers only SPECIFIC delivery lookups. Kept as a
// no-op (rather than deleting the call sites) so the lease lifecycle reads intact.
func (s *Service) registerLeaseName(ip IPv4) { _ = ip }

// unregisterLeaseName is the no-op counterpart to registerLeaseName (the gateway never
// registered a client IPADDRESS name, so there is nothing to withdraw). See registerLeaseName.
func (s *Service) unregisterLeaseName(ip IPv4) { _ = ip }

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
			names.UnregisterName(ipv4String(old), []byte(nbpTypeIPGateway), zone)
		}
		names.RegisterName(ipv4String(router), []byte(nbpTypeIPGateway), zone, Socket)
	}
	s.logger.Log1(log.Info, "macip: adopted DHCP-supplied gateway as advertised IPGATEWAY",
		log.Str("gateway", string(ipv4String(router))))
}

// Config-reply byte offsets, past the 8-byte ATP header (atpHeaderLen). The full config
// data block — space for all EIGHT IP addresses — is emitted in EVERY reply type
// (ASSIGN/SERVER/ERROR); only the first IP and (for errors) the appended string differ.
// Confirmed against Shiva Fastpath 5 / K-STAR and Apple IP Gateway (issue #17) and macipgw
// after njroadfan's "send back a complete config packet" fix.
// The MacIP control in the ATP DATA is just mipr_function(4) — mipr_version/_pad ride the
// ATP USER bytes (echoed by the header), NOT the data (wire-verified; see handleATPConfig
// and errata). So the reply data is function(4) then the eight-address block, matching what
// a real MacTCP client parses. (A prior layout prefixed version(2)+pad(2) here, shifting
// every address +4 on the wire, so the client read a garbage config and refused to come up.)
const (
	respFuncOff   = atpHeaderLen                                  // +8  mipr_function
	respIPOff     = respFuncOff + 4                               // +12 assigned IP (0.0.0.0 for SERVER/ERROR)
	respNSOff     = respIPOff + 4                                 // +16 nameserver
	respBcastOff  = respIPOff + 8                                 // +20 broadcast
	respSubnetOff = respIPOff + 16                                // +28 subnet mask (the 5th address; Apple IP Gateway convention)
	respErrOff    = respFuncOff + configFuncLen + configFieldsLen // error[] field, past function(4)+addresses
)

// sendATPConfigResp builds and sends an ATP TResp with the IP configuration. fn is the
// MacIP function code (MACIP_ASSIGN or MACIP_SERVER); for MACIP_SERVER the first IP address
// is left zero (only ASSIGN carries a value there — issue #17). Zero-valued fields in cfg
// fall back to the service Config defaults. The reply layout and length mirror macipgw's
// struct macip_req (see configUserLen): the 8-byte ATP header, an 8-byte control
// (version/pad/function), then a 33-byte data block (ip/nameserver/broadcast/pad2/subnet/
// pad3/pad4/pad5 = 32 bytes + the first NUL of the error field) — the exact
// "sizeof(macip_req) - 21 = 41" success length.
func (s *Service) sendATPConfigResp(d ddp.Datagram, rx router.RoutedPort, req atp.Header, fn int32, cfg AssignedConfig) {
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
	resp := s.newConfigReply(req, fn)
	if fn == macIPFuncAssign {
		copy(resp[respIPOff:respIPOff+4], cfg.IP[:]) // SERVER leaves the first IP zeroed
	}
	copy(resp[respNSOff:respNSOff+4], ns[:])
	copy(resp[respBcastOff:respBcastOff+4], bc[:])
	copy(resp[respSubnetOff:respSubnetOff+4], mask[:])
	s.logger.Log(log.Info, "macip: config reply",
		log.Str("ip", string(ipv4String(cfg.IP))),
		log.Str("nameserver", string(ipv4String(ns))),
		log.Str("subnet_mask", string(ipv4String(mask))))
	s.rtr.Reply(d, rx, ddpTypeATP, resp)
}

// sendATPConfigError sends a MACIP_ERROR reply carrying msg in the error field, matching
// macipgw's failure path (config_input: MACIP_ERROR + error_noip/error_noop, with len
// extended by the NUL-terminated string). The full config block is still present and the
// first IP address is zero (like SERVER); only the function code and the appended error
// string differ. The nameserver/broadcast/subnet fields are still populated (macipgw always
// sets them before the switch).
func (s *Service) sendATPConfigError(d ddp.Datagram, rx router.RoutedPort, req atp.Header, msg string) {
	if len(msg) >= configErrLen {
		msg = msg[:configErrLen-1] // never overrun the 22-byte error[] field
	}
	resp := s.newConfigReply(req, macIPFuncError)
	copy(resp[respNSOff:respNSOff+4], s.cfg.Nameserver[:])
	copy(resp[respBcastOff:respBcastOff+4], s.cfg.Broadcast[:])
	copy(resp[respSubnetOff:respSubnetOff+4], s.cfg.SubnetMask[:])
	// The error string starts at the error[] field. macipgw copies sizeof(str) bytes
	// (including the terminating NUL) and lengthens the reply by sizeof(str)-1 beyond the
	// 41-byte base, i.e. len(msg) extra bytes.
	resp = append(resp, make([]byte, len(msg)+1)...)
	copy(resp[respErrOff:], msg)
	s.rtr.Reply(d, rx, ddpTypeATP, resp)
}

// newConfigReply allocates a base config reply: the 8-byte ATP TResp header (EOM, seq 0,
// tid, user bytes) + the 37-byte MacIP data block (function(4) + the 32-byte address block +
// the leading NUL of error[]), all zeroed except the header and function. fn is the MacIP
// function code written big-endian (macIPFuncError = -1 → 0xFFFFFFFF, matching
// htonl(MACIP_ERROR)).
//
// mipr_version / _mipr_pad1 are NOT written into the data — they ride the ATP USER bytes.
// The reply ALWAYS carries version = macIPVersion (1) in the top two user bytes and 0 in the
// pad, exactly as macipgw sets macip_req.version on every reply and the pre-refactor gateway
// did. This is NOT an echo of the request's user bytes: a real MacTCP client sends arbitrary
// bytes there (observed e.g. 0x001addfc) and READS the version back from the reply — echoing
// its junk (0x001a) instead of stamping 1 made MacTCP reject the config as a version mismatch
// and refuse to bring up its stack. function is the FIRST 4 bytes of the ATP data
// (respFuncOff), matching where the client sends it in the request.
func (s *Service) newConfigReply(req atp.Header, fn int32) []byte {
	// version(2) in the high half, pad(2) = 0 in the low half.
	userData := uint32(macIPVersion) << 16
	respHdr := atp.Header{
		Control:  atp.TRESP | atp.EOM,
		Bitmap:   0, // sequence 0
		TransID:  req.TransID,
		UserData: userData,
	}
	resp := respHdr.Encode(make([]byte, 0, atpHeaderLen+configUserLen))
	resp = append(resp, make([]byte, configUserLen)...)
	// MacIP control in the ATP data is just mipr_function(4) at respFuncOff.
	u := uint32(fn)
	resp[respFuncOff] = byte(u >> 24)
	resp[respFuncOff+1] = byte(u >> 16)
	resp[respFuncOff+2] = byte(u >> 8)
	resp[respFuncOff+3] = byte(u)
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
		s.registerLeaseName(srcIP) // a snooped static-Mac address is a lease too — publish it
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

// parseDottedIPv4 parses a dotted-decimal IPv4 (the NBP object form ipv4String emits, e.g.
// "192.168.1.2") back into an IPv4. It is the inverse of ipv4String and stays reflection-
// free (no net/strconv). Returns ok=false on any malformed input: wrong octet count, an
// empty or >255 octet, a leading '+'/'-', or a non-digit byte.
func parseDottedIPv4(b []byte) (IPv4, bool) {
	var out IPv4
	octet := 0 // current octet index (0..3)
	val := -1  // accumulated value for the current octet; -1 = no digit yet
	for _, c := range b {
		switch {
		case c >= '0' && c <= '9':
			if val < 0 {
				val = 0
			}
			val = val*10 + int(c-'0')
			if val > 255 {
				return IPv4{}, false
			}
		case c == '.':
			if val < 0 || octet >= 3 {
				return IPv4{}, false // empty octet or too many dots
			}
			out[octet] = byte(val)
			octet++
			val = -1
		default:
			return IPv4{}, false // non-digit, non-dot
		}
	}
	if octet != 3 || val < 0 {
		return IPv4{}, false // need exactly four octets, last one non-empty
	}
	out[3] = byte(val)
	return out, true
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
