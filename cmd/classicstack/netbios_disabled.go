//go:build !netbios && !all

package main

import (
	"context"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/service/netbios"
)

type netbiosHookDisabled struct{}

func (netbiosHookDisabled) Start(_ context.Context) error  { return nil }
func (netbiosHookDisabled) Stop() error                    { return nil }
func (netbiosHookDisabled) NameService() netbios.NameService { return nil }
func (netbiosHookDisabled) Service() *netbios.Service       { return nil }

func wireNetBIOS(cfg NetBIOSConfig) (NetBIOSHook, error) {
	if cfg.Enabled {
		netlog.Warn("[MAIN][NetBIOS] -netbios-enabled set but binary was built without -tags netbios; ignoring")
	}
	return netbiosHookDisabled{}, nil
}
