//go:build !netbeui && !all

package main

import (
	"context"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/port/netbeui"
)

type netbeuiHookDisabled struct{}

func (netbeuiHookDisabled) Start(_ context.Context) error { return nil }
func (netbeuiHookDisabled) Stop() error                   { return nil }
func (netbeuiHookDisabled) Port() netbeui.Port            { return nil }

func wireNetBEUI(cfg NetBEUIConfig) (NetBEUIHook, error) {
	if cfg.Enabled {
		netlog.Warn("[MAIN][NetBEUI] -netbeui-enabled set but binary was built without -tags netbeui; ignoring")
	}
	return netbeuiHookDisabled{}, nil
}
