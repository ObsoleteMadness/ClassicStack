//go:build localtalk || all

package registry

import (
	"time"

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
		Key: key,
		New: func() config.Section {
			base := port.Base{SKey: key}
			if key == localtalk.NameTashTalk {
				return &port.TashTalkSection{Base: base}
			}
			return &port.LToUDPSection{Base: base}
		},
		Repeated:    true,
		DisplayName: key,
		Description: localTalkDescription(key),
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

// Per-transport default write-pace (min inter-frame gap per destination node), in
// milliseconds, used when a section leaves PaceMs at 0. A negative PaceMs disables
// pacing outright (link.Pace treats a non-positive gap as a no-op).
const (
	// defaultLToUDPPaceMs is a light 3 ms floor: the LToUDP transport has no link
	// backpressure and a captured MacTCP session showed a classic-Mac receiver
	// dropping frames that arrived <2 ms apart while coping at ~30 ms spacing. 3 ms
	// kills the tightest back-to-back bursts (the actual loss driver) at negligible
	// cost to light traffic; closed-loop flow control above (MacIP TCP window) does
	// the adaptive smoothing.
	defaultLToUDPPaceMs = 3
	// defaultTashTalkPaceMs is 0: the serial line self-paces (each frame takes real
	// wire time at 1 Mbit/s), so no software floor is needed by default.
	defaultTashTalkPaceMs = 0
)

// paceOpener decorates a per-Start FrameLink opener with per-destination-node write
// pacing (link.Pace). The gap comes from Section.PaceMs, falling back to defMs when
// PaceMs is 0; a negative PaceMs disables pacing. A nil base returns nil unchanged.
// Applied beneath captureOpener so a capture reflects the paced wire timing.
func paceOpener(sec *port.Section, defMs int, base func() (link.FrameLink, error)) func() (link.FrameLink, error) {
	if base == nil {
		return base
	}
	ms := defMs
	if sec != nil && sec.PaceMs != 0 {
		ms = sec.PaceMs // includes negative → disabled (link.Pace no-op)
	}
	if ms <= 0 {
		return base
	}
	gap := int64(ms) * int64(time.Millisecond)
	return func() (link.FrameLink, error) {
		fl, err := base()
		if err != nil || fl == nil {
			return fl, err
		}
		return link.Pace(fl, gap), nil
	}
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
	// Per-node write pacing: LToUDP has no link backpressure, so a fast producer
	// overruns a slow classic-Mac receiver unless successive frames to the same node
	// are spaced out. Applied BENEATH capture so the .pcap reflects the paced wire
	// timing that actually reaches the segment. Default 3 ms (see defaultLToUDPPaceMs).
	base = paceOpener(sec, defaultLToUDPPaceMs, base)
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
	iface := config.InterfaceSection{
		Kind: config.IfaceKindSerial, Device: device, Baud: sec.Baud,
		// RTS/CTS stays ON unless the port opts out: TashTalk clocks each frame onto
		// LocalTalk at 230.4 kbaud while the host feeds it at 1 Mbit/s, so without flow
		// control the adapter's buffer overruns and frames vanish (failed FCS).
		NoFlowControl: sec.NoFlowControl,
	}
	base := serialLinkOpener(ctx, iface, tashtalkFrame)
	// TashTalk self-paces on the 1 Mbit/s serial line (each frame takes real wire
	// time to clock out), so its default pace is 0 — but an operator can still set
	// pace_ms to add a floor. Applied beneath capture like LToUDP.
	base = paceOpener(sec, defaultTashTalkPaceMs, base)
	// TashTalk frames the serial byte stream as LLAP, so a Section.Capture writes DLT_LTALK.
	return captureOpener(sec, pcapfile.LinkTypeLocalTalk, base)
}

func localTalkDescription(key string) string {
	switch key {
	case localtalk.NameLToUDP:
		return "LocalTalk over UDP multicast (239.192.76.84:1954). Host-wide; optional bind address. Seeds an AppleTalk network and zone."
	case localtalk.NameTashTalk:
		return "LocalTalk over a TashTalk serial adaptor. Owns its own tty (device/baud); seeds an AppleTalk network and zone."
	}
	return "LocalTalk transport port."
}
