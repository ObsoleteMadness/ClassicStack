// Package macipgw is the IP-side egress adapter for the MacIP gateway: the
// physical-network half of macipgw that the core service (core/service/macip)
// delegates to through its IPEgress seam. Core owns the AppleTalk protocol, the
// lease pool, and stats; this adapter moves IP packets between Mac clients and the
// real network over a libpcap raw-Ethernet link, doing proxy ARP, NAT, and DHCP
// relay — none of which core (TinyGo-clean, no net package) may do itself.
//
// Three modes, selected by Config (ported from the legacy service/macip):
//
//   - bridge (default): clients get static-pool IPs on an existing subnet; the
//     adapter answers proxy ARP for them and sends their off-subnet IP directly via
//     the link (return traffic needs a host route to the MacIP subnet, or use DHCP).
//   - nat: off-subnet client traffic is forwarded through the host OS network stack
//     (real sockets) so the host IP is the NAT source — no host route needed. ICMP
//     ping to the gateway IP itself is answered locally.
//   - dhcp relay: client addresses are obtained by relaying DHCP onto the IP-side
//     network with a fabricated per-Mac MAC; the adapter implements macip.AddressAssigner
//     so core delegates assignment to it.
//
// Ring: ADAPTER. Gated `//go:build pcap || macipgw` because it needs the cgo/libpcap
// FrameLink; without the tag the package is empty (see macipgw_stub.go) so headless /
// TinyGo builds drop it and MacIP runs AppleTalk-only.
package macipgw

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/adapter/link/pcap"
	"github.com/ObsoleteMadness/ClassicStack/adapter/macipgw/nat"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/service/macip"
)

// Config is the IP-side configuration for the egress. The compose edge builds it from
// the MacIP section (macip.Section.EgressParams), filling in any auto-detected fields.
type Config struct {
	Interface      string // pcap device for the IP-side network (required)
	HostMAC        string // IP-side host MAC (colon/dash hex; required)
	HostIP         string // IP-side host IPv4 (dotted quad; may be empty)
	DefaultGateway string // upstream gateway IPv4 (dotted quad; required for off-subnet egress)
	GatewayIP      string // gateway IP advertised to clients (the gateway's own IP)
	Network        string // subnet network base (dotted quad)
	SubnetMask     string // subnet mask (dotted quad)
	NATEnabled     bool   // OS-stack NAT for off-subnet traffic
	DHCPRelay      bool   // relay DHCP for client addresses
}

// Egress is the IP-side network seam for the MacIP gateway. It satisfies
// macip.IPEgress, and macip.AddressAssigner when DHCP relay is enabled.
type Egress struct {
	cfg     Config
	log     *slog.Logger
	gwIP    net.IP
	network *net.IPNet

	ether *etherIPLink
	osnat *nat.OSNAT  // non-nil in NAT mode
	dhcp  *dhcpClient // non-nil in DHCP-relay mode

	mu       sync.Mutex
	inbound  func([]byte) // core's inbound callback (set via SetInbound)
	ownsIP   func(macip.IPv4) bool
	stopOnce sync.Once
	stop     chan struct{}
	started  bool
}

// compile-time assertions.
var (
	_ macip.IPEgress        = (*Egress)(nil)
	_ macip.AddressAssigner = (*Egress)(nil)
)

// New builds an IP-side egress over a fresh libpcap link on cfg.Interface, applying a
// MacIP-shaped BPF filter (ARP + subnet traffic, plus DHCP replies in relay mode). The
// returned Egress is injected into the MacIP core via Service.SetEgress; call Start
// once before the service starts and Close on shutdown. ownsIP is the core's lease
// predicate (Service.OwnsIP) used for proxy ARP and inbound filtering.
func New(cfg Config, ownsIP func(macip.IPv4) bool, log *slog.Logger) (*Egress, error) {
	if log == nil {
		log = slog.Default()
	}
	if cfg.Interface == "" {
		return nil, fmt.Errorf("macipgw: interface is required")
	}
	mac, err := parseEthernet(cfg.HostMAC)
	if err != nil {
		return nil, fmt.Errorf("macipgw: host MAC: %w", err)
	}
	gwIP := net.ParseIP(cfg.GatewayIP).To4()
	netIP := net.ParseIP(cfg.Network).To4()
	mask := net.ParseIP(cfg.SubnetMask).To4()
	var ipNet *net.IPNet
	if netIP != nil && mask != nil {
		ipNet = &net.IPNet{IP: netIP.Mask(net.IPMask(mask)), Mask: net.IPMask(mask)}
	}
	hostIP := net.ParseIP(cfg.HostIP).To4()
	defGW := net.ParseIP(cfg.DefaultGateway).To4()

	// Open + BPF-filter the IP-side link.
	fl, err := pcap.Open(pcap.DefaultMacIPConfig(cfg.Interface))
	if err != nil {
		return nil, fmt.Errorf("macipgw: open %s: %w", cfg.Interface, err)
	}
	if ff, ok := fl.(link.FilterableLink); ok && ipNet != nil {
		if err := ff.SetFilter(macipBPFFilter(ipNet, cfg.DHCPRelay)); err != nil {
			log.Warn("macipgw: BPF filter rejected; capturing unfiltered", "err", err)
		}
	}

	e := &Egress{
		cfg:     cfg,
		log:     log,
		gwIP:    gwIP,
		network: ipNet,
		ownsIP:  ownsIP,
		stop:    make(chan struct{}),
	}

	ether, err := newEtherIPLink(fl, mac, hostIP, ipNet, defGW, e.isOurClient, log)
	if err != nil {
		_ = fl.Close()
		return nil, err
	}
	e.ether = ether
	ether.onInbound = e.deliverInbound

	if cfg.NATEnabled {
		e.osnat = nat.New(e.deliverInbound, log)
	}
	if cfg.DHCPRelay {
		e.dhcp = newDHCPClient(ether, log, e.stop)
		ether.onDHCP = e.dhcp.handleReply
	}
	return e, nil
}

// Start brings the IP link up (capture + gateway ARP prime). Idempotent.
func (e *Egress) Start() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.started {
		return
	}
	e.started = true
	e.ether.start()
	mode := "bridge"
	if e.cfg.NATEnabled {
		mode = "nat"
	}
	e.log.Info("macipgw: IP egress started", "iface", e.cfg.Interface, "mode", mode, "dhcp_relay", e.cfg.DHCPRelay)
}

// Close stops the egress and frees the link and forwarding state. Idempotent.
func (e *Egress) Close() error {
	e.stopOnce.Do(func() {
		close(e.stop)
		if e.osnat != nil {
			e.osnat.Close()
		}
		if e.ether != nil {
			e.ether.close()
		}
	})
	return nil
}

// SetInbound installs core's inbound-IP callback (macip.IPEgress). Called once before
// Start.
func (e *Egress) SetInbound(fn func(packet []byte)) {
	e.mu.Lock()
	e.inbound = fn
	e.mu.Unlock()
}

// SendIP forwards one IPv4 packet from a Mac client toward the IP network
// (macip.IPEgress). In NAT mode, traffic addressed to the gateway IP is answered
// locally (ICMP echo) and off-subnet traffic goes through the OS stack; otherwise the
// packet is injected directly onto the link.
func (e *Egress) SendIP(pkt []byte) error {
	if len(pkt) < 20 {
		return fmt.Errorf("macipgw: short IP packet (%d)", len(pkt))
	}
	dstIP := net.IP(pkt[16:20]).To4()

	// Gateway-addressed traffic (e.g. ICMP ping to the gateway IP) is answered here.
	if e.cfg.NATEnabled && e.gwIP != nil && dstIP.Equal(e.gwIP) {
		e.handleGatewayICMP(pkt)
		return nil
	}
	if e.cfg.NATEnabled && e.osnat != nil {
		e.osnat.Forward(pkt)
		return nil
	}
	return e.ether.sendIPPacket(pkt)
}

// AssignIP relays DHCP for an AppleTalk node and returns the resulting config
// (macip.AddressAssigner). Only present in DHCP-relay mode; in other modes core uses
// its static pool and never calls this. On success the adapter announces the address
// via gratuitous ARP and adopts any DHCP-supplied default gateway.
func (e *Egress) AssignIP(atNet uint16, atNode uint8, requested macip.IPv4) (macip.AssignedConfig, bool) {
	if e.dhcp == nil {
		return macip.AssignedConfig{}, false
	}
	var req net.IP
	if (requested != macip.IPv4{}) {
		req = net.IP(requested[:]).To4()
	}
	res := e.dhcp.RequestIP(context.Background(), atNet, atNode, req)
	if res == nil || res.assignedIP == nil {
		return macip.AssignedConfig{}, false
	}
	if res.router != nil {
		e.ether.setDefaultGateway(res.router)
	}
	e.ether.sendGratuitousARP(res.assignedIP)

	cfg := macip.AssignedConfig{IP: toIPv4(res.assignedIP)}
	if res.nameserver != nil {
		cfg.Nameserver = toIPv4(res.nameserver)
	}
	if res.broadcast != nil {
		cfg.Broadcast = toIPv4(res.broadcast)
	}
	if res.mask != nil {
		cfg.SubnetMask = toIPv4(net.IP(res.mask))
	}
	return cfg, true
}

// AnnounceLease sends a gratuitous ARP for a statically assigned client IP so the
// segment routes return traffic to us. The compose edge may call it after a static
// assignment; harmless in DHCP mode (AssignIP already announces).
func (e *Egress) AnnounceLease(ip macip.IPv4) {
	if e.ether != nil {
		e.ether.sendGratuitousARP(net.IP(ip[:]))
	}
}

// isOurClient adapts the core lease predicate to the etherlink's net.IP form.
func (e *Egress) isOurClient(ip net.IP) bool {
	if e.ownsIP == nil {
		return false
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	return e.ownsIP(toIPv4(ip4))
}

// deliverInbound fragments an inbound IPv4 packet to the DDP MTU and hands each
// fragment to core's inbound callback (which routes it to the owning Mac client).
func (e *Egress) deliverInbound(pkt []byte) {
	e.mu.Lock()
	fn := e.inbound
	e.mu.Unlock()
	if fn == nil {
		return
	}
	for _, frag := range nat.FragmentIPv4(pkt, nat.MaxIPPerDDP) {
		fn(frag)
	}
}

// handleGatewayICMP answers an ICMP echo request addressed to the gateway IP itself
// (NAT mode); all other gateway-addressed traffic is dropped (no local IP stack).
func (e *Egress) handleGatewayICMP(pkt []byte) {
	if len(pkt) < 20 {
		return
	}
	ihl := int(pkt[0]&0xf) * 4
	if len(pkt) < ihl+8 || pkt[9] != 1 { // not ICMP
		return
	}
	if pkt[ihl] != 8 { // not echo request
		return
	}
	clientIP := net.IP(pkt[12:16]).To4()

	reply := append([]byte(nil), pkt...)
	copy(reply[12:16], e.gwIP)   // src = gwIP
	copy(reply[16:20], clientIP) // dst = client
	reply[8] = 64                // TTL
	reply[10], reply[11] = 0, 0
	sum := nat.RawChecksum(reply[:ihl])
	reply[10], reply[11] = byte(sum>>8), byte(sum)
	reply[ihl] = 0 // echo reply
	reply[ihl+2], reply[ihl+3] = 0, 0
	icsum := nat.RawChecksum(reply[ihl:])
	reply[ihl+2], reply[ihl+3] = byte(icsum>>8), byte(icsum)

	e.deliverInbound(reply)
}

// toIPv4 converts a net.IP to the core's [4]byte form (zero on non-IPv4).
func toIPv4(ip net.IP) macip.IPv4 {
	var out macip.IPv4
	if v := ip.To4(); v != nil {
		copy(out[:], v)
	}
	return out
}

// macipBPFFilter is the kernel-side capture filter for the IP-side link: ARP plus
// subnet-destined IP, and DHCP replies (UDP dst 68) when relaying. Mirrors the legacy
// macipBPFFilter.
func macipBPFFilter(ipNet *net.IPNet, dhcpMode bool) string {
	if dhcpMode {
		return "(arp) or (ip) or (udp dst port 68)"
	}
	return fmt.Sprintf("(arp) or (dst net %s)", ipNet.String())
}
