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

// LToUDP and TashTalk are DISTINCT AppleTalk segments over different transports
// (UDP multicast vs serial) — each its own network number, zone, node space, and
// node-claim — NOT two ways onto one segment. So they are registered as two
// independent ports/components, and a router can bridge both at once. Both are
// served by the one transport-agnostic core/port/localtalk package (LLAP framing
// + runport); the transport differs only in the FrameLink the factory injects.
func init() {
	registerLocalTalk(localtalk.NameLToUDP, ltoudpOpener)
	registerLocalTalk(localtalk.NameTashTalk, tashtalkOpener)
}

// registerLocalTalk registers one LocalTalk segment port under key, opening its
// transport via openerFor (the per-Start FrameLink opener built from the port's
// section). The two segments share this body; only the key + transport differ.
func registerLocalTalk(key string, openerFor func(sec *port.Section) func() (link.FrameLink, error)) {
	config.Register(config.SectionSchema{
		Key: key,
		New: func() config.Section { return &port.Section{SKey: key} },
	})

	Register(key, func(ctx *BuildContext) (component.Component, error) {
		logger := log.New(key, log.NewStderrSink(log.NewLevelVar(log.Info)))

		// ctx.Opener is the "live device backends enabled" switch (the runtime root
		// injects it at the cmd edge; nil in a tag-free build or a unit test). When
		// nil, honour the same graceful-degradation contract every port follows:
		// come up inert-but-routed — attached to the router but moving no frames —
		// rather than opening a real socket/port. The conformance harness relies on
		// this.
		if ctx.Opener == nil {
			return localtalk.NewNamed(key, ctx.Model, nil, nil, ctx.Router, logger)
		}

		// LIVE. A LocalTalk segment is NOT NIC-bound: it rides its own transport
		// (LToUDP multicast or a TashTalk serial line), NOT the pcap/Ethernet opener
		// carried in ctx.Opener. So it opens its transport directly via the matching
		// pure-Go adapter rather than calling ctx.Opener. The shared Bridge is
		// deliberately NOT consulted — that concept is for the L2/NIC ports
		// (EtherTalk/IPX/NetBEUI), not for a UDP-tunnelled or serial segment.
		sec := port.SectionFromModel(ctx.Model, key)

		// LiveAddr is bound to the port after construction so the LLAP framer can read
		// the port's claimed node/network (for outbound source-node stamping and
		// inbound short-header reconstruction); the short/long header CHOICE is the
		// router's, read from the datagram, not from this addr.
		live := &framing.LiveAddr{}
		framer := &framing.LocalTalk{Addr: live, CalcChecksum: true}

		comp, err := localtalk.NewFromOpenerNamed(key, ctx.Model, openerFor(sec), framer, ctx.Router, logger)
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
// tests so each factory's live-wiring (LiveAddr binding, per-Start reopen) can be
// exercised without binding a real socket or serial port. Production points them
// at the pure-Go ltoudp / serial adapters.
var (
	ltoudpOpen = func(iface string) (link.FrameLink, error) {
		return ltoudp.Open(ltoudp.DefaultConfig(iface))
	}
	tashtalkOpen = func(devicePath string) (link.FrameLink, error) {
		return tashtalk.Open(tashtalk.DefaultConfig(devicePath))
	}
)

// ltoudpOpener binds the LToUDP segment's section to a per-Start FrameLink opener.
// sec.Iface is the local IPv4 ADDRESS to bind/join on (empty → every multicast-
// capable interface). A fresh socket per Start lets the port survive a UI
// Stop→Start (Close on Stop frees the socket).
func ltoudpOpener(sec *port.Section) func() (link.FrameLink, error) {
	iface := sec.Iface
	return func() (link.FrameLink, error) { return ltoudpOpen(iface) }
}

// tashtalkOpener binds the TashTalk segment's section to a per-Start FrameLink
// opener. sec.Iface is the serial DEVICE PATH (COM3, /dev/ttyUSB0). A fresh open
// per Start lets the port survive a UI Stop→Start (Close on Stop frees the port).
func tashtalkOpener(sec *port.Section) func() (link.FrameLink, error) {
	dev := sec.Iface
	return func() (link.FrameLink, error) { return tashtalkOpen(dev) }
}
