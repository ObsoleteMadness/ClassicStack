//go:build localtalk || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
	"github.com/ObsoleteMadness/ClassicStack/core/port/localtalk"
)

func init() {
	config.Register(config.SectionSchema{
		Key: localtalk.Name,
		New: func() config.Section { return &port.Section{SKey: localtalk.Name} },
	})

	Register(localtalk.Name, func(ctx *BuildContext) (component.Component, error) {
		logger := log.New(localtalk.Name, log.NewStderrSink(log.NewLevelVar(log.Info)))
		// Router injected from context so an enabled port delivers inbound DDP to the
		// shared router. The LLAP framer now EXISTS (adapter/link/framing.LocalTalk),
		// but LocalTalk is NOT NIC-bound — it rides LToUDP multicast or a serial line,
		// not the pcap/Ethernet opener in ctx.Opener — so there is no FrameLink to
		// frame yet. LocalTalk stays inert-but-routed until the LToUDP/serial FrameLink
		// adapter lands; that slice will build framing.LocalTalk{Addr: <the port>} and
		// inject the transport opener here.
		return localtalk.New(ctx.Model, nil, nil, ctx.Router, logger)
	})
}
