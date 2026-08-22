package ltoudp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/ipv4"

	"github.com/ObsoleteMadness/ClassicStack/core/hostinfo"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

// LToUDP multicast group — the shared "LocalTalk over UDP" segment.
const (
	GroupAddr = "239.192.76.84" // IPv4 multicast group for LToUDP
	GroupPort = 1954            // UDP port
	group     = "239.192.76.84:1954"

	// senderIDLen is the 4-byte per-process sender ID prefixing every datagram so
	// a participant can drop its own multicast echo. It is NOT part of LLAP.
	senderIDLen = 4

	// maxDatagram bounds a single UDP read; an LLAP-over-UDP frame is far smaller
	// (a DDP datagram tops out near 600 bytes) but a generous buffer is cheap.
	maxDatagram = 65535

	// defaultReadTimeout bounds Read so the runport read loop can poll for Stop
	// (mapped to link.ErrTimeout, the same contract the pcap adapter honours).
	defaultReadTimeout = 250 * time.Millisecond
)

// Config holds parameters for opening an LToUDP link. A zero Config opens on the
// wildcard interface with the default read timeout.
type Config struct {
	// Interface is the local IPv4 address to bind/join on ("" or "0.0.0.0" → join
	// on every host LAN multicast-capable interface).
	Interface string
	// ReadTimeout bounds a blocking Read before it returns link.ErrTimeout (0 →
	// defaultReadTimeout).
	ReadTimeout time.Duration
	// Logger, when set, records the multicast join and the interface outbound
	// packets are pinned to. Nil is silent (tests).
	Logger log.Logger
}

// DefaultConfig returns a Config for the given interface address with the
// default read timeout.
func DefaultConfig(iface string) Config {
	return Config{Interface: iface, ReadTimeout: defaultReadTimeout}
}

// frameLink implements core/link.FrameLink over an LToUDP multicast socket. It
// strips/prepends the 4-byte sender ID and drops its own echoed frames, so the
// framer above sees only peer LLAP frames.
type frameLink struct {
	conn        *net.UDPConn
	group       *net.UDPAddr
	senderID    [senderIDLen]byte
	readTimeout time.Duration

	// mu guards closed so Close cannot race a Read/Write into a freed socket.
	mu      sync.RWMutex
	closed  bool
	sendBuf sync.Pool
}

// Compile-time assertion: *frameLink satisfies core/link.FrameLink.
var _ link.FrameLink = (*frameLink)(nil)

// Open joins the LToUDP multicast group on cfg.Interface and returns it as a
// core/link.FrameLink. It mirrors the legacy LtoudpPort.Start socket setup
// (SO_REUSEADDR, TTL 1, loopback on, fat socket buffers) reshaped onto the
// FrameLink seam. The caller frames the result with the LLAP framer.
func Open(cfg Config) (link.FrameLink, error) {
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = defaultReadTimeout
	}

	listenHost := "0.0.0.0"
	if cfg.Interface != "" {
		listenHost = cfg.Interface
	}
	listenAddr := net.JoinHostPort(listenHost, fmt.Sprintf("%d", GroupPort))

	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) { _ = setSockOptReuseAddr(fd) })
		},
	}
	pc2, err := lc.ListenPacket(context.Background(), "udp4", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("ltoudp: listen %s: %w", listenAddr, err)
	}
	c := pc2.(*net.UDPConn)

	pc := ipv4.NewPacketConn(c)
	joined, send, err := joinMulticastGroup(pc, cfg.Interface)
	if err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("ltoudp: join group: %w", err)
	}
	logJoin(cfg.Logger, joined, send)

	// TTL 1 keeps the segment link-local; loopback on so we receive our own sends
	// (and rely on the sender ID to drop them). Both are best-effort.
	_ = pc.SetMulticastTTL(1)
	_ = pc.SetMulticastLoopback(true)

	// macOS Local Network privacy (15+) silently drops multicast unless the
	// responsible app is allowed. Connecting UDP to the group raises the system
	// prompt; a CLI started from Terminal is auto-allowed, but a process spawned
	// by another app (IDE, Finder) uses that app's Local Network privilege.
	triggerLocalNetworkPrivacyAlert()

	// Fat socket buffers: a default ~8 KB SO_RCVBUF (Windows) drops packets during
	// bursty multi-fragment ATP responses on loopback.
	_ = c.SetReadBuffer(1 << 20)
	_ = c.SetWriteBuffer(1 << 20)

	ga, err := net.ResolveUDPAddr("udp", group)
	if err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("ltoudp: resolve group: %w", err)
	}

	fl := &frameLink{conn: c, group: ga, readTimeout: cfg.ReadTimeout}
	// A per-process sender ID — the PID, like the legacy port. Two ClassicStack
	// processes on one host get distinct IDs and so don't eat each other's frames.
	putUint32(fl.senderID[:], uint32(os.Getpid()))
	fl.sendBuf.New = func() any { b := make([]byte, maxDatagram); return &b }
	return fl, nil
}

// Read returns the next peer LLAP frame, mapping a read deadline to
// link.ErrTimeout (caller loops) and post-Close use to link.ErrClosed. Frames
// shorter than the sender ID, and this process's own echoed frames, are skipped
// internally — the deadline guarantees Read still returns (as ErrTimeout) even
// if the only traffic on the group is our own echo.
func (l *frameLink) Read() (link.Frame, error) {
	buf := make([]byte, maxDatagram)
	for {
		l.mu.RLock()
		if l.closed {
			l.mu.RUnlock()
			return nil, link.ErrClosed
		}
		conn := l.conn
		l.mu.RUnlock()

		_ = conn.SetReadDeadline(time.Now().Add(l.readTimeout))
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) {
				return nil, link.ErrTimeout
			}
			l.mu.RLock()
			closed := l.closed
			l.mu.RUnlock()
			if closed {
				return nil, link.ErrClosed
			}
			return nil, err
		}
		if n < senderIDLen {
			continue // too short to carry a sender ID + frame
		}
		if string(buf[:senderIDLen]) == string(l.senderID[:]) {
			continue // our own multicast echo
		}
		// Hand the caller its own copy of just the LLAP frame (sans sender ID).
		frame := make(link.Frame, n-senderIDLen)
		copy(frame, buf[senderIDLen:n])
		return frame, nil
	}
}

// Write sends frame as one LToUDP datagram (sender ID + frame) to the group. It
// does not retain frame past the call.
func (l *frameLink) Write(frame link.Frame) error {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.closed {
		return link.ErrClosed
	}

	need := senderIDLen + len(frame)
	bufPtr := l.sendBuf.Get().(*[]byte)
	buf := *bufPtr
	if cap(buf) < need {
		buf = make([]byte, need)
	} else {
		buf = buf[:need]
	}
	copy(buf[:senderIDLen], l.senderID[:])
	copy(buf[senderIDLen:], frame)
	_, err := l.conn.WriteToUDP(buf, l.group)
	*bufPtr = buf
	l.sendBuf.Put(bufPtr)
	return err
}

// Close shuts the socket; subsequent Read/Write return link.ErrClosed.
// Idempotent.
func (l *frameLink) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	return l.conn.Close()
}

// putUint32 writes v big-endian into b[:4] without pulling encoding/binary into
// the hot path (and keeps the adapter's surface minimal).
func putUint32(b []byte, v uint32) {
	b[0] = byte(v >> 24)
	b[1] = byte(v >> 16)
	b[2] = byte(v >> 8)
	b[3] = byte(v)
}

// joinMulticastGroup joins the LToUDP group on the configured interface, or on
// every host LAN multicast-capable IPv4 interface when iface is empty/wildcard.
// Outbound multicast is pinned to a real LAN NIC (the default-route interface
// when it is in the join set) so TTL-1 packets leave Wi-Fi/Ethernet instead of
// a VPN/AirDrop iface the kernel might otherwise pick.
func joinMulticastGroup(pc *ipv4.PacketConn, iface string) (joined []string, send string, err error) {
	groupIP := net.ParseIP(GroupAddr)
	g := &net.UDPAddr{IP: groupIP}

	if iface != "" && iface != "0.0.0.0" {
		intf, err := interfaceByIPv4(iface)
		if err != nil {
			return nil, "", err
		}
		if err := pc.JoinGroup(intf, g); err != nil {
			return nil, "", err
		}
		_ = pc.SetMulticastInterface(intf)
		return []string{intf.Name}, intf.Name, nil
	}

	return joinOnLANInterfaces(pc, g)
}

// joinOnLANInterfaces joins the group on every up, multicast-capable IPv4 host
// LAN interface (skipping VPN/AirDrop/tunnels), then loopback so two processes
// on one machine still share the segment. Outbound packets are pinned to the
// default-route LAN NIC when possible.
func joinOnLANInterfaces(pc *ipv4.PacketConn, g *net.UDPAddr) (joined []string, send string, err error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, "", err
	}
	lan, loopback := classifyMulticastInterfaces(ifaces)
	var lastErr error
	var sendIntf *net.Interface

	join := func(list []*net.Interface) {
		for _, intf := range list {
			if err := pc.JoinGroup(intf, g); err != nil {
				lastErr = err
				continue
			}
			joined = append(joined, intf.Name)
			if sendIntf == nil {
				sendIntf = intf
			}
		}
	}
	join(lan)
	if len(joined) == 0 {
		join(loopback)
	} else {
		// Still join loopback so same-host peers arrive, but do not pin send to it.
		for _, intf := range loopback {
			if err := pc.JoinGroup(intf, g); err != nil {
				lastErr = err
				continue
			}
			joined = append(joined, intf.Name)
		}
	}

	if len(joined) == 0 {
		if lastErr != nil {
			return nil, "", lastErr
		}
		return nil, "", errors.New("no multicast-capable IPv4 interface available")
	}

	if prefer, perr := hostinfo.PrimaryInterface(); perr == nil {
		if picked := pickSendInterface(lan, &prefer); picked != nil {
			sendIntf = picked
		}
	}
	if sendIntf != nil {
		_ = pc.SetMulticastInterface(sendIntf)
		send = sendIntf.Name
	}
	return joined, send, nil
}

func logJoin(logger log.Logger, joined []string, send string) {
	if logger == nil {
		return
	}
	logger.Log(log.Info, "ltoudp multicast joined",
		log.Str("group", group),
		log.Str("ifaces", strings.Join(joined, ",")),
		log.Str("send", send))
	if send != "" {
		logger.Log(log.Debug, "ltoudp outbound multicast pinned to LAN interface",
			log.Str("iface", send),
			log.Str("note", "macOS Local Network (Privacy & Security) and the Application Firewall can silently drop UDP multicast; a CLI from Terminal is auto-allowed, a process spawned by another app uses that app's permission"))
	}
}

// interfaceByIPv4 finds the interface owning the given IPv4 address.
func interfaceByIPv4(addr string) (*net.Interface, error) {
	ip := net.ParseIP(addr).To4()
	if ip == nil {
		return nil, fmt.Errorf("invalid IPv4 interface address %q", addr)
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	for i := range ifaces {
		intf := &ifaces[i]
		addrs, err := intf.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if ok && ipNet.IP != nil && ipNet.IP.To4() != nil && ipNet.IP.Equal(ip) {
				return intf, nil
			}
		}
	}
	return nil, fmt.Errorf("no interface for IPv4 address %q", addr)
}

// interfaceHasIPv4 reports whether intf has at least one IPv4 address.
func interfaceHasIPv4(intf *net.Interface) bool {
	addrs, err := intf.Addrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if ipNet, ok := a.(*net.IPNet); ok && ipNet.IP != nil && ipNet.IP.To4() != nil {
			return true
		}
	}
	return false
}
