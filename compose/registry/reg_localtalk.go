//go:build localtalk || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/adapter/link/framing"
	"github.com/ObsoleteMadness/ClassicStack/adapter/link/ltoudp"
	"github.com/ObsoleteMadness/ClassicStack/adapter/link/tashtalk"
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
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

		// ctx.Opener is the "live device backends enabled" switch (the runtime root
		// injects it at the cmd edge; nil in a tag-free build or a unit test). When
		// nil, honour the same graceful-degradation contract every port follows:
		// come up inert-but-routed — attached to the router but moving no frames —
		// rather than opening a real socket. The conformance harness relies on this.
		if ctx.Opener == nil {
			return localtalk.New(ctx.Model, nil, nil, ctx.Router, logger)
		}

		// LIVE. LocalTalk is NOT NIC-bound: it rides LToUDP multicast OR a TashTalk
		// serial line (per sec.Transport), NOT the pcap/Ethernet opener carried in
		// ctx.Opener. So it opens its OWN transport directly via the matching pure-Go
		// adapter rather than calling ctx.Opener. The shared Bridge is deliberately
		// NOT consulted — that concept is for the L2/NIC ports (EtherTalk/IPX/
		// NetBEUI), not for a UDP-tunnelled or serial segment.
		sec := port.SectionFromModel(ctx.Model, localtalk.Name)

		// LiveAddr is bound to the port after construction so the LLAP framer can read
		// the port's claimed node/network (for outbound source-node stamping and
		// inbound short-header reconstruction); the short/long header CHOICE is the
		// router's, read from the datagram, not from this addr.
		live := &framing.LiveAddr{}
		framer := &framing.LocalTalk{Addr: live, CalcChecksum: true}
		opener := localTalkOpener(sec)

		comp, err := localtalk.NewFromOpener(ctx.Model, opener, framer, ctx.Router, logger)
		if err != nil || comp == nil {
			return comp, err
		}
		// The constructed port exposes Network()/Node() (runport) — exactly the
		// framing.Addr shape. Bind it so the framer tracks the live claim.
		if src, ok := comp.(framing.Addr); ok {
			live.Set(src)
		}
		return comp, nil
	})
}

// ltoudpOpen / tashtalkOpen are the LocalTalk transport open seams, swappable in
// tests so the factory's live-wiring (LiveAddr binding, per-Start reopen,
// transport dispatch) can be exercised without binding a real socket or serial
// port. Production points them at the pure-Go ltoudp / serial adapters.
var (
	ltoudpOpen = func(iface string) (link.FrameLink, error) {
		return ltoudp.Open(ltoudp.DefaultConfig(iface))
	}
	tashtalkOpen = func(devicePath string) (link.FrameLink, error) {
		return tashtalk.Open(tashtalk.DefaultConfig(devicePath))
	}
)

// localTalkOpener returns a zero-arg FrameLink opener the port calls on each
// Start, dispatching on sec.Transport: TashTalk serial when "serial", LToUDP
// otherwise (the default). For LToUDP sec.Iface is the local IPv4 bind address;
// for serial it is the device path. A fresh link per Start lets the port survive
// a UI Stop→Start (Close on Stop frees the socket/port).
func localTalkOpener(sec *port.Section) func() (link.FrameLink, error) {
	if sec.Transport == port.TransportSerial {
		dev := sec.Iface
		return func() (link.FrameLink, error) { return tashtalkOpen(dev) }
	}
	iface := sec.Iface
	return func() (link.FrameLink, error) { return ltoudpOpen(iface) }
}
