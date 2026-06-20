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

		// EtherTalk is a NIC-bound transport, so it dispatches on the kind=nic branch
		// of the opener table (M11.c/D6): nicLinkOpener resolves this instance's
		// EFFECTIVE interface name (its named iface, or the Bridge default when it
		// names none) and binds it to a per-Start opener over the injected NIC opener
		// (pcap at the cmd edge). A nil opener (no NIC backend in this build) yields a
		// nil per-Start opener → the inert-but-routed form, the same graceful
		// degradation as before: the port satisfies the lifecycle and is attached to
		// the router, but moves no frames until a backend exists.
		opener := nicLinkOpener(ctx, ctx.Model.EffectiveInterfaceFor(sec))
		if opener == nil {
			return ethertalk.NewInstance(sec, nil, nil, ctx.Router, logger)
		}

		// LIVE: an Ethernet/SNAP framer stamped with the configured station MAC.
		// NewFromOpener reopens the device on every Start (a closed libpcap handle is
		// terminal), so the port survives a UI Stop→Start. A blank/invalid MAC leaves
		// SrcMAC nil and the framer falls back to the AppleTalk broadcast MAC
		// (pre-AARP behaviour).
		framer := etherTalkFramer(sec)
		return ethertalk.NewInstanceFromOpener(sec, opener, framer, ctx.Router, logger)
	})
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
