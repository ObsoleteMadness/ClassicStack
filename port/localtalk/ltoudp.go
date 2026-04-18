package localtalk

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/ipv4"

	"github.com/pgodw/omnitalk/go/netlog"
	"github.com/pgodw/omnitalk/go/port"
)

const (
	ltoudpGroupAddr = "239.192.76.84"
	ltoudpGroupPort = 1954
	ltoudpGroup     = "239.192.76.84:1954"
)

type LtoudpPort struct {
	*Port
	intfAddr  string
	conn      *net.UDPConn
	groupAddr *net.UDPAddr
	stop      chan struct{}
	senderID  [4]byte
	sendPool  sync.Pool
}

func NewLtoudpPort(intfAddr string, seedNetwork uint16, seedZoneName []byte) *LtoudpPort {
	base := New(seedNetwork, seedZoneName, true, 0xFE)
	p := &LtoudpPort{Port: base, intfAddr: intfAddr, stop: make(chan struct{})}
	p.ConfigureSendFrame(p.sendFrame)
	binary.BigEndian.PutUint32(p.senderID[:], uint32(os.Getpid()))
	return p
}

func (p *LtoudpPort) ShortString() string {
	if p.intfAddr == "" || p.intfAddr == "0.0.0.0" {
		return "LToUDP"
	}
	return p.intfAddr
}

func (p *LtoudpPort) Start(router port.RouterHooks) error {
	listenHost := "0.0.0.0"
	if p.intfAddr != "" {
		listenHost = p.intfAddr
	}
	listenAddr := net.JoinHostPort(listenHost, fmt.Sprintf("%d", ltoudpGroupPort))
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) { _ = setSockOptReuseAddr(fd) })
		},
	}
	pc2, err := lc.ListenPacket(context.Background(), "udp4", listenAddr)
	if err != nil {
		return err
	}
	c := pc2.(*net.UDPConn)

	pc := ipv4.NewPacketConn(c)

	groupIP := net.ParseIP(ltoudpGroupAddr)
	if err := p.joinMulticastGroup(pc, groupIP); err != nil {
		c.Close()
		return err
	}

	if err := pc.SetMulticastTTL(1); err != nil {
		netlog.Debug("%s SetMulticastTTL: %v", p.ShortString(), err)
	}

	// Ensure multicast loopback is on so the socket receives its own sent packets.
	if err := pc.SetMulticastLoopback(true); err != nil {
		netlog.Debug("%s SetMulticastLoopback: %v", p.ShortString(), err)
	}

	// Bump socket buffers: default Windows SO_RCVBUF is ~8 KB which loses
	// packets during bursty ATP multi-fragment responses on loopback.
	if err := c.SetReadBuffer(1 << 20); err != nil {
		netlog.Debug("%s SetReadBuffer: %v", p.ShortString(), err)
	}
	if err := c.SetWriteBuffer(1 << 20); err != nil {
		netlog.Debug("%s SetWriteBuffer: %v", p.ShortString(), err)
	}

	// Resolve the multicast group address once — ResolveUDPAddr is non-trivial
	// and was previously called on every frame.
	ga, err := net.ResolveUDPAddr("udp", ltoudpGroup)
	if err != nil {
		c.Close()
		return err
	}
	p.groupAddr = ga
	p.sendPool.New = func() interface{} {
		b := make([]byte, 65536)
		return &b
	}

	p.conn = c
	if err := p.Port.Start(router); err != nil {
		c.Close()
		return err
	}
	go p.run()
	return nil
}

func (p *LtoudpPort) Stop() error {
	close(p.stop)
	if p.conn != nil {
		_ = p.conn.Close()
	}
	return p.Port.Stop()
}

func (p *LtoudpPort) run() {
	buf := make([]byte, 65535)
	for {
		n, _, err := p.conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-p.stop:
				return
			default:
			}
			// Real read error (not a shutdown) — brief back-off, then continue.
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if n < 7 {
			netlog.Debug("%s UDP recv: %d bytes (too short, ignoring)", p.ShortString(), n)
			continue
		}
		if string(buf[:4]) == string(p.senderID[:]) {
			netlog.Debug("%s UDP recv: %d bytes (own frame, ignoring)", p.ShortString(), n)
			continue
		}
		netlog.Debug("%s UDP recv: %d bytes", p.ShortString(), n)
		p.InboundFrame(append([]byte(nil), buf[4:n]...))
	}
}

func (p *LtoudpPort) sendFrame(frame []byte) error {
	// Pull a scratch buffer from the pool so concurrent senders don't race.
	need := 4 + len(frame)
	bufPtr := p.sendPool.Get().(*[]byte)
	buf := *bufPtr
	if cap(buf) < need {
		buf = make([]byte, need)
	} else {
		buf = buf[:need]
	}
	copy(buf[:4], p.senderID[:])
	copy(buf[4:], frame)
	netlog.Debug("%s UDP send: %d bytes", p.ShortString(), need)
	_, err := p.conn.WriteToUDP(buf, p.groupAddr)
	if err != nil {
		netlog.Warn("%s sendFrame write error: %v", p.ShortString(), err)
	}
	*bufPtr = buf
	p.sendPool.Put(bufPtr)
	return err
}

func (p *LtoudpPort) joinMulticastGroup(pc *ipv4.PacketConn, groupIP net.IP) error {
	group := &net.UDPAddr{IP: groupIP}

	if p.intfAddr != "" && p.intfAddr != "0.0.0.0" {
		intf, err := interfaceByIPv4(p.intfAddr)
		if err != nil {
			return err
		}
		if err := pc.JoinGroup(intf, group); err != nil {
			return err
		}
		if err := pc.SetMulticastInterface(intf); err != nil {
			netlog.Debug("%s SetMulticastInterface(%s): %v", p.ShortString(), intf.Name, err)
		}
		return nil
	}

	if err := pc.JoinGroup(nil, group); err == nil {
		return nil
	} else {
		defaultErr := err
		if err := p.tryJoinGroupOnAnyInterface(pc, group); err == nil {
			return nil
		}
		return defaultErr
	}
}

func (p *LtoudpPort) tryJoinGroupOnAnyInterface(pc *ipv4.PacketConn, group *net.UDPAddr) error {
	ifaces, err := net.Interfaces()
	if err != nil {
		return err
	}

	operStatusByIndex, err := multicastInterfaceOperStatus()
	if err != nil {
		netlog.Debug("%s interface status probe failed: %v", p.ShortString(), err)
		operStatusByIndex = nil
	}

	joined := 0
	var lastErr error
	var sendIntf *net.Interface

	joinOnClass := func(includeLoopback bool) {
		for i := range ifaces {
			intf := &ifaces[i]
			hasIPv4 := interfaceHasIPv4(intf)
			connected, connectedKnown := operStatusByIndex[uint32(intf.Index)]
			if !shouldTryJoinInterface(intf, includeLoopback, hasIPv4, connectedKnown, connected) {
				if connectedKnown && !connected {
					netlog.Debug("%s skipping disconnected interface %q", p.ShortString(), intf.Name)
				}
				continue
			}
			if err := pc.JoinGroup(intf, group); err != nil {
				lastErr = err
				continue
			}
			netlog.Info("%s joined multicast group on interface %q", p.ShortString(), intf.Name)
			if sendIntf == nil {
				sendIntf = intf
			}
			joined++
		}
	}

	// Prefer real network interfaces first, then loopback as a fallback.
	joinOnClass(false)
	joinOnClass(true)

	if joined > 0 {
		if sendIntf != nil {
			if err := pc.SetMulticastInterface(sendIntf); err != nil {
				netlog.Debug("%s SetMulticastInterface(%s): %v", p.ShortString(), sendIntf.Name, err)
			}
		}
		return nil
	}

	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("no multicast-capable IPv4 interface available")
}

func shouldTryJoinInterface(intf *net.Interface, includeLoopback bool, hasIPv4 bool, connectedKnown bool, connected bool) bool {
	isLoopback := intf.Flags&net.FlagLoopback != 0
	if isLoopback != includeLoopback {
		return false
	}
	if intf.Flags&net.FlagUp == 0 || intf.Flags&net.FlagMulticast == 0 {
		return false
	}
	if !hasIPv4 {
		return false
	}
	if connectedKnown && !connected {
		return false
	}
	return true
}

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
			if !ok || ipNet.IP == nil {
				continue
			}
			if ipNet.IP.To4() != nil && ipNet.IP.Equal(ip) {
				return intf, nil
			}
		}
	}

	return nil, fmt.Errorf("no network interface found for IPv4 address %q", addr)
}

func interfaceHasIPv4(intf *net.Interface) bool {
	addrs, err := intf.Addrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok || ipNet.IP == nil {
			continue
		}
		if ipNet.IP.To4() != nil {
			return true
		}
	}
	return false
}
