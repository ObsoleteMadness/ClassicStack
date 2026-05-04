//go:build ipx || all

package main

import (
	"context"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/port/ipx"
	routeripx "github.com/ObsoleteMadness/ClassicStack/router/ipx"
	ipxsvc "github.com/ObsoleteMadness/ClassicStack/service/ipx"
)

type ipxHookEnabled struct {
	router routeripx.Router
	port   ipx.Port
	rip    *ipxsvc.RIPService
	sap    *ipxsvc.SAPService
}

func (h *ipxHookEnabled) Router() routeripx.Router { return h.router }

func (h *ipxHookEnabled) Start(ctx context.Context) error {
	if err := h.rip.Start(ctx); err != nil {
		return err
	}
	if err := h.sap.Start(ctx); err != nil {
		return err
	}
	netlog.Info("[MAIN][IPX] router up; RIP+SAP registered (stub)")
	return nil
}

func (h *ipxHookEnabled) Stop() error {
	if h.rip != nil {
		_ = h.rip.Stop()
	}
	if h.sap != nil {
		_ = h.sap.Stop()
	}
	if h.port != nil {
		_ = h.port.Close()
	}
	return nil
}

func wireIPX(cfg IPXConfig) (IPXHook, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	router := routeripx.NewRouter()

	hook := &ipxHookEnabled{
		router: router,
		rip:    ipxsvc.NewRIPService(router),
		sap:    ipxsvc.NewSAPService(router),
	}

	if cfg.Rawlink != nil {
		hook.port = ipx.NewPort(cfg.Rawlink)
		router.AddPort(hook.port)
	} else {
		netlog.Warn("[MAIN][IPX] enabled but no rawlink supplied; IPX router idle (stub)")
	}

	return hook, nil
}
