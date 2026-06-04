package router

import (
	"context"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/port"
	"github.com/ObsoleteMadness/ClassicStack/service"
)

// The methods in this file mutate the router's membership (ports and
// services) while it is running, so the management plane can enable or
// disable a transport or service without restarting the whole process.
// They take r.membership for writing; the receive path (deliver/Inbound)
// takes it for reading, so dispatch never observes a half-updated map.

// AddService starts s, registers the socket it listens on, and adds it to
// the active service set. If Start fails the service is not added.
func (r *Router) AddService(ctx context.Context, s service.Service) error {
	netlog.Info("%s adding service %T", r.ShortString(), s)
	if err := s.Start(ctx, r); err != nil {
		return err
	}
	r.membership.Lock()
	r.Services = append(r.Services, s)
	r.registerServiceSocket(s)
	r.membership.Unlock()
	return nil
}

// RemoveService stops s and removes it from the active service set and the
// socket dispatch map. The service's Stop error is returned but removal
// happens regardless so a failing Stop cannot wedge the membership.
func (r *Router) RemoveService(s service.Service) error {
	netlog.Info("%s removing service %T", r.ShortString(), s)
	r.membership.Lock()
	r.unregisterServiceSocket(s)
	for i, svc := range r.Services {
		if svc == s {
			r.Services = append(r.Services[:i], r.Services[i+1:]...)
			break
		}
	}
	r.membership.Unlock()
	return s.Stop()
}

// AddPort starts p, binds the LLAP link manager to it (for LocalTalk-style
// ports), and adds it to the active port set. RTMP's seed-network handling
// during Start advertises the port's networks/zones, so no explicit route
// injection is needed here.
func (r *Router) AddPort(_ context.Context, p port.Port) error {
	netlog.Info("%s adding port %T", r.ShortString(), p)
	r.bindPortLLAP(p)
	if err := p.Start(r); err != nil {
		return err
	}
	r.membership.Lock()
	r.Ports = append(r.Ports, p)
	r.membership.Unlock()
	return nil
}

// RemovePort stops p, removes it from the active port set, and reconciles
// the routing and zone tables by withdrawing every route reachable through
// p. This is the live counterpart to a port disappearing: disabling e.g.
// LToUDP drops its seed network and any networks learned over it so the
// router stops advertising and forwarding to them.
func (r *Router) RemovePort(p port.Port) error {
	netlog.Info("%s removing port %T", r.ShortString(), p)
	r.membership.Lock()
	for i, pt := range r.Ports {
		if pt == p {
			r.Ports = append(r.Ports[:i], r.Ports[i+1:]...)
			break
		}
	}
	r.membership.Unlock()

	// Withdraw routes/zones for the port before stopping it so dispatch no
	// longer selects it as a next hop.
	r.RoutingTable.RemoveEntriesForPort(p)

	return p.Stop()
}
