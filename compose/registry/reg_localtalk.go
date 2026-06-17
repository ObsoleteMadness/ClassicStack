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
	registerLocalTalk(localtalk.NameLToUDP, ltoudpLinkOpener)
	registerLocalTalk(localtalk.NameTashTalk, tashtalkLinkOpener)
}

// segmentOpener builds the per-Start FrameLink opener for one LocalTalk segment
// instance from the build context. It returns nil when the relevant device backend
// is absent (nil ctx.Opener/ctx.Serial or unconfigured), the signal to build the
// inert-but-routed form. The two segments differ ONLY in this function: LToUDP rides
// its own multicast adapter; TashTalk rides the shared serial opener + its framer.
type segmentOpener func(ctx *BuildContext, sec *port.Section) func() (link.FrameLink, error)

// registerLocalTalk registers one LocalTalk segment port under key, building its
// per-Start transport opener via openerFor. The two segments share this body (LLAP
// framing, LiveAddr binding, router attach); only the key + transport opener differ.
func registerLocalTalk(key string, openerFor segmentOpener) {
	// Repeated schema: several named instances per segment key — e.g. multiple
	// TashTalk dongles, each its own serial line and segment (§M11).
	config.Register(config.SectionSchema{
		Key:      key,
		New:      func() config.Section { return &port.Section{SKey: key} },
		Repeated: true,
	})

	RegisterPort(key, func(ctx *BuildContext) (component.Component, error) {
		sec := port.InstanceFromModel(ctx.Model, key, ctx.Instance)
		logger := log.New(sec.InstanceName(), log.NewStderrSink(log.NewLevelVar(log.Info)))

		// Build the transport opener. A nil result means the device backend is absent
		// (a tag-free build, a unit test, or a serial port with no serial opener
		// injected): honour the graceful-degradation contract every port follows —
		// come up inert-but-routed (attached to the router but moving no frames). The
		// conformance harness relies on this.
		open := openerFor(ctx, sec)
		if open == nil {
			return localtalk.NewInstance(sec, nil, nil, ctx.Router, logger)
		}

		// LIVE. LiveAddr is bound to the port after construction so the LLAP framer can
		// read the port's claimed node/network (for outbound source-node stamping and
		// inbound short-header reconstruction); the short/long header CHOICE is the
		// router's, read from the datagram, not from this addr.
		live := &framing.LiveAddr{}
		framer := &framing.LocalTalk{Addr: live, CalcChecksum: true}

		comp, err := localtalk.NewInstanceFromOpener(sec, open, framer, ctx.Router, logger)
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

// ltoudpOpen is the LToUDP transport open seam, swappable in tests so the factory's
// live-wiring (LiveAddr binding, per-Start reopen) can be exercised without binding a
// real socket. Production points it at the pure-Go ltoudp adapter.
var ltoudpOpen = func(iface string) (link.FrameLink, error) {
	return ltoudp.Open(ltoudp.DefaultConfig(iface))
}

// tashtalkFrame wraps an open serial byte stream in the TashTalk FrameLink. It is the
// SerialFramer the serial-opener dispatch pairs with the injected serial opener; a
// var so tests can stand in a fake framer. Production points it at tashtalk.NewStream.
var tashtalkFrame SerialFramer = tashtalk.NewStream

// ltoudpLinkOpener is the LToUDP segment's transport opener: a LToUDP segment is NOT
// NIC-bound and NOT serial — it rides its own multicast transport, so it ignores the
// kind→opener dispatch and opens the pure-Go ltoudp adapter directly. sec.Iface is
// the local IPv4 ADDRESS to bind/join on (empty → every multicast-capable interface).
// A fresh socket per Start lets the port survive a UI Stop→Start.
//
// It is still gated on ctx.Opener as the "live device backends enabled" switch (the
// same flag the NIC ports use): a nil Opener (tag-free build / conformance harness /
// unit test) yields nil → the inert-but-routed form, so an LToUDP segment does not
// bind a real socket where every other port stays inert. It does NOT call ctx.Opener
// (that is the pcap/NIC opener); it only reads its presence as the enabled signal.
func ltoudpLinkOpener(ctx *BuildContext, sec *port.Section) func() (link.FrameLink, error) {
	if ctx.Opener == nil {
		return nil
	}
	iface := sec.Iface
	return func() (link.FrameLink, error) { return ltoudpOpen(iface) }
}

// tashtalkLinkOpener is the TashTalk segment's transport opener: TashTalk rides a
// serial line, so it takes the kind=serial branch of the opener dispatch (M11.c/D7).
// It resolves the instance's effective interface and, when that is a serial-kind
// interface, opens the device via the injected shared serial opener and frames it
// with tashtalk. Returns nil (→ inert) when no serial backend is injected. The device
// path/baud come from the named serial INTERFACE (the §3b/D7 move), falling back to
// sec.Iface as the device path when the interface declares none — back-compat with a
// section that put the device path directly in iface.
func tashtalkLinkOpener(ctx *BuildContext, sec *port.Section) func() (link.FrameLink, error) {
	iface, ok := effectiveSerialInterface(ctx.Model, sec)
	if !ok {
		// The resolved interface is not serial-kind. Back-compat: treat sec.Iface as
		// the device path directly (a bare serial section with no namespace entry).
		iface = config.InterfaceSection{Kind: config.IfaceKindSerial, Device: sec.Iface}
	}
	if iface.Device == "" {
		iface.Device = sec.Iface
	}
	return serialLinkOpener(ctx, iface, tashtalkFrame)
}
