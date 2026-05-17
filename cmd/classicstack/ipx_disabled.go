//go:build !ipx && !all

package main

import (
	"context"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/router/ipx"
	ipxsvc "github.com/ObsoleteMadness/ClassicStack/service/ipx"
)

type ipxHookDisabled struct{}

func (ipxHookDisabled) Start(_ context.Context) error { return nil }
func (ipxHookDisabled) Stop() error                   { return nil }
func (ipxHookDisabled) Router() ipx.Router            { return nil }
func (ipxHookDisabled) SAP() *ipxsvc.SAPService       { return nil }

// wireIPX is the no-op stub used when the binary is built without the
// ipx tag. It logs a warning if the operator asked for IPX and returns
// a disabled hook so the rest of main.go skips IPX wiring.
func wireIPX(cfg IPXConfig) (IPXHook, error) {
	if cfg.Enabled {
		netlog.Warn("[MAIN][IPX] -ipx-enabled set but binary was built without -tags ipx; ignoring")
	}
	return ipxHookDisabled{}, nil
}
