//go:build macip

package macip

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/pgodw/omnitalk/netlog"
	"github.com/pgodw/omnitalk/port/rawlink"
)

const (
	etherTypeIPv4 = 0x0800
	etherTypeARP  = 0x0806

	arpHTypeEthernet = 1
	arpOpRequest     = 1
	arpOpReply       = 2

	arpCacheExpiry   = 10 * time.Minute
	arpLookupTimeout = 2 * time.Second
)

// arpCacheEntry stores a cached IPv4→MAC mapping and its expiry time.
type arpCacheEntry struct {
	// mac is the learned hardware address for the IPv4 address key.
	mac net.HardwareAddr
	// expiry is the time after which the cached mapping is considered stale.
	expiry time.Time
}

// etherIPLink bridges IP traffic to/from the host Ethernet network via a
// RawLink backend. It performs proxy ARP for Mac client IPs and delivers
// inbound packets to the pool. Off-subnet outbound traffic is handled by OSNAT.
//
// When the RawLink backend does not implement FilterableLink, all Ethernet
// frames reach the readLoop and are dispatched in software by EtherType.
// This is correct but less efficient than kernel BPF filtering.
type etherIPLink struct {
	// link is the raw Ethernet frame transport (pcap, TUN/TAP, etc.).
	link rawlink.RawLink
	// ourMAC is the Ethernet address used for proxy ARP and outbound frames.
	ourMAC net.HardwareAddr
	// hostIP is the IPv4 address of the physical host interface.
	hostIP net.IP
	// network is the configured IPv4 subnet for MacIP.
	network *net.IPNet
	// defaultGW is the configured default gateway for off-subnet traffic.
	defaultGW net.IP
	// gwMu protects reads/writes to defaultGW.
	gwMu sync.RWMutex

	// pool maps IPs back to AppleTalk addresses.
	pool *ipPool

	// arpMu protects the ARP cache and wait-list structures.
	arpMu sync.Mutex
	// arpCache maps 4-byte IPv4 keys to cached MAC entries.
	arpCache map[[4]byte]arpCacheEntry
	// arpWait contains channels waiting for a MAC resolution for a key.
	arpWait map[[4]byte][]chan net.HardwareAddr

	// inbound delivers raw IPv4 packets destined for tracked pool IPs.
	inbound chan []byte
	// dhcpInbound delivers DHCP UDP payloads when DHCP mode is enabled.
	dhcpInbound chan []byte
	// stop is closed to request goroutine termination.
	stop chan struct{}
}

// newEtherIPLink wraps the provided RawLink into an etherIPLink ready to
// start. The caller is responsible for applying any BPF filter on the link
// before passing it here; if the backend lacks FilterableLink, software
// filtering in readLoop handles correctness.
func newEtherIPLink(link rawlink.RawLink, ourMAC net.HardwareAddr, hostIP net.IP, network *net.IPNet, defaultGW net.IP, pool *ipPool, dhcpMode bool) (*etherIPLink, error) {
	if link == nil {
		return nil, fmt.Errorf("etherIPLink: rawlink must not be nil")
	}

	var dhcpInbound chan []byte
	if dhcpMode {
		dhcpInbound = make(chan []byte, 16)
	}

	return &etherIPLink{
		link:        link,
		ourMAC:      ourMAC,
		hostIP:      hostIP.To4(),
		network:     network,
		defaultGW:   defaultGW.To4(),
		pool:        pool,
		arpCache:    make(map[[4]byte]arpCacheEntry),
		arpWait:     make(map[[4]byte][]chan net.HardwareAddr),
		inbound:     make(chan []byte, 64),
		dhcpInbound: dhcpInbound,
		stop:        make(chan struct{}),
	}, nil
}

// start launches background goroutines for packet capture and optionally
// probes the configured default gateway to prime the ARP cache.
func (l *etherIPLink) start() {
	go l.readLoop()
	go func() {
		gw := l.getDefaultGateway()
		if _, err := l.resolveMAC(gw); err != nil {
			netlog.Warn("macip: could not ARP for default gateway %s: %v", gw, err)
		} else {
			netlog.Info("macip: resolved default gateway %s", gw)
		}
	}()
}

// getDefaultGateway returns a copy of the configured default gateway IP or
// nil if none is set.
func (l *etherIPLink) getDefaultGateway() net.IP {
	l.gwMu.RLock()
	defer l.gwMu.RUnlock()
	if l.defaultGW == nil {
		return nil
	}
	return append(net.IP(nil), l.defaultGW...)
}

// setDefaultGateway updates the default gateway used for off-subnet lookups.
// Non-IPv4 inputs are ignored.
func (l *etherIPLink) setDefaultGateway(gw net.IP) {
	ip := gw.To4()
	if ip == nil {
		return
	}
	l.gwMu.Lock()
	l.defaultGW = append(net.IP(nil), ip...)
	l.gwMu.Unlock()
}

// close stops background processing and closes the rawlink.
func (l *etherIPLink) close() {
	close(l.stop)
	l.link.Close()
}

// sendFrame transmits a raw Ethernet frame via the underlying rawlink.
func (l *etherIPLink) sendFrame(frame []byte) error {
	return l.link.WriteFrame(frame)
}

// readLoop continuously reads raw frames from the rawlink, processes
// ARP/IPv4 packets, learns MACs, and forwards relevant payloads into the
// MacIP subsystem.
func (l *etherIPLink) readLoop() {
	for {
		select {
		case <-l.stop:
			return
		default:
		}

		data, err := l.link.ReadFrame()
		if err != nil {
			select {
			case <-l.stop:
				return
			default:
				continue
			}
		}
		if len(data) < 14 {
			continue
		}

		if bytes.Equal(data[6:12], l.ourMAC) {
			continue
		}

		etherType := uint16(data[12])<<8 | uint16(data[13])
		switch etherType {
		case etherTypeARP:
			l.handleARP(data[14:])
		case etherTypeIPv4:
			if len(data) < 34 {
				continue
			}
			ip := data[14:]
			// Passively learn the IP→MAC mapping from every captured frame.
			// This is the primary mechanism for learning the default gateway's
			// MAC on Windows, where unicast ARP replies addressed to a custom
			// MAC (e.g. DE:AD:BE:EF:CA:FE) may not be reliably delivered by
			// the NDIS driver even in promiscuous mode. DHCP Offer/Ack frames
			// are Ethernet broadcasts and always captured; their source IP is
			// typically the gateway's IP, so we learn its MAC for free.
			if len(ip) >= 16 {
				srcIPv4 := ip[12:16]
				if !bytes.Equal(srcIPv4, []byte{0, 0, 0, 0}) {
					var key [4]byte
					copy(key[:], srcIPv4)
					l.arpLearnFromFrame(key, data[6:12])
				}
			}
			dstIP := net.IP(data[30:34]).To4()
			if atNet, atNode, ok := l.pool.lookupByIP(dstIP); ok {
				netlog.Debug("macip-ip: captured inbound IP %s→%s for AT %d.%d dst-mac=%s len=%d", net.IP(data[26:30]).To4(), dstIP, atNet, atNode, net.HardwareAddr(data[0:6]), len(ip))
				select {
				case l.inbound <- append([]byte(nil), ip...):
				default:
				}
			}
			// DHCP response: UDP dst port 68.
			if l.dhcpInbound != nil && len(ip) >= 28 {
				ihl := int(ip[0]&0xf) * 4
				if ip[9] == 17 && len(ip) >= ihl+8 {
					if binary.BigEndian.Uint16(ip[ihl+2:ihl+4]) == 68 && len(ip) > ihl+8 {
						select {
						case l.dhcpInbound <- append([]byte(nil), ip[ihl+8:]...):
						default:
						}
					}
				}
			}
		}
	}
}

// arpLearnFromFrame caches an IP→MAC mapping observed from an Ethernet frame
// and wakes any goroutines blocked in resolveMAC waiting for that IP.
func (l *etherIPLink) arpLearnFromFrame(key [4]byte, srcMAC []byte) {
	mac := append(net.HardwareAddr(nil), srcMAC...)
	l.arpMu.Lock()
	e, cached := l.arpCache[key]
	if !cached || time.Now().After(e.expiry) {
		l.arpCache[key] = arpCacheEntry{mac: mac, expiry: time.Now().Add(arpCacheExpiry)}
	}
	if waiters := l.arpWait[key]; len(waiters) > 0 {
		for _, ch := range waiters {
			select {
			case ch <- mac:
			default:
			}
		}
		delete(l.arpWait, key)
	}
	l.arpMu.Unlock()
}

// handleARP parses an ARP packet, updates the ARP cache with the sender's
// mapping, notifies waiters, and emits a proxy-ARP reply when the target IP
// belongs to a tracked MacIP client.
func (l *etherIPLink) handleARP(data []byte) {
	if len(data) < 28 {
		return
	}
	if binary.BigEndian.Uint16(data[0:2]) != arpHTypeEthernet ||
		binary.BigEndian.Uint16(data[2:4]) != etherTypeIPv4 {
		return
	}

	op := binary.BigEndian.Uint16(data[6:8])
	senderMAC := net.HardwareAddr(data[8:14])
	senderIP := net.IP(data[14:18]).To4()
	targetIP := net.IP(data[24:28]).To4()

	netlog.Debug("macip-ip: ARP op=%d sender=%s(%s) target=%s", op, senderIP, senderMAC, targetIP)

	var senderKey [4]byte
	copy(senderKey[:], senderIP)
	l.arpMu.Lock()
	l.arpCache[senderKey] = arpCacheEntry{
		mac:    append(net.HardwareAddr(nil), senderMAC...),
		expiry: time.Now().Add(arpCacheExpiry),
	}
	for _, ch := range l.arpWait[senderKey] {
		select {
		case ch <- append(net.HardwareAddr(nil), senderMAC...):
		default:
		}
	}
	delete(l.arpWait, senderKey)
	l.arpMu.Unlock()

	if op != arpOpRequest {
		return
	}

	if _, _, ok := l.pool.lookupByIP(targetIP); ok {
		netlog.Debug("macip-ip: proxy-ARP reply: %s is-at %s (to %s)", targetIP, l.ourMAC, senderIP)
		l.sendARPReply(senderMAC, senderIP, targetIP)
	} else {
		netlog.Debug("macip-ip: ARP request for %s ignored (not a tracked MacIP client)", targetIP)
	}
}

// sendARPReply crafts and transmits an ARP reply indicating that
// ourRepliedIP is at l.ourMAC, sent to dstMAC.
func (l *etherIPLink) sendARPReply(dstMAC net.HardwareAddr, dstIP, ourRepliedIP net.IP) {
	frame := make([]byte, 42)
	copy(frame[0:6], dstMAC)
	copy(frame[6:12], l.ourMAC)
	binary.BigEndian.PutUint16(frame[12:14], etherTypeARP)
	binary.BigEndian.PutUint16(frame[14:16], arpHTypeEthernet)
	binary.BigEndian.PutUint16(frame[16:18], etherTypeIPv4)
	frame[18] = 6
	frame[19] = 4
	binary.BigEndian.PutUint16(frame[20:22], arpOpReply)
	copy(frame[22:28], l.ourMAC)
	copy(frame[28:32], ourRepliedIP.To4())
	copy(frame[32:38], dstMAC)
	copy(frame[38:42], dstIP.To4())
	if err := l.link.WriteFrame(frame); err != nil {
		netlog.Debug("macip: ARP reply error: %v", err)
	}
}

// sendGratuitousARP broadcasts an ARP announcement for ip, pre-populating the
// ARP caches of every host on the segment so that return traffic is directed
// to us without a round-trip ARP exchange.
func (l *etherIPLink) sendGratuitousARP(ip net.IP) {
	ip4 := ip.To4()
	if ip4 == nil {
		return
	}
	// Gratuitous ARP reply: sender = target = announced IP, dst MAC = broadcast.
	frame := make([]byte, 42)
	for i := 0; i < 6; i++ {
		frame[i] = 0xff // Ethernet broadcast
	}
	copy(frame[6:12], l.ourMAC)
	binary.BigEndian.PutUint16(frame[12:14], etherTypeARP)
	binary.BigEndian.PutUint16(frame[14:16], arpHTypeEthernet)
	binary.BigEndian.PutUint16(frame[16:18], etherTypeIPv4)
	frame[18] = 6
	frame[19] = 4
	binary.BigEndian.PutUint16(frame[20:22], arpOpReply)
	copy(frame[22:28], l.ourMAC)
	copy(frame[28:32], ip4) // sender IP = announced IP
	// target MAC = zero (standard for gratuitous ARP)
	copy(frame[38:42], ip4) // target IP = announced IP
	if err := l.link.WriteFrame(frame); err != nil {
		netlog.Debug("macip: gratuitous ARP error for %s: %v", ip4, err)
	} else {
		netlog.Debug("macip: gratuitous ARP sent: %s is-at %s", ip4, l.ourMAC)
	}
}

// sendARPRequest broadcasts an ARP request for targetIP. When the target is
// outside the configured subnet, RFC 5227 probe semantics (sender=0.0.0.0)
// are used to maximize gateway compatibility.
func (l *etherIPLink) sendARPRequest(targetIP net.IP) {
	senderIP := l.hostIP.To4()
	if senderIP == nil || !l.network.Contains(senderIP) || !l.network.Contains(targetIP) {
		senderIP = []byte{0, 0, 0, 0}
	}
	frame := make([]byte, 42)
	for i := 0; i < 6; i++ {
		frame[i] = 0xFF
	}
	copy(frame[6:12], l.ourMAC)
	binary.BigEndian.PutUint16(frame[12:14], etherTypeARP)
	binary.BigEndian.PutUint16(frame[14:16], arpHTypeEthernet)
	binary.BigEndian.PutUint16(frame[16:18], etherTypeIPv4)
	frame[18] = 6
	frame[19] = 4
	binary.BigEndian.PutUint16(frame[20:22], arpOpRequest)
	copy(frame[22:28], l.ourMAC)
	copy(frame[28:32], senderIP)
	copy(frame[38:42], targetIP.To4())
	if err := l.link.WriteFrame(frame); err != nil {
		netlog.Debug("macip: ARP request error: %v", err)
	}
}

// resolveMAC returns the hardware address for the given IPv4 address.
// It consults the local cache, waits for an in-flight resolution, or sends
// an ARP request and blocks until a reply or timeout occurs.
func (l *etherIPLink) resolveMAC(ip net.IP) (net.HardwareAddr, error) {
	ip4 := ip.To4()
	if ip4 == nil {
		return nil, fmt.Errorf("not an IPv4 address: %s", ip)
	}
	if ip4.Equal(l.hostIP) {
		return append(net.HardwareAddr(nil), l.ourMAC...), nil
	}
	var key [4]byte
	copy(key[:], ip4)

	l.arpMu.Lock()
	if e, ok := l.arpCache[key]; ok && time.Now().Before(e.expiry) {
		mac := append(net.HardwareAddr(nil), e.mac...)
		l.arpMu.Unlock()
		return mac, nil
	}
	ch := make(chan net.HardwareAddr, 1)
	l.arpWait[key] = append(l.arpWait[key], ch)
	l.arpMu.Unlock()

	l.sendARPRequest(ip4)

	timer := time.NewTimer(arpLookupTimeout)
	defer timer.Stop()
	select {
	case mac := <-ch:
		return mac, nil
	case <-l.stop:
		l.dropARPWaiter(key, ch)
		return nil, fmt.Errorf("ARP lookup aborted for %s: link closing", ip4)
	case <-timer.C:
		l.dropARPWaiter(key, ch)
		return nil, fmt.Errorf("ARP timeout for %s", ip4)
	}
}

// dropARPWaiter removes ch from the waiter list for key. Called when an
// ARP request gives up (timeout or shutdown) so the next reply that
// arrives doesn't get delivered to a goroutine that has already moved on.
func (l *etherIPLink) dropARPWaiter(key [4]byte, ch chan net.HardwareAddr) {
	l.arpMu.Lock()
	waiters := l.arpWait[key]
	for i, c := range waiters {
		if c == ch {
			l.arpWait[key] = append(waiters[:i], waiters[i+1:]...)
			break
		}
	}
	l.arpMu.Unlock()
}

// sendIPPacket injects a raw IPv4 packet onto the IP-side Ethernet network.
// Used for on-subnet traffic to pool IPs. Off-subnet traffic goes via OSNAT.
func (l *etherIPLink) sendIPPacket(pkt []byte) error {
	if len(pkt) < 20 {
		return fmt.Errorf("IP packet too short (%d bytes)", len(pkt))
	}
	srcIP := net.IP(pkt[12:16]).To4()
	dstIP := net.IP(pkt[16:20]).To4()

	nextHop := l.getDefaultGateway()
	if l.network.Contains(dstIP) {
		nextHop = dstIP
	}

	dstMAC, err := l.resolveMAC(nextHop)
	if err != nil {
		netlog.Debug("macip-ip: IP out %s→%s: no ARP for %s: %v", srcIP, dstIP, nextHop, err)
		return fmt.Errorf("no ARP for %s: %w", nextHop, err)
	}

	netlog.Debug("macip-ip: IP out %s→%s len=%d via %s (%s)", srcIP, dstIP, len(pkt), nextHop, dstMAC)
	frame := make([]byte, 14+len(pkt))
	copy(frame[0:6], dstMAC)
	copy(frame[6:12], l.ourMAC)
	binary.BigEndian.PutUint16(frame[12:14], etherTypeIPv4)
	copy(frame[14:], pkt)
	return l.link.WriteFrame(frame)
}
