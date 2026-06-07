package app

import (
	"context"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/router"
)

// routerHook adapts the AppleTalk router's service set (RTMP, ZIP, NBP, AEP,
// LLAP, …) to the standalone hook lifecycle, so the management UI can stop and
// start the routing engine on its own. It deliberately does NOT own port
// lifecycle: ports are independent hooks (see portHook). Instead, on Start it
// adopts every running routed port into the freshly started router, and on Stop
// it detaches them (leaving them running, their frames simply unrouted).
type routerHook struct {
	router *router.Router
	// routedPorts returns the port hooks for the router-attached ports, so the
	// router hook can adopt/detach them as it starts/stops. Evaluated lazily so
	// it always sees the current set.
	routedPorts func() []*portHook
	running     bool
}

func newRouterHook(r *router.Router, routedPorts func() []*portHook) *routerHook {
	return &routerHook{router: r, routedPorts: routedPorts}
}

// Start brings the routing services up and adopts any already-running routed
// ports so their frames route immediately.
func (h *routerHook) Start(ctx context.Context) error {
	if h.running {
		return nil
	}
	if err := h.router.StartServices(ctx); err != nil {
		return err
	}
	for _, p := range h.routedPorts() {
		if err := p.attachToRouter(ctx); err != nil {
			netlog.Warn("[SUP][Router] attaching port: %v", err)
		}
	}
	h.running = true
	return nil
}

// Stop detaches the running routed ports (leaving them up) and stops the
// routing services.
func (h *routerHook) Stop() error {
	if !h.running {
		return nil
	}
	for _, p := range h.routedPorts() {
		p.detachFromRouter()
	}
	h.running = false
	return h.router.StopServices()
}

// IsRunning reports whether the routing services are live. Port hooks consult
// it to decide whether to attach on Start / detach on Stop.
func (h *routerHook) IsRunning() bool { return h.running }
