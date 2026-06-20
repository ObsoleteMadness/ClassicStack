//go:build ipx || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
	"github.com/ObsoleteMadness/ClassicStack/core/port/ipx"
)

func init() {
	// Repeated schema: several named IPX instances, each its own interface/segment;
	// they join the IPX mini-router (not the AppleTalk router) — §M11.
	config.Register(config.SectionSchema{
		Key:      ipx.Name,
		New:      func() config.Section { return &port.Section{SKey: ipx.Name} },
		Repeated: true,
	})

	RegisterPort(ipx.Name, func(ctx *BuildContext) (component.Component, error) {
		sec := port.InstanceFromModel(ctx.Model, ipx.Name, ctx.Instance)
		logger := log.New(sec.InstanceName(), log.NewStderrSink(log.NewLevelVar(log.Info)))
		// IPX is a NIC-bound transport (IPX-over-Ethernet), so — like EtherTalk — it
		// dispatches on the kind=nic branch of the opener table (M11.c/D6): resolve
		// this instance's effective interface and open it via the injected NIC opener.
		// Unlike EtherTalk it rides NO link.Framer: the port does its own Ethernet
		// encapsulation, so it takes the RAW NIC FrameLink. It feeds its own IPX
		// mini-router, not the AppleTalk router (so no ctx.Router and no [Router]
		// membership — that lands when the IPX mini-router itself joins compose). A
		// nil opener (no NIC backend) yields the inert-but-configured form.
		open := nicLinkOpener(ctx, ctx.Model.EffectiveInterfaceFor(sec))
		return ipx.NewInstanceFromOpener(sec, open, sectionMACFor(sec), logger)
	})
}
