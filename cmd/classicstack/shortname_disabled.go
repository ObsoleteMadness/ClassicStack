//go:build !shortname && !all

package main

import "github.com/ObsoleteMadness/ClassicStack/netlog"

func wireShortname(cfg ShortnameConfig) (ShortnameHook, error) {
	if cfg.Enabled {
		netlog.Warn("[MAIN][Shortname] -shortname-enabled set but binary was built without -tags shortname; ignoring")
	}
	return nil, nil
}
