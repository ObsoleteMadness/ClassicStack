//go:build netbeui || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
	"github.com/ObsoleteMadness/ClassicStack/core/port/netbeui"
)

func init() {
	// Repeated schema: several named NetBEUI instances, each its own interface; they
	// feed the NetBEUI mini-router (not the AppleTalk router) — §M11.
	config.Register(config.SectionSchema{
		Key:      netbeui.Name,
		New:      func() config.Section { return &port.Section{SKey: netbeui.Name} },
		Repeated: true,
	})

	RegisterPort(netbeui.Name, func(ctx *BuildContext) (component.Component, error) {
		sec := port.InstanceFromModel(ctx.Model, netbeui.Name, ctx.Instance)
		logger := log.New(sec.InstanceName(), log.NewStderrSink(log.NewLevelVar(log.Info)))
		// NetBEUI is a NIC-bound transport (NBF-over-802.2-LLC on Ethernet), so — like
		// EtherTalk/IPX — it dispatches on the kind=nic branch of the opener table
		// (M11.c/D6): resolve this instance's effective interface and open it via the
		// injected NIC opener. It rides NO link.Framer (the port does its own LLC/NBF
		// encapsulation), so it takes the RAW NIC FrameLink. It is a NetBIOS transport
		// feeding its own NetBEUI mini-router, not the AppleTalk router (no ctx.Router,
		// no [Router] membership — that lands when the mini-router joins compose). A
		// nil opener yields the inert-but-configured form.
		open := nicLinkOpener(ctx, ctx.Model.EffectiveInterfaceFor(sec))
		return netbeui.NewInstanceFromOpener(sec, open, sectionMACFor(sec), logger)
	})
}
