//go:build macip

// Package macip implements a MacIP gateway service (equivalent of macipgw).
// It bridges IP traffic between an Ethernet rawlink and AppleTalk nodes using
// the MacIP protocol:
//   - ATP (DDP type 3) on socket 72 for IP address assignment
//   - DDP type 22 on socket 72 for IP-in-DDP data transport
//
// The gateway performs proxy ARP on the IP-side interface so that the IP
// network routes Mac client addresses to it. The IP-side rawlink is injected
// at construction time, allowing pcap, TUN/TAP, or other backends.
package macip

import (
	"context"
	"encoding/binary"
	"net"
	"time"

	"github.com/pgodw/omnitalk/protocol/ddp"

	"github.com/pgodw/omnitalk/netlog"
	"github.com/pgodw/omnitalk/port"
	"github.com/pgodw/omnitalk/port/nat"
	"github.com/pgodw/omnitalk/port/rawlink"
	"github.com/pgodw/omnitalk/service"
	"github.com/pgodw/omnitalk/service/zip"
)

const (
	// Socket is the AppleTalk socket used by MacIP (both ATP config and data).
	Socket = 72

	// DDP types used by MacIP.
	ddpTypeATP   = 3
	ddpTypeMacIP = 22

	// MacIP config function codes.
	macIPFuncAssign = 1 // Mac requests an IP address
	macIPFuncServer = 3 // Mac checks the server is still alive

	// MacIP protocol version as sent in TResp (matches macipgw: htonl(1) truncated to short).
	macIPVersion = 1

	// ATP control byte values.
	atpFuncTReq  = 0x40
	atpFuncTResp = 0x80
	atpEOM       = 0x10

	// macIPCtrlLen is the minimum MacIP user-data size: version(2)+pad(2)+function(4).
	// MacTCP sends only this when it has no preferred IP address.
	macIPCtrlLen = 8

	// configDataLen is the full MacIP config payload size used in responses.
	configDataLen = 28

	expiryInterval = 30 * time.Second
)

// Service is a OmniRouter service that provides MacIP gateway functionality.
type Service struct {
	// Immutable configuration (set at construction).
	gwIP         net.IP
	subnetMask   net.IPMask
	nameserverIP net.IP
	broadcastIP  net.IP
	zoneName     []byte // may be empty; resolved in Start()
	nbp          *zip.NameInformationService

	// IP-side link parameters (set at construction).
	ipLink      rawlink.RawLink
	ipOurMAC    net.HardwareAddr
	ipHostIP    net.IP
	ipDefaultGW net.IP

	natEnabled bool
	dhcpMode   bool
	stateFile  string

	pool   *ipPool
	osnat  *nat.OSNAT
	dhcp   *dhcpClient
	link   *etherIPLink
	router service.Router // set in Start(), read-only afterwards

	ch   chan inboundPkt
	stop chan struct{}

	// ctx is cancelled when Stop() is called and is the parent of any
	// per-request contexts handed to background work (DHCP, etc.).
	ctx       context.Context
	ctxCancel context.CancelFunc
}

type inboundPkt struct {
	d ddp.Datagram
	p port.Port
}

// New returns a MacIP gateway service.
//
//   - gwIP: gateway IP advertised to MacIP clients
//   - network: subnet network address (e.g. 192.168.100.0)
//   - mask: subnet mask
//   - nameserver: nameserver IP advertised to clients (may equal gwIP)
//   - broadcast: subnet broadcast address
//   - zone: AppleTalk zone name for NBP registration (empty → resolved at start)
//   - nbp: the router's NameInformationService
//   - ipLink: pre-configured rawlink for the IP-side network (caller opens and BPF-filters it)
//   - ipOurMAC: our Ethernet MAC on the IP-side interface
//   - ipHostIP: host interface IPv4 used for ARP probes and local identity
//   - ipDefaultGW: default gateway IP on the IP-side network
//   - natEnabled: enable NAPT so Mac clients share gwIP on the physical network
func New(gwIP, network net.IP, mask net.IPMask, nameserver, broadcast net.IP,
	zone []byte, nbp *zip.NameInformationService,
	ipLink rawlink.RawLink, ipOurMAC net.HardwareAddr, ipHostIP, ipDefaultGW net.IP,
	natEnabled bool, dhcpMode bool, stateFile string) *Service {
	s := &Service{
		gwIP:         gwIP.To4(),
		subnetMask:   mask,
		nameserverIP: nameserver.To4(),
		broadcastIP:  broadcast.To4(),
		zoneName:     append([]byte(nil), zone...),
		nbp:          nbp,
		ipLink:       ipLink,
		ipOurMAC:     ipOurMAC,
		ipHostIP:     ipHostIP.To4(),
		ipDefaultGW:  ipDefaultGW.To4(),
		natEnabled:   natEnabled,
		dhcpMode:     dhcpMode,
		stateFile:    stateFile,
		pool:         newIPPool(network, mask),
		ch:           make(chan inboundPkt, 256),
		stop:         make(chan struct{}),
	}
	return s
}

// Socket returns the AppleTalk socket number for this service.
func (s *Service) Socket() uint8 { return Socket }

// Start opens the pcap IP link, registers the NBP name and starts goroutines.
func (s *Service) Start(ctx context.Context, r service.Router) error {
	s.router = r
	s.ctx, s.ctxCancel = context.WithCancel(ctx)

	// Resolve zone name if not supplied.
	if len(s.zoneName) == 0 {
		zones := r.Zones()
		if len(zones) > 0 {
			s.zoneName = append([]byte(nil), zones[0]...)
		}
	}

	// Create OS-stack NAT if enabled.
	if s.natEnabled {
		s.osnat = nat.NewOSNAT(r, Socket, ddpTypeMacIP)
		netlog.Info("macip: OS-stack NAT enabled (traffic proxied through host network stack)")
	} else {
		netlog.Warn("macip: MacIP gateway is intended to be used with NAT (-macip-nat). Non-NAT modes require additional routing and are not recommended.")
	}

	// Wrap the injected rawlink.
	ipNet := &net.IPNet{IP: s.gwIP.Mask(s.subnetMask), Mask: s.subnetMask}
	link, err := newEtherIPLink(s.ipLink, s.ipOurMAC, s.ipHostIP, ipNet, s.ipDefaultGW, s.pool, s.dhcpMode)
	if err != nil {
		return err
	}
	s.link = link
	s.link.start()

	if s.dhcpMode {
		s.dhcp = newDHCPClient(s.link, s.stop)
		go s.dhcp.run(s.stop)
		netlog.Info("macip: DHCP relay enabled — relaying DHCP and converting responses to MacIP configuration for clients")
	}

	s.pool.loadFromFile(s.stateFile)

	// Register as "<gwIP>:IPGATEWAY@<zone>" so Macs can find us via NBP.
	s.nbp.RegisterName([]byte(s.gwIP.String()), []byte("IPGATEWAY"), s.zoneName, Socket)

	go s.inboundLoop()
	go s.ipInboundLoop()
	go s.expiryLoop()

	netlog.Info("macip: gateway started gw=%s host-ip=%s zone=%q", s.gwIP, s.ipHostIP, s.zoneName)
	if !s.natEnabled && !s.dhcpMode {
		// In static-pool bridged mode, return traffic from external hosts reaches
		// Mac clients only if the physical router has a route to the MacIP subnet
		// pointing back to this host (e.g. "ip route add %s via <this host's IP>").
		// Alternatively, use -macip-dhcp so clients receive IPs on the same subnet
		// as the physical network, where proxy ARP handles routing automatically.
		netlog.Info("macip: static-pool bridged mode — ensure your router has a route to %s via this host, or use -macip-dhcp", &net.IPNet{IP: s.gwIP.Mask(s.subnetMask), Mask: s.subnetMask})
	}
	return nil
}

// Stop unregisters NBP, closes the IP link and shuts down all goroutines.
func (s *Service) Stop() error {
	s.nbp.UnregisterName([]byte(s.gwIP.String()), []byte("IPGATEWAY"), s.zoneName)
	s.ctxCancel()
	close(s.stop)
	if s.osnat != nil {
		s.osnat.Close()
	}
	if s.link != nil {
		s.link.close()
	}
	s.pool.saveToFile(s.stateFile)
	return nil
}

// PinLeaseToSession keeps a client's lease tracked while an ASP session is active.
func (s *Service) PinLeaseToSession(atNetwork uint16, atNode uint8, sessionID uint8) {
	s.pool.pinSessionLease(atNetwork, atNode, sessionID)
	netlog.Debug("macip: pin lease for AT %d.%d to ASP session %d", atNetwork, atNode, sessionID)
}

// UnpinLeaseFromSession removes ASP-driven lease pinning for a closed session.
func (s *Service) UnpinLeaseFromSession(sessionID uint8) {
	s.pool.unpinSessionLease(sessionID)
	netlog.Debug("macip: unpin lease for ASP session %d", sessionID)
}

// MarkSessionActivity refreshes the pin activity timestamp for stale-pin cleanup.
func (s *Service) MarkSessionActivity(sessionID uint8) {
	s.pool.markSessionActivity(sessionID)
}

// Inbound is called by the router for every DDP datagram addressed to socket 72.
func (s *Service) Inbound(d ddp.Datagram, p port.Port) {
	select {
	case s.ch <- inboundPkt{d: d, p: p}:
	default:
	}
}

// inboundLoop handles DDP datagrams arriving from AppleTalk.
func (s *Service) inboundLoop() {
	for {
		select {
		case <-s.stop:
			return
		case pkt := <-s.ch:
			switch pkt.d.DDPType {
			case ddpTypeATP:
				s.handleATPConfig(pkt.d, pkt.p)
			case ddpTypeMacIP:
				s.handleMacIPData(pkt.d)
			}
		}
	}
}

// handleATPConfig processes an ATP TReq on socket 72: an IP address request.
func (s *Service) handleATPConfig(d ddp.Datagram, rx port.Port) {
	atNet, atNode := normalizeATSource(d, rx)
	if !validATEndpoint(atNet, atNode) {
		netlog.Warn("macip: dropping ATP config request with invalid source AT %d.%d", d.SourceNetwork, d.SourceNode)
		return
	}

	netlog.Debug("macip: ATP pkt from AT %d.%d len=%d ctrl=0x%02x",
		atNet, atNode, len(d.Data), func() byte {
			if len(d.Data) > 0 {
				return d.Data[0]
			}
			return 0
		}())

	// ATP frame: ctrl(1) bitmap(1) tid(2) + at least the MacIP control struct.
	if len(d.Data) < 4+macIPCtrlLen {
		netlog.Debug("macip: dropping short ATP pkt from AT %d.%d (len=%d, need %d)",
			atNet, atNode, len(d.Data), 4+macIPCtrlLen)
		return
	}
	if d.Data[0]&0xC0 != atpFuncTReq {
		netlog.Debug("macip: dropping non-TReq ATP from AT %d.%d ctrl=0x%02x",
			atNet, atNode, d.Data[0])
		return
	}
	tid := binary.BigEndian.Uint16(d.Data[2:4])
	// userData starts at the ATP user-bytes field (netatalk atp_rreqdata = user_bytes + data).
	// mipr_version occupies user_bytes[0:2] — not checked, macipgw ignores it.
	// mipr_function is at user_bytes[4:8] (start of ATP data body).
	userData := d.Data[4:]
	function := binary.BigEndian.Uint32(userData[4:8])

	var requestedIP net.IP
	if len(userData) >= 12 {
		requestedIP = net.IP(userData[8:12]).To4()
	}

	netlog.Debug("macip: ATP TReq from AT %d.%d tid=%d func=%d requestedIP=%s",
		atNet, atNode, tid, function, requestedIP)

	if s.dhcpMode {
		// In DHCP mode: for server-check (func=3) reuse the existing lease to
		// avoid a redundant DHCP exchange; for assignment (func=1) always ask.
		if function == macIPFuncServer {
			if ip, ok := s.pool.lookupIPByAT(atNet, atNode); ok {
				netlog.Debug("macip-dhcp: server-check AT %d.%d — reusing lease %s", atNet, atNode, ip)
				s.sendATPConfigResp(d, rx, tid, ip, s.nameserverIP, s.broadcastIP, s.subnetMask)
				return
			}
		}
		go s.handleATPConfigDHCP(s.ctx, d, rx, tid, requestedIP, atNet, atNode)
		return
	}

	assignedIP, err := s.pool.assign(requestedIP, atNet, atNode)
	if err != nil {
		netlog.Warn("macip: pool assignment failed for AT %d.%d: %v", atNet, atNode, err)
		assignedIP = net.IPv4zero.To4()
	}

	netlog.Info("macip: assign %s → AT %d.%d (func=%d)", assignedIP, atNet, atNode, function)
	if !assignedIP.Equal(net.IPv4zero) {
		s.link.sendGratuitousARP(assignedIP)
	}
	s.sendATPConfigResp(d, rx, tid, assignedIP, s.nameserverIP, s.broadcastIP, s.subnetMask)
}

// sendATPConfigResp builds and sends an ATP TResp with the given IP configuration.
func (s *Service) sendATPConfigResp(d ddp.Datagram, rx port.Port, tid uint16, assignedIP, nameserver, broadcast net.IP, mask net.IPMask) {
	resp := make([]byte, 4+configDataLen)
	resp[0] = atpFuncTResp | atpEOM
	resp[1] = 0 // seq 0
	resp[2] = byte(tid >> 8)
	resp[3] = byte(tid)
	binary.BigEndian.PutUint16(resp[4:6], macIPVersion)
	// resp[6:8] = 0 (pad)
	binary.BigEndian.PutUint32(resp[8:12], macIPFuncAssign)
	copy(resp[12:16], assignedIP.To4())
	copy(resp[16:20], nameserver.To4())
	copy(resp[20:24], broadcast.To4())
	// resp[24:28] = 0 (pad2)
	copy(resp[28:32], net.IP(mask).To4())

	netlog.Debug("macip: ATP TResp to AT %d.%d tid=%d ip=%s ns=%s bcast=%s mask=%s",
		d.SourceNetwork, d.SourceNode, tid,
		assignedIP, nameserver, broadcast, net.IP(mask).String())

	s.router.Reply(d, rx, ddpTypeATP, resp)
}

// handleATPConfigDHCP runs in its own goroutine: performs a full DHCP exchange
// and sends the ATP TResp once an address is assigned.
func (s *Service) handleATPConfigDHCP(ctx context.Context, d ddp.Datagram, rx port.Port, tid uint16, requestedIP net.IP, atNet uint16, atNode uint8) {
	res := s.dhcp.RequestIP(ctx, atNet, atNode, requestedIP)
	if res == nil {
		netlog.Warn("macip-dhcp: no DHCP response for AT %d.%d — not replying to ATP", atNet, atNode)
		return
	}

	// Fall back to service-level defaults for any fields the DHCP server omitted.
	ns := res.nameserver
	if ns == nil {
		ns = s.nameserverIP
	}
	bc := res.broadcast
	if bc == nil {
		bc = s.broadcastIP
	}
	mask := res.mask
	if mask == nil {
		mask = s.subnetMask
	}
	if res.router != nil {
		s.link.setDefaultGateway(res.router)
		netlog.Info("macip-dhcp: using DHCP router %s as IP-side gateway for AT %d.%d", res.router, atNet, atNode)
	}

	netlog.Info("macip-dhcp: assign %s → AT %d.%d (lease=%ds)", res.assignedIP, atNet, atNode, res.leaseTime)
	s.pool.registerDHCP(res.assignedIP, atNet, atNode)
	s.link.sendGratuitousARP(res.assignedIP)
	s.sendATPConfigResp(d, rx, tid, res.assignedIP, ns, bc, mask)
}

// handleMacIPData processes a DDP type 22 packet: a raw IP packet from a Mac.
func (s *Service) handleMacIPData(d ddp.Datagram) {
	if len(d.Data) < 20 {
		netlog.Debug("macip: dropping short MacIP data from AT %d.%d (len=%d)",
			d.SourceNetwork, d.SourceNode, len(d.Data))
		return
	}
	srcIP := net.IP(d.Data[12:16]).To4()
	dstIP := net.IP(d.Data[16:20]).To4()
	netlog.Debug("macip: IP from AT %d.%d %s→%s len=%d",
		d.SourceNetwork, d.SourceNode, srcIP, dstIP, len(d.Data))
	s.pool.updateSeen(d.SourceNetwork, d.SourceNode)

	// Handle packets destined for the gateway itself (e.g. ICMP ping).
	if s.natEnabled && dstIP.Equal(s.gwIP) {
		s.handleGatewayICMP(d.SourceNetwork, d.SourceNode, d.Data)
		return
	}

	// If the destination is another pool client, deliver directly over AppleTalk.
	if atNet, atNode, ok := s.pool.lookupByIP(dstIP); ok {
		netlog.Debug("macip: IP pool→pool %s→%s via AT %d.%d", srcIP, dstIP, atNet, atNode)
		s.routeIPToMac(atNet, atNode, d.Data)
		return
	}

	// Off-subnet: use OS-stack NAT if enabled, otherwise send directly via pcap.
	if s.natEnabled && s.osnat != nil {
		s.osnat.Forward(d.Data, d.SourceNetwork, d.SourceNode)
		return
	}
	if err := s.link.sendIPPacket(d.Data); err != nil {
		netlog.Debug("macip: IP send error: %v", err)
	}
}

// routeIPToMac fragments pkt if needed and routes each fragment to the given
// AppleTalk node via DDP type 22.
func (s *Service) routeIPToMac(atNet uint16, atNode uint8, pkt []byte) {
	if !validATEndpoint(atNet, atNode) {
		netlog.Debug("macip: dropping route to invalid AT destination %d.%d", atNet, atNode)
		return
	}

	frags := nat.FragmentIPv4(pkt, nat.MaxIPPerDDP)
	if frags == nil {
		netlog.Debug("macip: IP pkt DF+oversized or malformed, dropped (len=%d)", len(pkt))
		return
	}
	for _, frag := range frags {
		if err := s.router.Route(ddp.Datagram{
			DestinationNetwork: atNet,
			DestinationNode:    atNode,
			DestinationSocket:  Socket,
			SourceSocket:       Socket,
			DDPType:            ddpTypeMacIP,
			Data:               frag,
		}, true); err != nil {
			netlog.Debug("macip: AT route error for AT %d.%d: %v", atNet, atNode, err)
		}
	}
}

func normalizeATSource(d ddp.Datagram, rx port.Port) (uint16, uint8) {
	atNet := d.SourceNetwork
	if atNet == 0 && rx != nil && rx.Network() != 0 {
		atNet = rx.Network()
	}
	return atNet, d.SourceNode
}

// ipInboundLoop reads IP packets captured from the IP-side network and
// forwards them to the appropriate AppleTalk node via DDP type 22.
func (s *Service) ipInboundLoop() {
	for {
		select {
		case <-s.stop:
			return
		case pkt := <-s.link.inbound:
			if len(pkt) < 20 {
				continue
			}
			srcIP := net.IP(pkt[12:16]).To4()
			dstIP := net.IP(pkt[16:20]).To4()
			atNet, atNode, ok := s.pool.lookupByIP(dstIP)
			if !ok {
				netlog.Debug("macip: IP pkt dst=%s not in pool, dropped", dstIP)
				continue
			}
			netlog.Debug("macip: IP to AT %d.%d %s→%s len=%d", atNet, atNode, srcIP, dstIP, len(pkt))
			s.routeIPToMac(atNet, atNode, pkt)
		}
	}
}

// handleGatewayICMP responds to ICMP echo requests addressed to the gateway IP.
// All other traffic to the gateway is silently dropped (no local IP stack).
func (s *Service) handleGatewayICMP(srcNet uint16, srcNode uint8, pkt []byte) {
	if len(pkt) < 20 {
		return
	}
	ihl := int(pkt[0]&0xf) * 4
	if len(pkt) < ihl+8 || pkt[9] != 1 { // not ICMP or too short
		return
	}
	if pkt[ihl] != 8 { // not echo request
		netlog.Debug("macip: ICMP type %d to gateway, ignored", pkt[ihl])
		return
	}

	clientIP := net.IP(pkt[12:16]).To4()
	atNet, atNode, ok := s.pool.lookupByIP(clientIP)
	if !ok {
		// Sender not in pool — use the source AT node directly.
		atNet, atNode = srcNet, srcNode
	}

	// Copy packet and build echo reply: swap IPs, set type=0, recalc checksums.
	reply := append([]byte(nil), pkt...)
	copy(reply[12:16], s.gwIP)   // src = gwIP
	copy(reply[16:20], clientIP) // dst = client IP
	reply[8] = 64                // TTL
	binary.BigEndian.PutUint16(reply[10:12], 0)
	binary.BigEndian.PutUint16(reply[10:12], nat.RawChecksum(reply[:ihl]))
	reply[ihl] = 0 // ICMP echo reply
	binary.BigEndian.PutUint16(reply[ihl+2:ihl+4], 0)
	binary.BigEndian.PutUint16(reply[ihl+2:ihl+4], nat.RawChecksum(reply[ihl:]))

	netlog.Debug("macip: ICMP echo reply %s→%s via AT %d.%d", s.gwIP, clientIP, atNet, atNode)
	_ = s.router.Route(ddp.Datagram{
		DestinationNetwork: atNet,
		DestinationNode:    atNode,
		DestinationSocket:  Socket,
		SourceSocket:       Socket,
		DDPType:            ddpTypeMacIP,
		Data:               reply,
	}, true)
}

// expiryLoop periodically evicts stale leases from the IP pool and saves state.
func (s *Service) expiryLoop() {
	t := time.NewTicker(expiryInterval)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			s.pool.expireLeases()
			s.pool.saveToFile(s.stateFile)
		}
	}
}
