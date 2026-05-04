//go:build netbeui || all

package main

import (
	"context"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/port/netbeui"
)

type netbeuiHookEnabled struct {
	port netbeui.Port
}

func (h *netbeuiHookEnabled) Start(_ context.Context) error {
	netlog.Info("[MAIN][NetBEUI] port up (stub)")
	return nil
}
func (h *netbeuiHookEnabled) Stop() error {
	if h.port != nil {
		return h.port.Close()
	}
	return nil
}
func (h *netbeuiHookEnabled) Port() netbeui.Port { return h.port }

func wireNetBEUI(cfg NetBEUIConfig) (NetBEUIHook, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if cfg.Rawlink == nil {
		netlog.Warn("[MAIN][NetBEUI] enabled but no rawlink supplied; NetBEUI idle (stub)")
		return &netbeuiHookEnabled{}, nil
	}
	return &netbeuiHookEnabled{port: netbeui.NewPort(cfg.Rawlink)}, nil
}
