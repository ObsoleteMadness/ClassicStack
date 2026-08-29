package nat

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

const (
	// osNATICMPTimeout is the idle timeout for ICMP echo mappings.
	osNATICMPTimeout = 30 * time.Second
	// osNATUDPTimeout is the idle timeout for UDP forwarding flows.
	osNATUDPTimeout = 30 * time.Second
	// osNATTCPTimeout is the idle timeout for established TCP forwarding flows.
	osNATTCPTimeout = 5 * time.Minute
	// osNATCleanupPeriod is how often stale forwarding state is purged.
	osNATCleanupPeriod = time.Minute
	// osNATTCPDialTimeout bounds outbound TCP connection attempts.
	osNATTCPDialTimeout = 5 * time.Second
	// osNATMaxSegment is the maximum TCP payload that fits in one DDP-carried IP packet.
	osNATMaxSegment = 546 // max TCP payload: 586 (DDP) - 20 (IP) - 20 (TCP)
)

// osFlowKey identifies a UDP or TCP flow by 5-tuple.
type osFlowKey struct {
	proto      uint8   // proto is the IP protocol number for the flow.
	clientIP   [4]byte // clientIP is the Mac client's IPv4 address.
	clientPort uint16  // clientPort is the client's transport-layer source port.
	dstIP      [4]byte // dstIP is the remote server's IPv4 address.
	dstPort    uint16  // dstPort is the remote server's transport-layer port.
}

// icmpClientKey identifies an ICMP echo flow (client IP + original identifier).
type icmpClientKey struct {
	clientIP [4]byte // clientIP is the Mac client's IPv4 address.
	clientID uint16  // clientID is the ICMP identifier chosen by the client.
}

// icmpFwdEntry stores the NAT state for one ICMP echo exchange.
type icmpFwdEntry struct {
	clientIP [4]byte   // clientIP is the originating Mac client's IPv4 address.
	clientID uint16    // clientID is the original ICMP identifier from the client.
	natID    uint16    // natID is the rewritten ICMP identifier used on the host network.
	expiry   time.Time // expiry is when this mapping should be discarded.
}

// udpFwdFlow tracks one UDP socket and the Mac client it belongs to.
type udpFwdFlow struct {
	conn       *net.UDPConn // conn is the host UDP socket connected to the remote server.
	clientIP   [4]byte      // clientIP is the originating Mac client's IPv4 address.
	clientPort uint16       // clientPort is the originating Mac client's UDP port.
	expiry     time.Time    // expiry is when this flow should be discarded.
}

// tcpFwdFlow tracks TCP sequence state between a Mac client and a host TCP socket.
type tcpFwdFlow struct {
	mu         sync.Mutex    // mu protects the mutable TCP sequencing and lifetime state.
	conn       net.Conn      // conn is the host TCP connection, or nil while connecting.
	clientIP   [4]byte       // clientIP is the Mac client's IPv4 address.
	serverIP   [4]byte       // serverIP is the remote server's IPv4 address.
	clientPort uint16        // clientPort is the Mac client's TCP port.
	serverPort uint16        // serverPort is the remote server's TCP port.
	ourSeq     uint32        // ourSeq is the next TCP sequence number sent toward the Mac.
	macSeq     uint32        // macSeq is the next TCP sequence number expected from the Mac.
	macAck     uint32        // macAck is the highest ACK received from the Mac.
	macWindow  uint16        // macWindow is the Mac's advertised receive window.
	mss        uint16        // mss is the maximum segment size used when sending to the Mac.
	expiry     time.Time     // expiry is when this flow should be discarded.
	windowAdv  chan struct{} // windowAdv is signaled when macAck or macWindow advances.
	done       chan struct{} // done is closed when the flow is terminated.
	doneOnce   sync.Once     // doneOnce ensures done is only closed once.
}

// closeConn closes the flow's done channel once and then closes the host connection.
func (f *tcpFwdFlow) closeConn() {
	f.doneOnce.Do(func() { close(f.done) })
	if f.conn != nil {
		_ = f.conn.Close()
	}
}

// OSNAT forwards off-subnet Mac IP traffic through the host OS network stack.
// Each protocol uses real OS sockets so the host's own IP is the NAT source.
// Reply IPv4 packets are emitted to the deliver sink, which the egress hands to
// the MacIP core for routing back to the owning Mac client (resolved by dst IP).
type OSNAT struct {
	deliver func([]byte) // deliver emits a reassembled inbound IPv4 packet (server→Mac).
	log     *slog.Logger // log records dropped/failed flows.

	icmpConn     *icmp.PacketConn                // icmpConn is the raw ICMP socket, or nil when unavailable.
	icmpMu       sync.Mutex                      // icmpMu protects the ICMP forwarding maps.
	icmpByClient map[icmpClientKey]*icmpFwdEntry // icmpByClient maps original client identifiers to ICMP NAT entries.
	icmpByNatID  map[uint16]*icmpFwdEntry        // icmpByNatID maps rewritten ICMP identifiers back to NAT entries.
	icmpNextID   uint16                          // icmpNextID is the next candidate ICMP identifier for NAT allocation.

	udpMu    sync.Mutex                // udpMu protects udpFlows.
	udpFlows map[osFlowKey]*udpFwdFlow // udpFlows tracks active UDP forwarding sockets.

	tcpMu    sync.Mutex                // tcpMu protects tcpFlows.
	tcpFlows map[osFlowKey]*tcpFwdFlow // tcpFlows tracks TCP forwarding state; nil means a dial is in progress.

	stop chan struct{} // stop is closed to shut down background goroutines.
}

// New creates an OSNAT forwarder. deliver receives each reassembled inbound IPv4
// packet (the host's reply to a Mac client); the caller forwards it to the MacIP
// core. log may be nil (a discard logger is used).
func New(deliver func([]byte), log *slog.Logger) *OSNAT {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	n := &OSNAT{
		deliver:      deliver,
		log:          log,
		icmpByClient: make(map[icmpClientKey]*icmpFwdEntry),
		icmpByNatID:  make(map[uint16]*icmpFwdEntry),
		icmpNextID:   1000,
		udpFlows:     make(map[osFlowKey]*udpFwdFlow),
		tcpFlows:     make(map[osFlowKey]*tcpFwdFlow),
		stop:         make(chan struct{}),
	}
	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		n.log.Warn("macip-nat: ICMP forwarding disabled (raw socket unavailable)", "err", err)
	} else {
		n.icmpConn = conn
		go n.icmpReadLoop()
	}
	go n.cleanupLoop()
	return n
}

// Close stops all goroutines and closes open connections.
func (n *OSNAT) Close() {
	close(n.stop)
	if n.icmpConn != nil {
		_ = n.icmpConn.Close()
	}
	n.udpMu.Lock()
	for _, f := range n.udpFlows {
		_ = f.conn.Close()
	}
	n.udpMu.Unlock()
	n.tcpMu.Lock()
	for _, f := range n.tcpFlows {
		if f != nil {
			f.closeConn()
		}
	}
	n.tcpMu.Unlock()
}

// Forward dispatches an off-subnet IPv4 packet from a Mac client onto the host network.
func (n *OSNAT) Forward(pkt []byte) {
	if len(pkt) < 20 {
		return
	}
	ihl := int(pkt[0]&0xf) * 4
	if len(pkt) < ihl {
		return
	}
	switch pkt[9] {
	case 1:
		n.forwardICMP(pkt, ihl)
	case 17:
		n.forwardUDP(pkt, ihl)
	case 6:
		n.handleTCP(pkt, ihl)
	default:
		n.log.Debug("macip-nat: unsupported proto, dropped", "proto", pkt[9])
	}
}

// ── ICMP ──────────────────────────────────────────────────────────────────────

// allocICMPNatID reserves a unique ICMP identifier for host-side echo requests.
func (n *OSNAT) allocICMPNatID() uint16 {
	for {
		id := n.icmpNextID
		n.icmpNextID++
		if n.icmpNextID == 0 {
			n.icmpNextID = 1000
		}
		if _, used := n.icmpByNatID[id]; !used {
			return id
		}
	}
}

// forwardICMP translates an ICMP echo request onto the host network.
func (n *OSNAT) forwardICMP(pkt []byte, ihl int) {
	if n.icmpConn == nil {
		return
	}
	if len(pkt) < ihl+8 || pkt[ihl] != 8 { // echo request only
		return
	}
	clientIP := [4]byte{pkt[12], pkt[13], pkt[14], pkt[15]}
	dstIP := net.IP(pkt[16:20])
	origID := binary.BigEndian.Uint16(pkt[ihl+4 : ihl+6])
	origSeq := int(binary.BigEndian.Uint16(pkt[ihl+6 : ihl+8]))
	data := append([]byte(nil), pkt[ihl+8:]...)

	ck := icmpClientKey{clientIP, origID}
	n.icmpMu.Lock()
	entry := n.icmpByClient[ck]
	if entry == nil {
		natID := n.allocICMPNatID()
		entry = &icmpFwdEntry{clientIP: clientIP, clientID: origID, natID: natID}
		n.icmpByClient[ck] = entry
		n.icmpByNatID[natID] = entry
	}
	entry.expiry = time.Now().Add(osNATICMPTimeout)
	natID := entry.natID
	n.icmpMu.Unlock()

	msg := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{ID: int(natID), Seq: origSeq, Data: data},
	}
	b, err := msg.Marshal(nil)
	if err != nil {
		n.log.Debug("macip-nat: ICMP marshal", "err", err)
		return
	}
	if _, err := n.icmpConn.WriteTo(b, &net.IPAddr{IP: dstIP}); err != nil {
		n.log.Debug("macip-nat: ICMP send", "dst", dstIP.String(), "err", err)
	}
}

// icmpReadLoop receives host ICMP replies and emits them back toward the Mac client.
func (n *OSNAT) icmpReadLoop() {
	buf := make([]byte, 65535)
	for {
		select {
		case <-n.stop:
			return
		default:
		}
		_ = n.icmpConn.SetDeadline(time.Now().Add(100 * time.Millisecond))
		size, peer, err := n.icmpConn.ReadFrom(buf)
		if err != nil {
			continue
		}
		msg, err := icmp.ParseMessage(1, buf[:size])
		if err != nil || msg.Type != ipv4.ICMPTypeEchoReply {
			continue
		}
		echo, ok := msg.Body.(*icmp.Echo)
		if !ok {
			continue
		}
		natID := uint16(echo.ID)
		n.icmpMu.Lock()
		entry, ok := n.icmpByNatID[natID]
		if ok {
			entry.expiry = time.Now().Add(osNATICMPTimeout)
		}
		n.icmpMu.Unlock()
		if !ok {
			continue
		}
		srcIP := peer.(*net.IPAddr).IP.To4()
		replyMsg := &icmp.Message{
			Type: ipv4.ICMPTypeEchoReply,
			Code: 0,
			Body: &icmp.Echo{ID: int(entry.clientID), Seq: echo.Seq, Data: echo.Data},
		}
		reply, err := replyMsg.Marshal(nil)
		if err != nil {
			continue
		}
		n.emit(BuildIPv4Packet(srcIP, entry.clientIP[:], 1, reply))
	}
}

// ── UDP ───────────────────────────────────────────────────────────────────────

// forwardUDP forwards one UDP datagram from a Mac client to the host network.
func (n *OSNAT) forwardUDP(pkt []byte, ihl int) {
	if len(pkt) < ihl+8 {
		return
	}
	clientIP := [4]byte{pkt[12], pkt[13], pkt[14], pkt[15]}
	dstIPb := [4]byte{pkt[16], pkt[17], pkt[18], pkt[19]}
	clientPort := binary.BigEndian.Uint16(pkt[ihl : ihl+2])
	dstPort := binary.BigEndian.Uint16(pkt[ihl+2 : ihl+4])
	udpLen := int(binary.BigEndian.Uint16(pkt[ihl+4 : ihl+6]))
	if udpLen < 8 || len(pkt) < ihl+udpLen {
		return
	}
	payload := pkt[ihl+8 : ihl+udpLen]

	key := osFlowKey{17, clientIP, clientPort, dstIPb, dstPort}
	n.udpMu.Lock()
	flow := n.udpFlows[key]
	if flow == nil {
		conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IP(dstIPb[:]), Port: int(dstPort)})
		if err != nil {
			n.udpMu.Unlock()
			n.log.Debug("macip-nat: UDP dial", "dst", net.IP(dstIPb[:]).String(), "port", dstPort, "err", err)
			return
		}
		flow = &udpFwdFlow{
			conn: conn, clientIP: clientIP, clientPort: clientPort,
			expiry: time.Now().Add(osNATUDPTimeout),
		}
		n.udpFlows[key] = flow
		go n.udpReadLoop(key, flow, dstIPb)
	}
	flow.expiry = time.Now().Add(osNATUDPTimeout)
	n.udpMu.Unlock()

	if _, err := flow.conn.Write(payload); err != nil {
		n.log.Debug("macip-nat: UDP write", "err", err)
	}
}

// udpReadLoop reads reply datagrams from the host UDP socket and returns them to the Mac.
func (n *OSNAT) udpReadLoop(key osFlowKey, flow *udpFwdFlow, serverIP [4]byte) {
	buf := make([]byte, 65535)
	for {
		_ = flow.conn.SetReadDeadline(time.Now().Add(osNATUDPTimeout))
		m, err := flow.conn.Read(buf)
		if m > 0 {
			seg := make([]byte, 8+m)
			binary.BigEndian.PutUint16(seg[0:2], key.dstPort)     // src port
			binary.BigEndian.PutUint16(seg[2:4], flow.clientPort) // dst port
			binary.BigEndian.PutUint16(seg[4:6], uint16(8+m))
			copy(seg[8:], buf[:m])
			n.emit(BuildIPv4Packet(serverIP[:], flow.clientIP[:], 17, seg))
		}
		if err != nil {
			n.udpMu.Lock()
			if n.udpFlows[key] == flow {
				delete(n.udpFlows, key)
			}
			n.udpMu.Unlock()
			return
		}
	}
}

// ── TCP ───────────────────────────────────────────────────────────────────────

// handleTCP processes one TCP segment from a Mac client and updates forwarding state.
func (n *OSNAT) handleTCP(pkt []byte, ihl int) {
	if len(pkt) < ihl+20 {
		return
	}
	clientIP := [4]byte{pkt[12], pkt[13], pkt[14], pkt[15]}
	serverIPb := [4]byte{pkt[16], pkt[17], pkt[18], pkt[19]}
	clientPort := binary.BigEndian.Uint16(pkt[ihl : ihl+2])
	serverPort := binary.BigEndian.Uint16(pkt[ihl+2 : ihl+4])
	seq := binary.BigEndian.Uint32(pkt[ihl+4 : ihl+8])
	tcpHdrLen := int(pkt[ihl+12]>>4) * 4
	if len(pkt) < ihl+tcpHdrLen {
		return
	}
	flags := pkt[ihl+13]
	payload := pkt[ihl+tcpHdrLen:]

	const (
		flagFIN = 0x01
		flagSYN = 0x02
		flagRST = 0x04
		flagACK = 0x10
	)

	key := osFlowKey{6, clientIP, clientPort, serverIPb, serverPort}

	if flags&flagSYN != 0 && flags&flagACK == 0 {
		// New connection.
		n.tcpMu.Lock()
		if _, exists := n.tcpFlows[key]; exists {
			n.tcpMu.Unlock()
			return
		}
		n.tcpFlows[key] = nil // mark as connecting
		n.tcpMu.Unlock()

		// Parse MSS from SYN options.
		mss := uint16(osNATMaxSegment)
		opts := pkt[ihl+20 : ihl+tcpHdrLen]
		for i := 0; i < len(opts); {
			if opts[i] == 0 {
				break
			}
			if opts[i] == 1 {
				i++
				continue
			}
			if i+1 >= len(opts) {
				break
			}
			l := int(opts[i+1])
			if l < 2 || i+l > len(opts) {
				break
			}
			if opts[i] == 2 && l == 4 {
				if m := binary.BigEndian.Uint16(opts[i+2 : i+4]); m < mss {
					mss = m
				}
			}
			i += l
		}
		synWindow := binary.BigEndian.Uint16(pkt[ihl+14 : ihl+16])
		go n.tcpConnect(key, seq, mss, synWindow, serverIPb, serverPort, clientIP, clientPort)
		return
	}

	n.tcpMu.Lock()
	flow, exists := n.tcpFlows[key]
	n.tcpMu.Unlock()
	if !exists || flow == nil {
		return
	}

	if flags&flagRST != 0 {
		flow.closeConn()
		n.tcpMu.Lock()
		delete(n.tcpFlows, key)
		n.tcpMu.Unlock()
		return
	}

	flow.mu.Lock()
	flow.expiry = time.Now().Add(osNATTCPTimeout)
	if len(payload) > 0 {
		flow.macSeq += uint32(len(payload))
	}
	hasFIN := flags&flagFIN != 0
	if hasFIN {
		flow.macSeq++
	}
	ack := flow.macSeq
	ourSeq := flow.ourSeq
	if flags&flagACK != 0 {
		macAck := binary.BigEndian.Uint32(pkt[ihl+8 : ihl+12])
		if int32(macAck-flow.macAck) > 0 {
			flow.macAck = macAck
		}
		flow.macWindow = binary.BigEndian.Uint16(pkt[ihl+14 : ihl+16])
	}
	flow.mu.Unlock()
	if flags&flagACK != 0 {
		select {
		case flow.windowAdv <- struct{}{}:
		default:
		}
	}

	if len(payload) > 0 {
		if _, err := flow.conn.Write(payload); err != nil {
			n.log.Debug("macip-nat: TCP write", "err", err)
		}
	}
	if hasFIN {
		if tc, ok := flow.conn.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
	}
	if len(payload) > 0 || hasFIN {
		n.sendTCPSegment(flow, ourSeq, ack, 0x10, nil) // ACK
	}
}

// tcpConnect dials the remote server and initializes host-side state for a new TCP flow.
func (n *OSNAT) tcpConnect(key osFlowKey, macISN uint32, mss uint16, synWindow uint16, serverIPb [4]byte, serverPort uint16, clientIP [4]byte, clientPort uint16) {
	addr := net.JoinHostPort(net.IP(serverIPb[:]).String(), strconv.Itoa(int(serverPort)))
	conn, err := net.DialTimeout("tcp4", addr, osNATTCPDialTimeout)
	if err != nil {
		n.log.Debug("macip-nat: TCP dial", "addr", addr, "err", err)
		n.tcpMu.Lock()
		delete(n.tcpFlows, key)
		n.tcpMu.Unlock()
		n.sendTCPRST(serverIPb, clientIP, serverPort, clientPort, macISN+1)
		return
	}

	// The host-side TCP Initial Sequence Number is drawn from crypto/rand:
	// a predictable ISN would let an off-path attacker forge segments into a
	// forwarded flow (RFC 6528), so it must not come from a weak PRNG.
	var isnBuf [4]byte
	if _, err := rand.Read(isnBuf[:]); err != nil {
		// crypto/rand should never fail; if it does, abandon the flow rather
		// than fall back to a predictable ISN.
		n.log.Debug("macip-nat: ISN entropy", "err", err)
		n.tcpMu.Lock()
		delete(n.tcpFlows, key)
		n.tcpMu.Unlock()
		n.sendTCPRST(serverIPb, clientIP, serverPort, clientPort, macISN+1)
		return
	}
	ourISN := binary.BigEndian.Uint32(isnBuf[:])
	flow := &tcpFwdFlow{
		conn:     conn,
		clientIP: clientIP, serverIP: serverIPb,
		clientPort: clientPort, serverPort: serverPort,
		ourSeq:    ourISN + 1,
		macSeq:    macISN + 1,
		macAck:    ourISN + 1, // optimistic: assume Mac will ACK our SYN-ACK
		macWindow: synWindow,
		mss:       mss,
		expiry:    time.Now().Add(osNATTCPTimeout),
		windowAdv: make(chan struct{}, 1),
		done:      make(chan struct{}),
	}

	n.tcpMu.Lock()
	n.tcpFlows[key] = flow
	n.tcpMu.Unlock()

	n.sendTCPSYNACK(flow, ourISN)
	n.tcpServerReadLoop(key, flow)
}

// tcpServerReadLoop relays data from the host TCP connection back to the Mac client.
func (n *OSNAT) tcpServerReadLoop(key osFlowKey, flow *tcpFwdFlow) {
	defer func() {
		n.tcpMu.Lock()
		if n.tcpFlows[key] == flow {
			delete(n.tcpFlows, key)
		}
		n.tcpMu.Unlock()
		flow.closeConn()
	}()

	buf := make([]byte, 65535)
	for {
		// Wait until the Mac's receive window has space before reading more from server.
		for {
			flow.mu.Lock()
			space := int(int32(flow.macAck + uint32(flow.macWindow) - flow.ourSeq))
			flow.mu.Unlock()
			if space > 0 {
				break
			}
			select {
			case <-flow.done:
				return
			case <-flow.windowAdv:
			}
		}

		// Cap the read to available window so we don't overshoot and get dropped.
		flow.mu.Lock()
		space := int(int32(flow.macAck + uint32(flow.macWindow) - flow.ourSeq))
		flow.mu.Unlock()
		if space > len(buf) {
			space = len(buf)
		}

		m, err := flow.conn.Read(buf[:space])
		if m > 0 {
			data := buf[:m]
			for len(data) > 0 {
				chunk := data
				if len(chunk) > int(flow.mss) {
					chunk = chunk[:flow.mss]
				}
				data = data[len(chunk):]
				flow.mu.Lock()
				seq := flow.ourSeq
				ack := flow.macSeq
				flow.ourSeq += uint32(len(chunk))
				flow.mu.Unlock()
				n.sendTCPSegment(flow, seq, ack, 0x18, chunk) // PSH+ACK
			}
		}
		if err != nil {
			flow.mu.Lock()
			seq := flow.ourSeq
			ack := flow.macSeq
			flow.ourSeq++
			flow.mu.Unlock()
			if errors.Is(err, io.EOF) {
				n.sendTCPSegment(flow, seq, ack, 0x11, nil) // FIN+ACK — graceful close
			} else {
				n.sendTCPSegment(flow, seq, ack, 0x14, nil) // RST+ACK — abortive close
			}
			return
		}
	}
}

// sendTCPSYNACK sends a synthetic SYN-ACK back to the Mac for a newly connected flow.
func (n *OSNAT) sendTCPSYNACK(flow *tcpFwdFlow, ourISN uint32) {
	hdr := make([]byte, 24) // 20-byte TCP header + 4-byte MSS option
	binary.BigEndian.PutUint16(hdr[0:2], flow.serverPort)
	binary.BigEndian.PutUint16(hdr[2:4], flow.clientPort)
	binary.BigEndian.PutUint32(hdr[4:8], ourISN)
	binary.BigEndian.PutUint32(hdr[8:12], flow.macSeq) // ack = macISN+1
	hdr[12] = 0x60                                     // data offset = 6 (24 bytes / 4)
	hdr[13] = 0x12                                     // SYN+ACK
	binary.BigEndian.PutUint16(hdr[14:16], 8192)
	hdr[20] = 2 // MSS option
	hdr[21] = 4
	binary.BigEndian.PutUint16(hdr[22:24], flow.mss)
	binary.BigEndian.PutUint16(hdr[16:18], 0)
	binary.BigEndian.PutUint16(hdr[16:18], TransportChecksum(flow.serverIP[:], flow.clientIP[:], 6, hdr))
	n.emit(BuildIPv4Packet(flow.serverIP[:], flow.clientIP[:], 6, hdr))
}

// sendTCPSegment builds a TCP segment from host-side state and emits it back to the Mac.
func (n *OSNAT) sendTCPSegment(flow *tcpFwdFlow, seq, ack uint32, flags byte, data []byte) {
	hdr := make([]byte, 20+len(data))
	binary.BigEndian.PutUint16(hdr[0:2], flow.serverPort)
	binary.BigEndian.PutUint16(hdr[2:4], flow.clientPort)
	binary.BigEndian.PutUint32(hdr[4:8], seq)
	binary.BigEndian.PutUint32(hdr[8:12], ack)
	hdr[12] = 0x50 // data offset = 5 (20 bytes / 4)
	hdr[13] = flags
	binary.BigEndian.PutUint16(hdr[14:16], 8192)
	copy(hdr[20:], data)
	binary.BigEndian.PutUint16(hdr[16:18], 0)
	binary.BigEndian.PutUint16(hdr[16:18], TransportChecksum(flow.serverIP[:], flow.clientIP[:], 6, hdr))
	n.emit(BuildIPv4Packet(flow.serverIP[:], flow.clientIP[:], 6, hdr))
}

// sendTCPRST emits a reset segment to tear down a Mac-side TCP flow immediately.
func (n *OSNAT) sendTCPRST(serverIP, clientIP [4]byte, serverPort, clientPort uint16, ack uint32) {
	hdr := make([]byte, 20)
	binary.BigEndian.PutUint16(hdr[0:2], serverPort)
	binary.BigEndian.PutUint16(hdr[2:4], clientPort)
	binary.BigEndian.PutUint32(hdr[8:12], ack)
	hdr[12] = 0x50
	hdr[13] = 0x14 // RST+ACK
	binary.BigEndian.PutUint16(hdr[16:18], 0)
	binary.BigEndian.PutUint16(hdr[16:18], TransportChecksum(serverIP[:], clientIP[:], 6, hdr))
	n.emit(BuildIPv4Packet(serverIP[:], clientIP[:], 6, hdr))
}

// ── Utilities ─────────────────────────────────────────────────────────────────

// emit hands a reassembled inbound IPv4 packet to the deliver sink.
func (n *OSNAT) emit(pkt []byte) {
	if n.deliver != nil {
		n.deliver(pkt)
	}
}

// cleanupLoop periodically expires stale ICMP, UDP, and TCP forwarding state.
func (n *OSNAT) cleanupLoop() {
	t := time.NewTicker(osNATCleanupPeriod)
	defer t.Stop()
	for {
		select {
		case <-n.stop:
			return
		case <-t.C:
			now := time.Now()
			n.icmpMu.Lock()
			for ck, e := range n.icmpByClient {
				if now.After(e.expiry) {
					delete(n.icmpByNatID, e.natID)
					delete(n.icmpByClient, ck)
				}
			}
			n.icmpMu.Unlock()
			n.udpMu.Lock()
			for k, f := range n.udpFlows {
				if now.After(f.expiry) {
					_ = f.conn.Close()
					delete(n.udpFlows, k)
				}
			}
			n.udpMu.Unlock()
			n.tcpMu.Lock()
			for k, f := range n.tcpFlows {
				if f != nil && now.After(f.expiry) {
					f.closeConn()
					delete(n.tcpFlows, k)
				}
			}
			n.tcpMu.Unlock()
		}
	}
}
