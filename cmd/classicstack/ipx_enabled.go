//go:build ipx || all

package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/port/ipx"
	"github.com/ObsoleteMadness/ClassicStack/port/rawlink"
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
	if h.port != nil {
		if err := h.port.Start(); err != nil {
			return err
		}
	}
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
		_ = h.port.Stop()
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

	link := cfg.Rawlink
	if link == nil && strings.TrimSpace(cfg.Interface) != "" {
		opened, err := rawlink.OpenPcap(rawlink.DefaultIPXConfig(cfg.Interface))
		if err != nil {
			return nil, fmt.Errorf("opening IPX rawlink on %q: %w", cfg.Interface, err)
		}
		link = opened
	}
	if link != nil {
		framing := parseIPXFraming(cfg.Framing)
		hook.port = ipx.NewPortWithFraming(link, framing)
		router.AddPort(hook.port)
		netlog.Info("[MAIN][IPX] pcap interface=%s framing=%s", cfg.Interface, cfg.Framing)
	} else {
		netlog.Warn("[MAIN][IPX] enabled but no -ipx-interface configured; IPX router idle")
	}

	return hook, nil
}

// parseIPXFraming maps the operator-facing framing name to the wire
// constant. Unknown values fall back to Ethernet II with a warning.
func parseIPXFraming(name string) ipx.Framing {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "ethernet_ii", "ethernet-ii", "ethernetii":
		return ipx.FramingEthernetII
	case "raw_802_3", "raw-802-3", "raw802.3":
		return ipx.FramingRaw8023
	case "llc", "802.2":
		return ipx.FramingLLC
	case "snap":
		return ipx.FramingSNAP
	default:
		netlog.Warn("[MAIN][IPX] unknown framing %q; defaulting to ethernet_ii", name)
		return ipx.FramingEthernetII
	}
}
