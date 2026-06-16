//go:build ethertalk || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/adapter/link/framing"
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
	"github.com/ObsoleteMadness/ClassicStack/core/port/ethertalk"
)

func init() {
	// Register the config schema so a TOML/UCI codec can round-trip the [EtherTalk]
	// section (iface/mac/seed) into a *port.Section. Gated by the same tag as the
	// factory, so a build without EtherTalk neither builds nor round-trips it.
	// Repeated schema: one [[EtherTalk]] array-of-tables, several named instances —
	// each a distinct EtherTalk segment bound to its own interface (§M11).
	config.Register(config.SectionSchema{
		Key:      ethertalk.Name,
		New:      func() config.Section { return &port.Section{SKey: ethertalk.Name} },
		Repeated: true,
	})

	RegisterPort(ethertalk.Name, func(ctx *BuildContext) (component.Component, error) {
		// Resolve THIS instance (ctx.Instance) from the model; the component names
		// itself from the instance name via the runport.
		sec := port.InstanceFromModel(ctx.Model, ethertalk.Name, ctx.Instance)
		logger := log.New(sec.InstanceName(), log.NewStderrSink(log.NewLevelVar(log.Info)))

		// No device backend in this build (nil Opener) → the inert-but-routed form,
		// the same graceful degradation as before: the port satisfies the lifecycle
		// and is attached to the router, but moves no frames until an opener exists.
		if ctx.Opener == nil {
			return ethertalk.NewInstance(sec, nil, nil, ctx.Router, logger)
		}

		// LIVE: bind the EFFECTIVE interface (this instance's named iface, resolved
		// against the interface namespace — Bridge default when it names none) to a
		// per-Start FrameLink opener and an Ethernet/SNAP framer stamped with the
		// configured station MAC. NewFromOpener reopens the device on every Start (a
		// closed libpcap handle is terminal), so the port survives a UI Stop→Start. A
		// blank/invalid MAC leaves SrcMAC nil and the framer falls back to the
		// AppleTalk broadcast MAC (pre-AARP behaviour).
		opener := openerFor(ctx.Opener, ctx.Model.EffectiveInterfaceFor(sec).Name)
		framer := etherTalkFramer(sec)
		return ethertalk.NewInstanceFromOpener(sec, opener, framer, ctx.Router, logger)
	})
}

// openerFor binds an interface name to a zero-arg FrameLink opener the port calls
// on each Start.
func openerFor(open LinkOpener, iface string) func() (link.FrameLink, error) {
	return func() (link.FrameLink, error) { return open(iface) }
}

// etherTalkFramer builds the Ethernet/SNAP DDP framer from the section, stamping
// the configured station MAC as the outbound Ethernet source when it parses; an
// empty or malformed MAC yields a nil SrcMAC (the framer then uses a zero source
// and the broadcast destination — the pre-AARP default).
func etherTalkFramer(sec *port.Section) link.Framer {
	f := &framing.EtherTalk{}
	if mac, err := port.ParseMAC(sec.MAC); err == nil && sec.MAC != "" {
		f.SrcMAC = mac[:]
	}
	return f
}
