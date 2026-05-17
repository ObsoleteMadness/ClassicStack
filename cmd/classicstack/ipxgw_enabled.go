//go:build ipxgw || all

package main

import (
	"github.com/ObsoleteMadness/ClassicStack/netlog"
	routeripx "github.com/ObsoleteMadness/ClassicStack/router/ipx"
	"github.com/ObsoleteMadness/ClassicStack/service"
	"github.com/ObsoleteMadness/ClassicStack/service/ipxgw"
)

type ipxgwHookEnabled struct {
	svc *ipxgw.Service
}

func (h *ipxgwHookEnabled) Service() service.Service { return h.svc }

func (h *ipxgwHookEnabled) AttachIPXRouter(r routeripx.Router) {
	if r == nil {
		return
	}
	h.svc.SetIPXRouter(r)
	netlog.Info("[MAIN][IPXGW] attached to IPX router; encapsulated IPX will be forwarded")
}

func wireIPXGW(cfg IPXGWConfig) (IPXGWHook, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	bindings := make([]ipxgw.ZoneBinding, 0, len(cfg.Bindings))
	for _, b := range cfg.Bindings {
		bindings = append(bindings, ipxgw.ZoneBinding{
			Object: []byte(b.Object),
			Zone:   []byte(b.Zone),
		})
	}
	svc := ipxgw.NewWithConfig(cfg.NBP, bindings, ipxgw.Config{
		IPXNetwork: cfg.IPXNetwork,
	})
	netlog.Info("[MAIN][IPXGW] gateway enabled; ipx-net=0x%08x; %d explicit zone binding(s)",
		svc.IPXNetwork(), len(bindings))
	return &ipxgwHookEnabled{svc: svc}, nil
}
