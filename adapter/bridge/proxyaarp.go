package bridge

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/adapter/link/framing"
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

// Name is the ProxyAARP component/registry key.
const Name = "ProxyAARP"

// LinkOpener opens one side's raw FrameLink. Injected per-Start (a fresh handle each
// Start) so the bridge survives a Stop→Start, mirroring the port opener seam. A nil
// opener means "no backend in this build" — the bridge then comes up inert.
type LinkOpener func() (link.FrameLink, error)

// ProxyAARP is the proxy-AARP Wi-Fi/tunnel bridge component (see doc.go). It forwards
// frames between the tunnel and egress FrameLinks, applying the atalk-proxy AARP-Reply
// rewrite on the tunnel→egress direction.
type ProxyAARP struct {
	name      string
	openTun   LinkOpener // opens the tunnel/local side
	openEgr   LinkOpener // opens the egress (e.g. Wi-Fi) side
	egressMAC [6]byte    // the egress interface's own MAC (the rewrite target)
	logger    log.Logger

	mu      sync.Mutex
	started bool
	tun     link.FrameLink
	egr     link.FrameLink
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// New builds a ProxyAARP bridge. openTun/openEgr open the two sides on Start; egressMAC
// is the egress interface's hardware address (the MAC AARP Replies are rewritten to). A
// nil opener yields the inert form (Start is a no-op that still satisfies the lifecycle),
// the same graceful degradation the inert-but-routed ports use when no backend exists.
func New(name string, openTun, openEgr LinkOpener, egressMAC [6]byte, logger log.Logger) *ProxyAARP {
	if name == "" {
		name = Name
	}
	return &ProxyAARP{
		name:      name,
		openTun:   openTun,
		openEgr:   openEgr,
		egressMAC: egressMAC,
		logger:    logger,
	}
}

var _ component.Component = (*ProxyAARP)(nil)
var _ component.Bindable = (*ProxyAARP)(nil)
var _ component.Describable = (*ProxyAARP)(nil)

// Name reports the component identity.
func (p *ProxyAARP) Name() string { return p.name }

// Binding is a short human label for the dashboard: the two bridged sides.
func (p *ProxyAARP) Binding() string { return "proxy-aarp (tunnel↔egress)" }

// Kind labels the component for the dashboard.
func (p *ProxyAARP) Kind() string { return "bridge" }

// Props surfaces the egress MAC for dashboard detail.
func (p *ProxyAARP) Props() map[string]string {
	m := p.egressMAC
	return map[string]string{
		"egress_mac": fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", m[0], m[1], m[2], m[3], m[4], m[5]),
	}
}

// Start opens both sides and launches the two forwarding goroutines. It is idempotent
// (a second Start on a running bridge returns nil) and, when either opener is nil,
// comes up inert. On a partial open failure it closes whatever opened so a failed Start
// leaks no handle.
func (p *ProxyAARP) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started {
		return nil
	}
	if p.openTun == nil || p.openEgr == nil {
		// Inert: no device backend in this build. Satisfy the lifecycle without moving
		// frames, like the inert-but-routed ports.
		p.started = true
		return nil
	}

	tun, err := p.openTun()
	if err != nil {
		return err
	}
	egr, err := p.openEgr()
	if err != nil {
		_ = tun.Close()
		return err
	}

	stopCh := make(chan struct{})
	p.tun = tun
	p.egr = egr
	p.stopCh = stopCh
	p.started = true

	p.wg.Add(2)
	// tunnel → egress: apply the proxy-AARP rewrite (Replies get the egress MAC).
	go p.forward(tun, egr, true, stopCh)
	// egress → tunnel: verbatim pass-through (no rewrite this way).
	go p.forward(egr, tun, false, stopCh)
	return nil
}

// Stop closes both sides and waits for the forwarding goroutines to drain. Safe after a
// failed/partial Start and idempotent.
func (p *ProxyAARP) Stop(ctx context.Context) error {
	p.mu.Lock()
	if !p.started {
		p.mu.Unlock()
		return nil
	}
	p.started = false
	stopCh := p.stopCh
	tun, egr := p.tun, p.egr
	p.tun, p.egr, p.stopCh = nil, nil, nil
	p.mu.Unlock()

	if stopCh != nil {
		close(stopCh)
	}
	var err error
	if tun != nil {
		err = errors.Join(err, tun.Close())
	}
	if egr != nil {
		err = errors.Join(err, egr.Close())
	}
	p.wg.Wait()
	return err
}

// forward reads frames from src and writes them to dst until the link closes or Stop
// fires. When rewrite is true it applies the atalk-proxy transform to each frame
// (framing.ProxyRewriteFrame): an AARP Reply is re-sourced from the egress MAC, everything
// else passes through byte-for-byte. A per-read ErrTimeout is transient (keep looping);
// ErrClosed / any other terminal error ends the goroutine.
func (p *ProxyAARP) forward(src, dst link.FrameLink, rewrite bool, stopCh chan struct{}) {
	defer p.wg.Done()
	for {
		select {
		case <-stopCh:
			return
		default:
		}

		frame, err := src.Read()
		if err != nil {
			if errors.Is(err, link.ErrTimeout) {
				continue
			}
			return // ErrClosed or terminal — exit; Stop/next Start re-establishes it
		}
		if len(frame) == 0 {
			continue
		}

		out := frame
		if rewrite {
			if rewritten, changed := framing.ProxyRewriteFrame(frame, p.egressMAC); changed {
				out = rewritten
			}
		}
		if werr := dst.Write(out); werr != nil {
			if errors.Is(werr, link.ErrClosed) {
				return
			}
			// A transient write error (timeout / dropped frame) is logged and skipped;
			// the bridge keeps forwarding rather than tearing down on one bad frame.
			if p.logger != nil {
				p.logger.Log1(log.Debug, "proxyaarp: frame write dropped", log.Str("err", werr.Error()))
			}
		}
	}
}
