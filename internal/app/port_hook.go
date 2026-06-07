package app

import (
	"context"

	"github.com/ObsoleteMadness/ClassicStack/port"
	"github.com/ObsoleteMadness/ClassicStack/router"
)

// portHook adapts a single transport port to the standalone hook lifecycle so
// the management UI can start and stop each port independently, without
// rebuilding the whole stack.
//
// Ports are independent of the router: a port can run while the router is
// stopped (its frames simply go nowhere) and the router can run without a given
// port. A routed port (one bound to the AppleTalk router) therefore couples to
// the router only when both are running:
//
//   - Start brings the port up. When the port is routed and the router is
//     running, it is attached to the router so its frames are routed; otherwise
//     it comes up detached (the router hook adopts running routed ports when it
//     later starts).
//   - Stop detaches a routed port from the running router (withdrawing its
//     routes) before stopping the port itself.
//
// A standalone port (router-attach off) is driven directly with a no-op
// router-hooks sink the whole time, exactly as the supervisor drove it before.
type portHook struct {
	port   port.Port
	router *router.Router
	routed bool
	// routerRunning reports whether the AppleTalk router's services are live,
	// so a routed port knows whether to attach on Start / detach on Stop.
	routerRunning func() bool
	running       bool
}

// newPortHook returns a hook over p. routed marks the port as one that
// participates in the AppleTalk router (vs. a standalone port driven detached).
func newPortHook(p port.Port, r *router.Router, routed bool, routerRunning func() bool) *portHook {
	return &portHook{port: p, router: r, routed: routed, routerRunning: routerRunning}
}

// Start brings the port up. A routed port is attached to the router when the
// router is already running (AddPort starts it against the live router);
// otherwise — and for standalone ports — it starts detached with a no-op
// router-hooks sink so it still receives (capture/metering keep working).
func (h *portHook) Start(ctx context.Context) error {
	if h.running {
		return nil
	}
	if h.routed && h.routerRunning != nil && h.routerRunning() {
		if err := h.router.AddPort(ctx, h.port); err != nil {
			return err
		}
		h.running = true
		return nil
	}
	if err := h.port.Start(noopRouterHooks{}); err != nil {
		return err
	}
	h.running = true
	return nil
}

// Stop tears the port down. A routed port that is part of the running router is
// removed from it first (RemovePort withdraws its routes and stops it);
// otherwise the port is stopped directly.
func (h *portHook) Stop() error {
	if !h.running {
		return nil
	}
	h.running = false
	if h.routed && h.routerRunning != nil && h.routerRunning() && h.router.HasPort(h.port) {
		return h.router.RemovePort(h.port)
	}
	return h.port.Stop()
}

// attachToRouter brings an already-running routed port into the freshly
// started router so it routes again. It is called by the router hook's Start
// for each running routed port; stopped or standalone ports are left alone.
//
// A detached running port was started with a no-op router-hooks sink (the
// router was down), so its inbound frames currently go nowhere. Merely adding
// it to the router's membership would not redirect those frames, so the port is
// restarted against the live router (Stop then AddPort) — the pcap/serial port
// lifecycle is restart-safe. A port already in the router's set is only
// (re)bound to the LLAP link manager.
func (h *portHook) attachToRouter(ctx context.Context) error {
	if !h.running || !h.routed {
		return nil
	}
	if h.router.HasPort(h.port) {
		h.router.AttachStartedPort(h.port) // idempotent; re-binds LLAP
		return nil
	}
	if err := h.port.Stop(); err != nil {
		return err
	}
	return h.router.AddPort(ctx, h.port)
}

// detachFromRouter removes a running routed port from the router that is about
// to stop, leaving the port itself running. Called by the router hook's Stop.
func (h *portHook) detachFromRouter() {
	if h.running && h.routed {
		h.router.DetachPort(h.port)
	}
}
