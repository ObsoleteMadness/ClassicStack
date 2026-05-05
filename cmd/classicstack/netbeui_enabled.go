//go:build netbeui || all

package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/port/netbeui"
	"github.com/ObsoleteMadness/ClassicStack/port/rawlink"
)

type netbeuiHookEnabled struct {
	port netbeui.Port
}

func (h *netbeuiHookEnabled) Start(_ context.Context) error {
	if h.port != nil {
		if err := h.port.Start(); err != nil {
			return err
		}
	}
	netlog.Info("[MAIN][NetBEUI] port up (stub)")
	return nil
}
func (h *netbeuiHookEnabled) Stop() error {
	if h.port != nil {
		return h.port.Stop()
	}
	return nil
}
func (h *netbeuiHookEnabled) Port() netbeui.Port { return h.port }

func wireNetBEUI(cfg NetBEUIConfig) (NetBEUIHook, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	link := cfg.Rawlink
	if link == nil && strings.TrimSpace(cfg.Interface) != "" {
		opened, err := rawlink.OpenPcap(rawlink.DefaultNetBEUIConfig(cfg.Interface))
		if err != nil {
			return nil, fmt.Errorf("opening NetBEUI rawlink on %q: %w", cfg.Interface, err)
		}
		link = opened
	}
	if link == nil {
		netlog.Warn("[MAIN][NetBEUI] enabled but no -netbeui-interface configured; NetBEUI idle")
		return &netbeuiHookEnabled{}, nil
	}
	netlog.Info("[MAIN][NetBEUI] pcap interface=%s", cfg.Interface)
	return &netbeuiHookEnabled{port: netbeui.NewPort(link)}, nil
}
