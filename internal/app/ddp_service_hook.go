package app

import (
	"context"
	"errors"

	"github.com/ObsoleteMadness/ClassicStack/router"
	"github.com/ObsoleteMadness/ClassicStack/service"
)

// ddpServiceHook adapts a group of DDP services (the ones a single optional
// subsystem — AFP, MacIP, or the IPX gateway — registers with the AppleTalk
// router) to the standalone hook lifecycle. Unlike the transport hooks that
// own their own listener, these services ride the shared router; the hook
// drives them with the router's runtime AddService/RemoveService primitives
// so the management UI can start and stop each subsystem independently
// without rebuilding the whole stack.
//
// The router must already be running before Start is called: AddService
// starts each service against the live router. The supervisor guarantees
// this by starting the router before walking the hook order.
type ddpServiceHook struct {
	router   *router.Router
	services []service.Service
	running  bool
}

// newDDPServiceHook returns a hook over svcs, or nil when svcs is empty so the
// supervisor records no unit for a subsystem that contributed no services.
func newDDPServiceHook(r *router.Router, svcs []service.Service) *ddpServiceHook {
	if len(svcs) == 0 {
		return nil
	}
	return &ddpServiceHook{router: r, services: svcs}
}

// Start registers (and starts) each managed service against the router. On
// the first failure it rolls back the services already added so a partial
// start does not leave half the subsystem live.
func (h *ddpServiceHook) Start(ctx context.Context) error {
	if h.running {
		return nil
	}
	for i, svc := range h.services {
		if err := h.router.AddService(ctx, svc); err != nil {
			for j := i - 1; j >= 0; j-- {
				_ = h.router.RemoveService(h.services[j])
			}
			return err
		}
	}
	h.running = true
	return nil
}

// Stop removes (and stops) each managed service from the router in reverse
// registration order, joining any teardown errors.
func (h *ddpServiceHook) Stop() error {
	if !h.running {
		return nil
	}
	var errs []error
	for i := len(h.services) - 1; i >= 0; i-- {
		if err := h.router.RemoveService(h.services[i]); err != nil {
			errs = append(errs, err)
		}
	}
	h.running = false
	return errors.Join(errs...)
}
