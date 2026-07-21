//go:build localtalk || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/adapter/capture/pcapfile"
	"github.com/ObsoleteMadness/ClassicStack/adapter/link/framing"
	"github.com/ObsoleteMadness/ClassicStack/adapter/link/ltoudp"
	"github.com/ObsoleteMadness/ClassicStack/adapter/link/tashtalk"
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
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
	// respondToEnq differs per segment (spec/09 §"respondToEnq Flag"): the LToUDP
	// shared simulated segment must answer ENQs for a claimed node so new joiners
	// learn it is taken; the physical TashTalk medium defends in hardware, so the
	// host stays silent.
	registerLocalTalk(localtalk.NameLToUDP, ltoudpLinkOpener, true)
	registerLocalTalk(localtalk.NameTashTalk, tashtalkLinkOpener, false)
}

// segmentOpener builds the per-Start FrameLink opener for one LocalTalk segment
// instance from the build context. It returns nil when the relevant device backend
// is absent (nil ctx.Opener/ctx.Serial or unconfigured), the signal to build the
// inert-but-routed form. The two segments differ ONLY in this function: LToUDP rides
// its own multicast adapter; TashTalk rides the shared serial opener + its framer.
type segmentOpener func(ctx *BuildContext, sec *port.Section) func() (link.FrameLink, error)

// registerLocalTalk registers one LocalTalk segment port under key, building its
// per-Start transport opener via openerFor. The two segments share this body (LLAP
// framing + node-claim, OnClaimed→SetAddress wiring, router attach); only the key,
// transport opener, and respondToEnq (shared-segment vs hardware-defended) differ.
func registerLocalTalk(key string, openerFor segmentOpener, respondToEnq bool) {
	// Repeated schema: several named instances per segment key — e.g. multiple
	// TashTalk dongles, each its own serial line and segment (§M11).
	config.Register(config.SectionSchema{
		Key:      key,
		New:      func() config.Section { return &port.Section{SKey: key} },
		Repeated: true,
	})

	RegisterPort(key, func(ctx *BuildContext) (component.Component, error) {
		sec := port.InstanceFromModel(ctx.Model, key, ctx.Instance)
		logger := ctx.Logger(sec.InstanceName())

		// Build the transport opener. A nil result means the device backend is absent
		// (a tag-free build, a unit test, or a serial port with no serial opener
		// injected): honour the graceful-degradation contract every port follows —
		// come up inert-but-routed (attached to the router but moving no frames). The
		// conformance harness relies on this.
		open := openerFor(ctx, sec)
		if open == nil {
			return localtalk.NewInstance(sec, nil, nil, ctx.Router, logger)
		}

		// LIVE. The LLAP framer runs the node-claim (ENQ/ACK) dance: it probes a
		// candidate node, rerolls on a collision, and on success publishes the claimed
		// node into the shared LiveAddr (so the framer stamps it as the LLAP source +
		// reconstructs inbound short-header network/node) AND via OnClaimed, which we
		// point at the port's SetAddress so the router sees the claim. This mirrors the
		// EtherTalk AARP wiring. The short/long header CHOICE stays the router's, read
		// from the datagram, not from this addr.
		live := &framing.LiveAddr{}
		framer := &framing.LocalTalk{
			Addr:         live,
			Live:         live,
			CalcChecksum: true,
			EnableClaim:  true,
			RespondToEnq: respondToEnq,
			SeedNetwork:  sec.SeedNetwork,
			Logger:       logger,
		}

		comp, err := localtalk.NewInstanceFromOpener(sec, open, framer, ctx.Router, logger)
		if err != nil || comp == nil {
			return comp, err
		}
		// Late-bind the claim → port.SetAddress hook now that the port exists: the claim
		// goroutine publishes the claimed node into the LiveAddr (src stamping) and calls
		// OnClaimed, which records the address on the port so the router can deliver to
		// it. (LocalTalk is non-extended: netMin==netMax==network.)
		if p, ok := comp.(interface {
			SetAddress(network uint16, node uint8, netMin, netMax uint16)
		}); ok {
			framer.OnClaimed = func(network uint16, node uint8, netMin, netMax uint16) {
				p.SetAddress(network, node, netMin, netMax)
			}
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
	base := func() (link.FrameLink, error) { return ltoudpOpen(iface) }
	// LToUDP presents clean LLAP frames upward, so a Section.Capture writes DLT_LTALK.
	return captureOpener(sec, pcapfile.LinkTypeLocalTalk, base)
}

// tashtalkLinkOpener is the TashTalk segment's transport opener: TashTalk rides a
// serial line, so it opens the device via the injected shared serial opener and
// frames it with tashtalk. Returns nil (→ inert) when no serial backend is injected.
// The device path/baud come from the PORT itself (Section.Device/Baud) — a TashTalk
// port owns its own tty, so serial is a port property, not a named interface (the
// "one interface = the uplink bridge" model). Device falls back to sec.Iface so an
// older section that put the device path in iface still opens.
func tashtalkLinkOpener(ctx *BuildContext, sec *port.Section) func() (link.FrameLink, error) {
	device := sec.Device
	if device == "" {
		device = sec.Iface
	}
	iface := config.InterfaceSection{Kind: config.IfaceKindSerial, Device: device, Baud: sec.Baud}
	base := serialLinkOpener(ctx, iface, tashtalkFrame)
	// TashTalk frames the serial byte stream as LLAP, so a Section.Capture writes DLT_LTALK.
	return captureOpener(sec, pcapfile.LinkTypeLocalTalk, base)
}
