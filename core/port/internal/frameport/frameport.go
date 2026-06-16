// Package frameport is the real (Phase 2 / M3) frame-level port base for the
// non-AppleTalk transports (IPX, NetBEUI). Unlike runport — which speaks DDP
// datagrams to the AppleTalk router — these transports ride their own
// mini-routers and exchange raw link frames (§3: "IPX/NetBEUI transports speak
// frames to their own mini-routers"). frameport owns the read loop, inbound
// frame dedup, throughput metering, frame counters, and the Stop→Start /
// Reconfigure lifecycle; the embedding transport decodes each delivered frame
// and dispatches it.
//
// Ring: CORE (stdlib only, reflection-free). The FrameLink is opened via an
// injected factory on each Start so the port is restartable (a closed link
// cannot be reopened — see the pcap restart lifecycle).
package frameport

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
)

// LinkFactory opens a fresh FrameLink for the port, called once per Start so a
// stopped port can be started again. A nil link with a nil error means "no link
// available" (inert), which Start accepts as a successful no-data-path start.
type LinkFactory func() (link.FrameLink, error)

// FrameSink receives each surviving inbound frame (post-dedup). It runs on the
// read goroutine, so it MUST NOT block for long: decode and hand off, then
// return. The frame is owned by the sink for the duration of the call only.
type FrameSink func(f link.Frame)

// inboundDedupWindow / inboundDedupTTL match the legacy IPX/NetBEUI ports: a
// frame seen again within the window is a reflected duplicate (e.g. our own
// multicast echoed back) and is dropped.
const (
	inboundDedupWindow = 25 * time.Millisecond
	inboundDedupTTL    = 100 * time.Millisecond
)

// Port is the shared frame-level port machinery. It satisfies
// component.Component plus Enableable/Bindable/Statful/Metered/Configurable.
// Embed it and supply a FrameSink; call Send to transmit.
type Port struct {
	mu     sync.Mutex
	sec    *port.Section
	open   LinkFactory
	sink   FrameSink
	logger log.Logger

	running bool
	fl      link.FrameLink
	stopCh  chan struct{}
	loopWG  sync.WaitGroup

	// dedup of inbound frames, keyed by FNV-1a over the frame bytes.
	dedupMu sync.Mutex
	recent  map[uint64]time.Time

	observe atomic.Pointer[func(rxBytes, txBytes int)]

	framesRx     atomic.Uint64
	framesTx     atomic.Uint64
	framesDup    atomic.Uint64
	decodeErrors atomic.Uint64
	bytesRx      atomic.Uint64
	bytesTx      atomic.Uint64
}

// New builds a frame-level port base. sec is the typed config section; open
// opens the FrameLink on Start (may return nil,nil for inert); sink receives
// inbound frames (nil → frames are counted and dropped).
func New(sec *port.Section, open LinkFactory, sink FrameSink, logger log.Logger) *Port {
	return &Port{sec: sec, open: open, sink: sink, logger: logger, recent: make(map[uint64]time.Time)}
}

// Name returns the component identity: the section's instance name (§M11), which
// for a singleton/default port falls back to the schema key.
func (p *Port) Name() string { return p.sec.InstanceName() }

// Start opens the link and spawns the read loop. Idempotent (§3). A nil link is
// a successful inert start.
func (p *Port) Start(ctx context.Context) error {
	_ = ctx
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running {
		return nil
	}

	var fl link.FrameLink
	if p.open != nil {
		var err error
		fl, err = p.open()
		if err != nil {
			return err
		}
	}
	p.fl = fl
	p.running = true
	p.stopCh = make(chan struct{})

	if fl != nil {
		p.loopWG.Add(1)
		go p.readLoop(fl, p.stopCh)
		p.logf("port started (frame read loop active)")
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
	fl := p.fl
	p.fl = nil
	p.mu.Unlock()

	if fl != nil {
		_ = fl.Close()
	}
	p.loopWG.Wait()
	p.logf("port stopped")
	return nil
}

// readLoop reads frames until the link closes or stopCh fires, dropping
// reflected duplicates and handing survivors to the sink.
func (p *Port) readLoop(fl link.FrameLink, stopCh chan struct{}) {
	defer p.loopWG.Done()
	for {
		select {
		case <-stopCh:
			return
		default:
		}

		frame, err := fl.Read()
		if err != nil {
			if err == link.ErrTimeout {
				continue
			}
			return // ErrClosed or terminal error
		}
		if len(frame) == 0 {
			continue
		}
		p.framesRx.Add(1)
		p.bytesRx.Add(uint64(len(frame)))
		if fn := p.observe.Load(); fn != nil {
			(*fn)(len(frame), 0)
		}
		if p.isDuplicate(frame) {
			p.framesDup.Add(1)
			continue
		}
		if p.sink != nil {
			p.sink(frame)
		}
	}
}

// Send transmits a raw frame on the link. It meters and counts the frame. A
// stopped/inert port silently drops (the transport decides whether that is an
// error for its caller).
func (p *Port) Send(frame link.Frame) error {
	p.mu.Lock()
	fl := p.fl
	p.mu.Unlock()
	if fl == nil {
		return link.ErrClosed
	}
	// Record our own outbound frame so its multicast echo is deduped on receive.
	p.remember(frameHash(frame))
	if err := fl.Write(frame); err != nil {
		return err
	}
	p.framesTx.Add(1)
	p.bytesTx.Add(uint64(len(frame)))
	if fn := p.observe.Load(); fn != nil {
		(*fn)(0, len(frame))
	}
	return nil
}

// isDuplicate reports whether frame was seen within the dedup window, recording
// it for future checks and expiring stale entries.
func (p *Port) isDuplicate(frame link.Frame) bool {
	h := frameHash(frame)
	now := time.Now()
	p.dedupMu.Lock()
	defer p.dedupMu.Unlock()
	last, seen := p.recent[h]
	if seen && now.Sub(last) <= inboundDedupWindow {
		return true
	}
	p.recent[h] = now
	// Opportunistically expire stale entries to bound the map.
	for k, t := range p.recent {
		if now.Sub(t) > inboundDedupTTL {
			delete(p.recent, k)
		}
	}
	return false
}

func (p *Port) remember(h uint64) {
	now := time.Now()
	p.dedupMu.Lock()
	p.recent[h] = now
	p.dedupMu.Unlock()
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
			"frames_dup":    p.framesDup.Load(),
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
// enabled-flag change applies live; an interface change is structural and
// returns ErrNeedsRestart so the supervisor restarts (reopening the link).
func (p *Port) ApplyConfig(section any) error {
	sec, ok := section.(*port.Section)
	if !ok || sec == nil {
		return nil
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

// CountDecodeError bumps the decode-error counter when the embedding transport's
// frame decode rejects a delivered frame.
func (p *Port) CountDecodeError() { p.decodeErrors.Add(1) }

func (p *Port) logf(msg string) {
	if p.logger == nil || !p.logger.Enabled(log.Info) {
		return
	}
	p.logger.Log1(log.Info, msg, log.Str("port", p.sec.SKey))
}

// frameHash is FNV-1a over the frame bytes (no hash/fnv import to keep this
// allocation-free and identical to core/link's frame dedup keying).
func frameHash(frame link.Frame) uint64 {
	const (
		offset64 = 1469598103934665603
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	for _, b := range frame {
		h ^= uint64(b)
		h *= prime64
	}
	return h
}

// compile-time capability assertions.
var (
	_ component.Component    = (*Port)(nil)
	_ component.Enableable   = (*Port)(nil)
	_ component.Bindable     = (*Port)(nil)
	_ component.Statful      = (*Port)(nil)
	_ component.Metered      = (*Port)(nil)
	_ component.Configurable = (*Port)(nil)
)
