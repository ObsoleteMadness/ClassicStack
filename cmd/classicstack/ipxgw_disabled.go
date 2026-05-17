//go:build !ipxgw && !all

package main

import "github.com/ObsoleteMadness/ClassicStack/netlog"

func wireIPXGW(cfg IPXGWConfig) (IPXGWHook, error) {
	if cfg.Enabled {
		netlog.Warn("[MAIN][IPXGW] enabled in config but binary was built without -tags ipxgw; ignoring")
	}
	return nil, nil
}
