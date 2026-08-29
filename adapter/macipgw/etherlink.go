package macipgw

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
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
	mac    net.HardwareAddr
	expiry time.Time
}

// etherIPLink bridges IP traffic to/from the host Ethernet network via a
// core/link.FrameLink backend. It performs proxy ARP for Mac client IPs and
// delivers inbound packets to the egress. Off-subnet outbound traffic is handled
// by the OSNAT engine (NAT mode) or sent directly (bridge mode).
//
// Ported from the legacy service/macip/etherlink.go; the only structural change is
// the link contract (core/link.FrameLink's Read/Write/Close vs the legacy
// rawlink.RawLink's ReadFrame/WriteFrame/Close) and that pool ownership lives in
// the MacIP core, so the link consults two injected callbacks — isOurClient (proxy
// ARP / inbound filter) and onInboundIP / onDHCPReply (delivery).
type etherIPLink struct {
	link   link.FrameLink
	ourMAC net.HardwareAddr
	hostIP net.IP
	// network is the configured IPv4 subnet for MacIP.
	network *net.IPNet
	// defaultGW is the configured default gateway for off-subnet traffic.
	defaultGW net.IP
	gwMu      sync.RWMutex

	// isOurClient reports whether an IPv4 belongs to a tracked MacIP client (for
	// proxy-ARP replies and inbound-packet filtering). Supplied by the egress.
	isOurClient func(ip net.IP) bool
	// onInbound delivers a captured inbound IPv4 packet destined for a tracked client.
	onInbound func(pkt []byte)
	// onDHCP delivers a captured DHCP reply (UDP dst port 68) payload; nil unless
	// DHCP-relay mode is active.
	onDHCP func(pkt []byte)

	log *slog.Logger

	arpMu    sync.Mutex
	arpCache map[[4]byte]arpCacheEntry
	arpWait  map[[4]byte][]chan net.HardwareAddr

	stop chan struct{}
	wg   sync.WaitGroup
}

// newEtherIPLink wraps the provided FrameLink. The caller has already applied any
// BPF filter (and bridge frame-mode) on the link.
func newEtherIPLink(fl link.FrameLink, ourMAC net.HardwareAddr, hostIP net.IP, network *net.IPNet, defaultGW net.IP, isOurClient func(net.IP) bool, log *slog.Logger) (*etherIPLink, error) {
	if fl == nil {
		return nil, fmt.Errorf("macipgw: FrameLink must not be nil")
	}
	if log == nil {
		log = slog.Default()
	}
	return &etherIPLink{
		link:        fl,
		ourMAC:      ourMAC,
		hostIP:      hostIP.To4(),
		network:     network,
		defaultGW:   defaultGW.To4(),
		isOurClient: isOurClient,
		log:         log,
		arpCache:    make(map[[4]byte]arpCacheEntry),
		arpWait:     make(map[[4]byte][]chan net.HardwareAddr),
		stop:        make(chan struct{}),
	}, nil
}

// start launches the capture goroutine and primes the default-gateway ARP entry.
func (l *etherIPLink) start() {
	l.wg.Add(2)
	go func() {
		defer l.wg.Done()
		l.readLoop()
	}()
	go func() {
		defer l.wg.Done()
		gw := l.getDefaultGateway()
		if gw == nil {
			return
		}
		if _, err := l.resolveMAC(gw); err != nil {
			l.log.Warn("macipgw: could not ARP for default gateway", "gw", gw.String(), "err", err)
		} else {
			l.log.Info("macipgw: resolved default gateway", "gw", gw.String())
		}
	}()
}

// getDefaultGateway returns a copy of the configured default gateway IP or nil.
func (l *etherIPLink) getDefaultGateway() net.IP {
	l.gwMu.RLock()
	defer l.gwMu.RUnlock()
	if l.defaultGW == nil {
		return nil
	}
	return append(net.IP(nil), l.defaultGW...)
}

// setDefaultGateway updates the default gateway used for off-subnet lookups.
func (l *etherIPLink) setDefaultGateway(gw net.IP) {
	ip := gw.To4()
	if ip == nil {
		return
	}
	l.gwMu.Lock()
	l.defaultGW = append(net.IP(nil), ip...)
	l.gwMu.Unlock()
}

// close stops background processing and closes the link, joining goroutines.
func (l *etherIPLink) close() {
	close(l.stop)
	_ = l.link.Close()
	l.wg.Wait()
}

// sendFrame transmits a raw Ethernet frame via the underlying link.
func (l *etherIPLink) sendFrame(frame []byte) error {
	return l.link.Write(frame)
}

// readLoop continuously reads frames, processes ARP/IPv4, learns MACs, and
// forwards relevant payloads to the egress.
func (l *etherIPLink) readLoop() {
	for {
		select {
		case <-l.stop:
			return
		default:
		}

		data, err := l.link.Read()
		if err != nil {
			if errors.Is(err, link.ErrClosed) {
				return
			}
			// ErrTimeout and transient read errors: keep looping (unless stopping).
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
			// Passively learn IP→MAC from every captured frame. This is the primary
			// mechanism for learning the gateway's MAC on Windows, where unicast ARP
			// replies addressed to a synthetic MAC may not be delivered reliably.
			if len(ip) >= 16 {
				srcIPv4 := ip[12:16]
				if !bytes.Equal(srcIPv4, []byte{0, 0, 0, 0}) {
					var key [4]byte
					copy(key[:], srcIPv4)
					l.arpLearnFromFrame(key, data[6:12])
				}
			}
			dstIP := net.IP(data[30:34]).To4()
			if l.isOurClient != nil && l.isOurClient(dstIP) && l.onInbound != nil {
				l.onInbound(append([]byte(nil), ip...))
			}
			// DHCP response: UDP dst port 68.
			if l.onDHCP != nil && len(ip) >= 28 {
				ihl := int(ip[0]&0xf) * 4
				if ip[9] == 17 && len(ip) >= ihl+8 {
					if binary.BigEndian.Uint16(ip[ihl+2:ihl+4]) == 68 && len(ip) > ihl+8 {
						l.onDHCP(append([]byte(nil), ip[ihl+8:]...))
					}
				}
			}
		}
	}
}

// arpLearnFromFrame caches an IP→MAC mapping observed from a frame and wakes any
// goroutines blocked in resolveMAC waiting for that IP.
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

// handleARP parses an ARP packet, updates the cache, notifies waiters, and emits a
// proxy-ARP reply when the target IP belongs to a tracked MacIP client.
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
	if l.isOurClient != nil && l.isOurClient(targetIP) {
		l.sendARPReply(senderMAC, senderIP, targetIP)
	}
}

// sendARPReply crafts and transmits an ARP reply indicating that ourRepliedIP is
// at l.ourMAC, sent to dstMAC.
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
	if err := l.link.Write(frame); err != nil {
		l.log.Debug("macipgw: ARP reply error", "err", err)
	}
}

// sendGratuitousARP broadcasts an ARP announcement for ip, pre-populating peers'
// ARP caches so return traffic is directed to us without a round-trip.
func (l *etherIPLink) sendGratuitousARP(ip net.IP) {
	ip4 := ip.To4()
	if ip4 == nil {
		return
	}
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
	if err := l.link.Write(frame); err != nil {
		l.log.Debug("macipgw: gratuitous ARP error", "ip", ip4.String(), "err", err)
	}
}

// sendARPRequest broadcasts an ARP request for targetIP. When the target is
// outside the configured subnet, RFC 5227 probe semantics (sender=0.0.0.0) are
// used for gateway compatibility.
func (l *etherIPLink) sendARPRequest(targetIP net.IP) {
	senderIP := l.hostIP.To4()
	if senderIP == nil || l.network == nil || !l.network.Contains(senderIP) || !l.network.Contains(targetIP) {
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
	if err := l.link.Write(frame); err != nil {
		l.log.Debug("macipgw: ARP request error", "err", err)
	}
}

// resolveMAC returns the hardware address for an IPv4 address, consulting the
// cache, waiting for an in-flight resolution, or sending an ARP request.
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

// dropARPWaiter removes ch from the waiter list for key (timeout/shutdown).
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

// sendIPPacket injects a raw IPv4 packet onto the IP-side network (bridge mode,
// on-subnet traffic to pool IPs). Off-subnet traffic in NAT mode goes via OSNAT.
func (l *etherIPLink) sendIPPacket(pkt []byte) error {
	if len(pkt) < 20 {
		return fmt.Errorf("IP packet too short (%d bytes)", len(pkt))
	}
	srcIP := net.IP(pkt[12:16]).To4()
	dstIP := net.IP(pkt[16:20]).To4()

	nextHop := l.getDefaultGateway()
	if l.network != nil && l.network.Contains(dstIP) {
		nextHop = dstIP
	}
	if nextHop == nil {
		return fmt.Errorf("no next hop for %s", dstIP)
	}

	dstMAC, err := l.resolveMAC(nextHop)
	if err != nil {
		return fmt.Errorf("no ARP for %s: %w", nextHop, err)
	}
	_ = srcIP
	frame := make([]byte, 14+len(pkt))
	copy(frame[0:6], dstMAC)
	copy(frame[6:12], l.ourMAC)
	binary.BigEndian.PutUint16(frame[12:14], etherTypeIPv4)
	copy(frame[14:], pkt)
	return l.link.Write(frame)
}
