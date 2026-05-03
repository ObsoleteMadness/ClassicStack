//go:build !macip && !all

package main

import "github.com/ObsoleteMadness/ClassicStack/netlog"

// wireMacIP is the no-op stub used when the binary is built without the
// macip tag. It logs a warning if the operator asked for MacIP and exits
// returning a nil hook so the rest of main.go skips MacIP wiring.
func wireMacIP(cfg MacIPConfig) (MacIPHook, error) {
	if cfg.Enabled {
		netlog.Warn("[MAIN][MacIP] -macip-enabled set but binary was built without -tags macip; ignoring")
	}
	return nil, nil
}
