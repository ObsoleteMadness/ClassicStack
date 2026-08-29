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
//     ping to the gateway IP itself is answered locally. NAT-only (no DHCP-relay)
//     skips pcap entirely so it works on WiFi.
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
	"github.com/ObsoleteMadness/ClassicStack/core/csnet"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/service/macip"
)

// Config is the IP-side configuration for the egress. The compose edge builds it from
// the MacIP section (macip.Section.EgressParams), filling in any auto-detected fields.
type Config struct {
	Interface      string // pcap device for the IP-side network (required)
	HostMAC        string // IP-side host MAC (colon/dash hex; required except NAT-only)
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

// New builds an IP-side egress. Bridge and DHCP-relay open a libpcap link on
// cfg.Interface (ARP + subnet BPF, plus DHCP replies in relay mode). NAT-only
// (NATEnabled && !DHCPRelay) skips pcap entirely and forwards through OS sockets
// — required on WiFi, where APs drop injected frames that are not sourced from
// the host NIC. The returned Egress is injected into the MacIP core via
// Service.SetEgress; call Start once before the service starts and Close on
// shutdown. ownsIP is the core's lease predicate (Service.OwnsIP) used for proxy
// ARP and inbound filtering.
func New(cfg Config, ownsIP func(macip.IPv4) bool, log *slog.Logger) (*Egress, error) {
	if log == nil {
		log = slog.Default()
	}
	if cfg.Interface == "" {
		return nil, fmt.Errorf("macipgw: interface is required")
	}
	if cfg.NATEnabled && cfg.DHCPRelay {
		log.Warn("macipgw: dhcp_relay is not supported in nat mode (clients would get real-LAN addresses instead of the NAT pool); disabling dhcp_relay", "iface", cfg.Interface)
		cfg.DHCPRelay = false
	}
	gwIP := parseOptionalIPv4(cfg.GatewayIP)
	netIP := parseOptionalIPv4(cfg.Network)
	mask := parseOptionalIPv4(cfg.SubnetMask)
	var ipNet *net.IPNet
	if netIP != nil && mask != nil {
		ipNet = &net.IPNet{IP: netIP.Mask(net.IPMask(mask)), Mask: net.IPMask(mask)}
	}
	hostIP := parseOptionalIPv4(cfg.HostIP)
	defGW := parseOptionalIPv4(cfg.DefaultGateway)

	e := &Egress{
		cfg:     cfg,
		log:     log,
		gwIP:    gwIP,
		network: ipNet,
		ownsIP:  ownsIP,
		stop:    make(chan struct{}),
	}

	// NAT-only uses the host OS stack; no Ethernet inject, so no pcap handle.
	natOnly := cfg.NATEnabled && !cfg.DHCPRelay
	if !natOnly {
		mac, err := parseEthernet(cfg.HostMAC)
		if err != nil {
			return nil, fmt.Errorf("macipgw: host MAC: %w", err)
		}
		fl, err := pcap.Open(pcap.DefaultMacIPConfig(cfg.Interface))
		if err != nil {
			return nil, fmt.Errorf("macipgw: open %s: %w", cfg.Interface, err)
		}
		if ff, ok := fl.(link.FilterableLink); ok && ipNet != nil {
			var macArr [6]byte
			copy(macArr[:], mac)
			if err := ff.SetFilter(macipBPFFilter(ipNet, cfg.DHCPRelay, macArr)); err != nil {
				log.Warn("macipgw: BPF filter rejected; capturing unfiltered", "err", err)
			}
		}
		ether, err := newEtherIPLink(fl, mac, hostIP, ipNet, defGW, e.isOurClient, log)
		if err != nil {
			_ = fl.Close()
			return nil, err
		}
		e.ether = ether
		ether.onInbound = e.deliverInbound
		if cfg.DHCPRelay {
			e.dhcp = newDHCPClient(ether, log, e.stop)
			ether.onDHCP = e.dhcp.handleReply
		}
	}

	if cfg.NATEnabled {
		e.osnat = nat.New(e.deliverInbound, log)
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
	if e.ether != nil {
		e.ether.start()
	}
	mode := "bridge"
	if e.cfg.NATEnabled {
		mode = "nat"
	}
	e.log.Info("macipgw: IP egress started", "iface", e.cfg.Interface, "mode", mode, "dhcp_relay", e.cfg.DHCPRelay)
	if !e.cfg.NATEnabled {
		e.log.Warn("macipgw: bridge mode (proxy-ARP / raw IP inject); on WiFi use mode=nat — APs drop non-host source MACs")
	}
	if e.cfg.DHCPRelay {
		e.log.Warn("macipgw: DHCP-relay fabricates per-Mac MACs (02:00:00:…); WiFi APs drop those frames — set dhcp_relay=false")
	}
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

// GatewayIP reports the IP-side gateway identity the core should advertise to MacTCP
// clients: the configured GatewayIP when set, otherwise the resolved IP-side default
// (upstream) gateway. In bridge mode the Mac's lease is on the real LAN subnet, so its
// gateway must be a real on-subnet IP — mirroring the legacy resolveMacIPGatewayIP,
// which used the upstream gateway in non-NAT mode. Returns the zero IPv4 when neither is
// known (the core then keeps whatever it had). The core adopts this at Start so the
// IPGATEWAY NBP name and MacTCP's gateway are never 0.0.0.0.
func (e *Egress) GatewayIP() macip.IPv4 {
	if gw := e.gwIP.To4(); gw != nil && !gw.Equal(net.IPv4zero) {
		return toIPv4(gw)
	}
	if gw := net.ParseIP(e.cfg.DefaultGateway).To4(); gw != nil && !gw.Equal(net.IPv4zero) {
		return toIPv4(gw)
	}
	return macip.IPv4{}
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
	if e.ether == nil {
		return fmt.Errorf("macipgw: no IP link")
	}
	return e.ether.sendIPPacket(pkt)
}

// AssignerActive reports whether this egress is currently sourcing client addresses
// from the IP network (macip.AddressAssigner) — true only in DHCP-relay mode. In NAT
// and bridge modes e.dhcp is nil, so the core must NOT delegate assignment here (AssignIP
// would always fail); it uses its static pool instead. Structurally *Egress always
// carries AssignIP, so this method is how core distinguishes "can actually assign" from
// "merely has the method".
func (e *Egress) AssignerActive() bool { return e.dhcp != nil }

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
	if res.router != nil {
		// Propagate the DHCP-supplied router so the core advertises a gateway that is
		// on the client's own (real LAN) subnet; otherwise MacTCP is handed the static
		// GatewayIP, sees it off-subnet from its lease, and refuses to route off-net.
		cfg.Router = toIPv4(res.router)
	}
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

// parseOptionalIPv4 parses a dotted-quad config field via csnet.ParseIPv4 — the same
// parser core/service/macip's own config validation now uses, so a malformed address
// can no longer be silently accepted by one side and rejected by the other. An empty
// or invalid field is nil, not an error: every field this backs (GatewayIP, Network,
// SubnetMask, HostIP, DefaultGateway) is optional at this layer and validated (if the
// operator set it) up in macip.Section.Validate before New is ever called.
func parseOptionalIPv4(s string) net.IP {
	ip, err := csnet.ParseIPv4(s)
	if err != nil {
		return nil
	}
	return net.IP(ip[:])
}

// macipBPFFilter is the kernel-side capture filter for the IP-side link: ARP plus
// subnet-destined IP, and DHCP replies (UDP dst 68) when relaying. Mirrors the legacy
// macipBPFFilter. mac (the gateway's own IP-side Ethernet address, cfg.HostMAC) is folded
// in via pcap.ExcludeSelf so the kernel drops this gateway's own transmitted frames
// instead of relying solely on etherIPLink's software self-check (readLoop's ourMAC
// comparison, kept as a fallback for when the kernel filter is rejected).
func macipBPFFilter(ipNet *net.IPNet, dhcpMode bool, mac [6]byte) string {
	var base string
	if dhcpMode {
		base = "(arp) or (ip) or (udp dst port 68)"
	} else {
		base = fmt.Sprintf("(arp) or (dst net %s)", ipNet.String())
	}
	return pcap.ExcludeSelf(base, mac)
}
