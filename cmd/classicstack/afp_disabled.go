//go:build !afp && !all

package main

import (
	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/service"
)

type afpHookDisabled struct{}

func (afpHookDisabled) Services() []service.Service     { return nil }
func (afpHookDisabled) AttachMacIP(_ AFPSessionHooks)   {}

// wireAFP is the no-op stub used when the binary is built without the
// afp tag. It logs a warning if the operator asked for AFP and returns
// a nil hook so the rest of main.go skips AFP wiring.
func wireAFP(in AFPWiring) (AFPHook, error) {
	if in.FromConfig && in.Source.K != nil && in.Source.K.Exists("AFP") {
		netlog.Warn("[MAIN][AFP] [AFP] section present in config but binary was built without -tags afp; ignoring")
	} else if !in.FromConfig {
		if len(in.Flags.VolumeFlagValues) > 0 || in.Flags.ExtensionMap != "" {
			netlog.Warn("[MAIN][AFP] -afp-* flags set but binary was built without -tags afp; ignoring")
		}
	}
	return afpHookDisabled{}, nil
}
