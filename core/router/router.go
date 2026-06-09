package router

import (
	"context"
	"errors"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
)

// RoutedPort is the data half a routed port exposes to the router (the lifecycle half is
// component.Component). A port is RoutedPort + Component. The router never knows whether the
// port's datagrams came from a kernel socket or a Framing(FrameLink) (§2).
type RoutedPort interface {
	component.Component
	Unicast(network uint16, node uint8, d ddp.Datagram)
	Broadcast(d ddp.Datagram)
	Multicast(zoneName []byte, d ddp.Datagram)
	Network() uint16
	Node() uint8
	NetworkMin() uint16
	NetworkMax() uint16
}

// Router is a Component. Attach/Detach are membership events: Detach withdraws the port's
// directly-connected routes IMMEDIATELY (no aging delay, §3). Inbound is the port→router hook.
type Router interface {
	component.Component
	Attach(p RoutedPort) error
	Detach(p RoutedPort) error
	Inbound(d ddp.Datagram, from RoutedPort)
}

// Name is the component name for the AppleTalk router.
const Name = "Router"

// RouterImpl is a placeholder Router implementation for Phase 1.
type RouterImpl struct {
	mu       sync.Mutex
	running  bool
	ports    map[string]RoutedPort
	logger   log.Logger
}

// New builds the Phase 1 Router placeholder.
func New(logger log.Logger) *RouterImpl {
	return &RouterImpl{
		ports:  make(map[string]RoutedPort),
		logger: logger,
	}
}

// Name returns the component name.
func (r *RouterImpl) Name() string { return Name }

// Start brings the placeholder router up. Idempotent (§3).
func (r *RouterImpl) Start(ctx context.Context) error {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return nil
	}
	r.running = true
	r.logf("router started (routing logic not implemented)")
	return nil
}

// Stop brings the placeholder router down. Safe after failed/partial Start (§3).
func (r *RouterImpl) Stop(ctx context.Context) error {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.running {
		return nil
	}
	r.running = false
	r.logf("router stopped")
	return nil
}

// Attach attaches a RoutedPort to the router. Establish the membership logic (§3).
func (r *RouterImpl) Attach(p RoutedPort) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.running {
		return errors.New("router: cannot attach port to stopped router")
	}
	name := p.Name()
	if _, ok := r.ports[name]; ok {
		return errors.New("router: port already attached")
	}
	r.ports[name] = p
	r.logf("port " + name + " attached to router")
	return nil
}

// Detach detaches a RoutedPort from the router. Withdraws connected routes immediately (§3).
func (r *RouterImpl) Detach(p RoutedPort) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := p.Name()
	if _, ok := r.ports[name]; !ok {
		return errors.New("router: port not attached")
	}
	delete(r.ports, name)
	r.logf("port " + name + " detached from router")
	return nil
}

// Inbound is a no-op placeholder for inbound packet routing.
func (r *RouterImpl) Inbound(d ddp.Datagram, from RoutedPort) {
	_ = d
	_ = from
}

// logf emits one info line through the logger if configured.
func (r *RouterImpl) logf(msg string) {
	if r.logger == nil || !r.logger.Enabled(log.Info) {
		return
	}
	r.logger.Log1(log.Info, msg, log.Str("scope", Name))
}

// compile-time assertions.
var (
	_ Router              = (*RouterImpl)(nil)
	_ component.Component = (*RouterImpl)(nil)
)
