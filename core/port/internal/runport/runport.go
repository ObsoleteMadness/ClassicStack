// Package runport is the real (Phase 2 / M3) AppleTalk port base: a datagram
// read loop over a link.DatagramLink with throughput metering, frame counters,
// and a Stop→Start / Reconfigure lifecycle. Embed it in a per-transport package
// (ethertalk, localtalk, …) and supply a Framer that turns the transport's
// FrameLink into DDP datagrams.
//
// Ring: CORE (stdlib only, reflection-free). The link (via a LinkFactory) and
// the inbound router target are injected; runport owns no real I/O of its own.
package runport

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

// LinkFactory opens a fresh DatagramLink for the port. It is called once per
// Start so a port can survive Stop→Start: the previous link is Closed on Stop
// and a brand-new one is opened on the next Start (a closed link cannot be
// reopened — see the pcap restart lifecycle). Returning a nil link with a nil
// error means "no link available" (degraded/inert), which Start treats as a
// successful no-data-path start rather than an error.
type LinkFactory func() (link.DatagramLink, error)

// Port is the shared real-port machinery. It satisfies component.Component plus
// Enableable/Bindable/Statful/Metered/Configurable and the router's data half
// (Unicast/Broadcast). Construct it with New and embed it.
type Port struct {
	mu     sync.Mutex
	sec    *port.Section
	open   LinkFactory
	router router.Router // inbound target; the port delivers DDP via router.Inbound (§3)
	logger log.Logger
	owner  router.RoutedPort // the embedding port, passed to router.Inbound as the rx port

	// Network addressing (claimed in M3 via the port's claim logic; the router
	// reads these to build directly-connected routes in M4). Zero until claimed.
	network        uint16
	node           uint8
	netMin, netMax uint16

	running bool
	dl      link.DatagramLink
	stopCh  chan struct{}
	loopWG  sync.WaitGroup

	observe atomic.Pointer[func(rxBytes, txBytes int)]

	framesRx     atomic.Uint64
	framesTx     atomic.Uint64
	decodeErrors atomic.Uint64
	bytesRx      atomic.Uint64
	bytesTx      atomic.Uint64
}

// New builds a real port base. sec is the typed config section; open opens the
// datagram link on Start (may return nil,nil for inert); rtr is the router the
// port delivers inbound datagrams to via router.Inbound (nil → datagrams are
// dropped until the router is wired, which is the registry path before M4).
//
// The rx-port identity handed to router.Inbound defaults to nil; an embedding
// port MUST call SetOwner(self) after construction so the router sees the outer
// component (which alone satisfies the full router.RoutedPort).
func New(sec *port.Section, open LinkFactory, rtr router.Router, logger log.Logger) *Port {
	return &Port{sec: sec, open: open, router: rtr, logger: logger}
}

// SetOwner records the rx-port identity passed to router.Inbound. An embedding
// port (e.g. ethertalk.Port) calls this with itself so the router sees the
// outer component, not the runport base (the base alone does not satisfy the
// transport-specific parts of router.RoutedPort).
func (p *Port) SetOwner(owner router.RoutedPort) {
	p.mu.Lock()
	p.owner = owner
	p.mu.Unlock()
}

// Name returns the component name (the section key).
func (p *Port) Name() string { return p.sec.SKey }

// Start opens the link and spawns the read loop. Idempotent (§3): starting a
// started port is a no-op. A nil link from the factory is a successful inert
// start (no data path) so a port with no device still satisfies the lifecycle.
func (p *Port) Start(ctx context.Context) error {
	_ = ctx
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running {
		return nil
	}

	var dl link.DatagramLink
	if p.open != nil {
		var err error
		dl, err = p.open()
		if err != nil {
			return err
		}
	}
	p.dl = dl
	p.running = true
	p.stopCh = make(chan struct{})

	if dl != nil {
		p.loopWG.Add(1)
		go p.readLoop(dl, p.stopCh, p.router, p.owner)
		p.logf("port started (read loop active)")
	} else {
		p.logf("port started (no link; data path inert)")
	}
	return nil
}

// Stop closes the link and joins the read loop. Safe after a failed/partial
// Start (§3) and idempotent.
func (p *Port) Stop(ctx context.Context) error {
	_ = ctx
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return nil
	}
	p.running = false
	close(p.stopCh)
	dl := p.dl
	p.dl = nil
	p.mu.Unlock()

	// Close the link OUTSIDE the lock so a blocked ReadDatagram unblocks and the
	// loop can exit; then wait for it. Closing is what makes ReadDatagram return
	// ErrClosed.
	if dl != nil {
		_ = dl.Close()
	}
	p.loopWG.Wait()
	p.logf("port stopped")
	return nil
}

// readLoop pulls datagrams off dl until the link closes or stopCh fires. A
// per-read ErrTimeout is transient (keep looping); ErrClosed is terminal.
func (p *Port) readLoop(dl link.DatagramLink, stopCh chan struct{}, rtr router.Router, owner router.RoutedPort) {
	defer p.loopWG.Done()
	for {
		select {
		case <-stopCh:
			return
		default:
		}

		dg, err := dl.ReadDatagram()
		if err != nil {
			if err == link.ErrTimeout {
				continue
			}
			// ErrClosed or any other terminal error ends the loop. On Stop the
			// stopCh is already closed; on an unexpected close we simply exit and
			// the supervisor's health check / next Start re-establishes the link.
			return
		}
		p.framesRx.Add(1)
		p.bytesRx.Add(uint64(ddpWireLen(dg)))
		if fn := p.observe.Load(); fn != nil {
			(*fn)(ddpWireLen(dg), 0)
		}
		// Deliver to the router (§3: AppleTalk ports speak DDP to router.Inbound).
		// rtr is nil on the registry path until M4 wires a real router.
		if rtr != nil {
			rtr.Inbound(dg, owner)
		}
	}
}

// Unicast writes a datagram addressed to (network,node). M3 has no per-node MAC
// resolution in the framing seam yet, so the underlying DatagramLink decides the
// destination (broadcast MAC); the network/node are carried in the DDP header.
func (p *Port) Unicast(network uint16, node uint8, d ddp.Datagram) {
	d.DestNetwork = network
	d.DestNode = node
	p.write(d)
}

// Broadcast writes a datagram to the link's broadcast address.
func (p *Port) Broadcast(d ddp.Datagram) {
	d.DestNode = 0xFF // DDP broadcast node
	p.write(d)
}

// Multicast writes a datagram to the multicast group for a zone. The framing
// seam has no zone→multicast-MAC map yet (that lands with ZIP/zone wiring), so
// M3 sends it as a DDP broadcast on the link; the zone name is accepted for
// contract compatibility with router.RoutedPort. TODO(M4): map zoneName to the
// EtherTalk multicast MAC via the zone multicast table.
func (p *Port) Multicast(zoneName []byte, d ddp.Datagram) {
	_ = zoneName
	d.DestNode = 0xFF
	p.write(d)
}

func (p *Port) write(d ddp.Datagram) {
	p.mu.Lock()
	dl := p.dl
	p.mu.Unlock()
	if dl == nil {
		return
	}
	if err := dl.WriteDatagram(d); err != nil {
		return
	}
	p.framesTx.Add(1)
	p.bytesTx.Add(uint64(ddpWireLen(d)))
	if fn := p.observe.Load(); fn != nil {
		(*fn)(0, ddpWireLen(d))
	}
}

// Network/Node/NetworkMin/NetworkMax expose the claimed address to the router
// (RoutedPort, M4). They are zero until the port claims an address.
func (p *Port) Network() uint16 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.network
}

func (p *Port) Node() uint8 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.node
}

func (p *Port) NetworkMin() uint16 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.netMin
}

func (p *Port) NetworkMax() uint16 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.netMax
}

// SetAddress records the claimed network/node and the network range this port
// serves. The transport package calls it once its claim logic completes (M3
// node-claim is per-transport; on EtherTalk it is driven by AARP, deferred —
// see the ethertalk package). Safe to call while running.
func (p *Port) SetAddress(network uint16, node uint8, netMin, netMax uint16) {
	p.mu.Lock()
	p.network = network
	p.node = node
	p.netMin = netMin
	p.netMax = netMax
	p.mu.Unlock()
}

// Enabled reports the configured-enabled flag (≠ running). Capability: Enableable.
func (p *Port) Enabled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sec.IsEnabled
}

// Binding reports the bound interface for the dashboard. Capability: Bindable.
func (p *Port) Binding() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sec.Iface
}

// Stats returns a point-in-time snapshot. Capability: Statful (§5).
func (p *Port) Stats() component.Stats {
	return component.Stats{
		Counters: map[string]uint64{
			"frames_rx":     p.framesRx.Load(),
			"frames_tx":     p.framesTx.Load(),
			"decode_errors": p.decodeErrors.Load(),
			"bytes_rx":      p.bytesRx.Load(),
			"bytes_tx":      p.bytesTx.Load(),
		},
		Gauges: map[string]float64{},
	}
}

// SetTrafficObserver installs the rx/tx byte observer. Capability: Metered (§5).
func (p *Port) SetTrafficObserver(fn func(rxBytes, txBytes int)) {
	if fn == nil {
		p.observe.Store(nil)
		return
	}
	p.observe.Store(&fn)
}

// ApplyConfig hot-applies a new section. Capability: Configurable (§11). An
// enabled-flag change applies live; an interface (binding) change is structural
// and returns ErrNeedsRestart so the supervisor restarts the port (which
// reopens the link via the factory).
func (p *Port) ApplyConfig(section any) error {
	sec, ok := section.(*port.Section)
	if !ok || sec == nil {
		return nil // nil/typeless notify pass: absorb live
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if sec.Iface != p.sec.Iface {
		return component.ErrNeedsRestart
	}
	p.sec = sec
	p.logf("port reconfigured live")
	return nil
}

// CountDecodeError lets a transport package bump the decode-error counter when
// its framer rejects a frame (the read loop only sees post-framer datagrams).
func (p *Port) CountDecodeError() { p.decodeErrors.Add(1) }

func (p *Port) logf(msg string) {
	if p.logger == nil || !p.logger.Enabled(log.Info) {
		return
	}
	p.logger.Log1(log.Info, msg, log.Str("port", p.sec.SKey))
}

// ddpWireLen returns the on-wire byte length of a datagram (long header + data),
// used for throughput metering without re-encoding.
func ddpWireLen(d ddp.Datagram) int {
	const ddpLongHeaderLen = 13
	return ddpLongHeaderLen + len(d.Data)
}

// compile-time capability assertions. The base satisfies the full data half, so
// an embedding port that adds only framing is a complete router.RoutedPort.
var (
	_ component.Component    = (*Port)(nil)
	_ component.Enableable   = (*Port)(nil)
	_ component.Bindable     = (*Port)(nil)
	_ component.Statful      = (*Port)(nil)
	_ component.Metered      = (*Port)(nil)
	_ component.Configurable = (*Port)(nil)
	_ router.RoutedPort      = (*Port)(nil)
)
