//go:build ethertalk || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/port/ethertalk"
)

func init() {
	Register(ethertalk.Name, func(ctx *BuildContext) (component.Component, error) {
		logger := log.New(ethertalk.Name, log.NewStderrSink(log.NewLevelVar(log.Info)))
		// The router is injected from the context so an enabled EtherTalk port
		// delivers inbound DDP to the shared router (and the router can Attach it).
		// The device link + framer are still nil here — building a real pcap link
		// from config is the next slice (it needs the port config schema: MAC,
		// framing). Until then the port comes up inert (no link) but ROUTED, so the
		// data path is one injection away.
		return ethertalk.New(ctx.Model, nil, nil, ctx.Router, logger)
	})
}
