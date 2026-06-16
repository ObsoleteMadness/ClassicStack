//go:build localtalk || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/adapter/link/framing"
	"github.com/ObsoleteMadness/ClassicStack/adapter/link/ltoudp"
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

		// LIVE. LocalTalk is NOT NIC-bound: it rides LToUDP multicast (or, later, a
		// serial line), NOT the pcap/Ethernet opener carried in ctx.Opener. So it
		// opens its OWN transport directly via the pure-Go ltoudp adapter (net +
		// x/net/ipv4, no cgo) rather than calling ctx.Opener. The Section's Iface,
		// when set, is the local IPv4 ADDRESS to bind/join on (not a NIC name); empty
		// means every multicast-capable interface (the legacy default). The shared
		// Bridge is deliberately NOT consulted — that concept is for the L2/NIC ports
		// (EtherTalk/IPX/NetBEUI), not for a UDP-tunnelled segment.
		sec := port.SectionFromModel(ctx.Model, localtalk.Name)

		// LiveAddr is bound to the port after construction so the LLAP framer can read
		// the port's claimed node/network (for outbound source-node stamping and
		// inbound short-header reconstruction); the short/long header CHOICE is the
		// router's, read from the datagram, not from this addr.
		live := &framing.LiveAddr{}
		framer := &framing.LocalTalk{Addr: live, CalcChecksum: true}
		opener := ltoudpOpener(sec.Iface)

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

// ltoudpOpen is the LToUDP transport open seam, swappable in tests so the
// localtalk factory's live-wiring (LiveAddr binding, per-Start reopen) can be
// exercised without binding a real multicast socket. Production points it at the
// pure-Go ltoudp adapter.
var ltoudpOpen = func(iface string) (link.FrameLink, error) {
	return ltoudp.Open(ltoudp.DefaultConfig(iface))
}

// ltoudpOpener binds the LToUDP interface address to a zero-arg FrameLink opener
// the port calls on each Start — a fresh multicast socket per Start, so the port
// survives a UI Stop→Start (Close on Stop frees the socket).
func ltoudpOpener(iface string) func() (link.FrameLink, error) {
	return func() (link.FrameLink, error) { return ltoudpOpen(iface) }
}
