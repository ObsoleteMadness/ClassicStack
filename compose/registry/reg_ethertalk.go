//go:build ethertalk || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/adapter/link/framing"
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
	"github.com/ObsoleteMadness/ClassicStack/core/port/ethertalk"
)

func init() {
	// Register the config schema so a TOML/UCI codec can round-trip [[ethertalk]]
	// into an EtherTalkSection (Base + seed + capture). Gated by the same tag as
	// the factory. Repeated: several named instances, each its own segment (§M11).
	config.Register(config.SectionSchema{
		Key:         ethertalk.Name,
		New:         func() config.Section { return &port.EtherTalkSection{Base: port.Base{SKey: ethertalk.Name}} },
		Repeated:    true,
		DisplayName: "EtherTalk",
		Description: "DDP over raw Ethernet (libpcap/Npcap). Binds an uplink bridge; seeds an AppleTalk network range and zone.",
	})

	RegisterPort(ethertalk.Name, func(ctx *BuildContext) (component.Component, error) {
		// Resolve THIS instance (ctx.Instance) from the model; the component names
		// itself from the instance name via the runport.
		sec := port.InstanceFromModel(ctx.Model, ethertalk.Name, ctx.Instance)
		logger := ctx.Logger(sec.InstanceName())

		// EtherTalk is a NIC-bound transport, so it dispatches on the kind=nic branch
		// of the opener table (M11.c/D6): nicLinkOpener resolves this instance's
		// EFFECTIVE interface name (its named iface, or the Bridge default when it
		// names none) and binds it to a per-Start opener over the injected NIC opener
		// (pcap at the cmd edge). A nil opener (no NIC backend in this build) yields a
		// nil per-Start opener → the inert-but-routed form, the same graceful
		// degradation as before: the port satisfies the lifecycle and is attached to
		// the router, but moves no frames until a backend exists.
		iface := ctx.Model.EffectiveInterfaceFor(sec)
		// The resolved station mac excludes this instance's own transmitted frames
		// from the capture at the kernel (nicLinkOpener); etherTalkFramer below
		// resolves it again for the AARP framer's source-address identity.
		opener := nicLinkOpener(ctx, sec, iface, ethertalk.BPFFilter, sectionMACFor(ctx, sec, iface))
		if opener == nil {
			return ethertalk.NewInstance(sec, nil, nil, ctx.Router, logger)
		}

		// LIVE framer. When a station MAC is configured we use the AARP-aware framer,
		// which claims a unique node address by probing on Start, resolves peer
		// node→MAC via the AMT (unicast instead of broadcast), and answers/gleans AARP.
		// Without a MAC there is no station identity to claim with, so we fall back to
		// the plain Ethernet/SNAP DDP framer (broadcast-only, pre-AARP behaviour).
		// NewInstanceFromOpener reopens the device on every Start (a closed libpcap
		// handle is terminal), so the port survives a UI Stop→Start.
		framer, claimWiring := etherTalkFramer(ctx, sec, iface)
		comp, err := ethertalk.NewInstanceFromOpener(sec, opener, framer, ctx.Router, logger)
		if err != nil || comp == nil {
			return comp, err
		}
		// Late-bind the claim → port.SetAddress hook now that the port exists: the AARP
		// framer publishes the claimed node into the shared LiveAddr (src stamping) and
		// calls OnClaimed, which we point at the port's SetAddress so the router sees the
		// claimed address. (The framer is built before the port, so this is wired here —
		// the same build-framer-then-bind-port shape LocalTalk uses for LiveAddr.)
		if claimWiring != nil {
			if p, ok := comp.(*ethertalk.Port); ok {
				claimWiring.OnClaimed = func(network uint16, node uint8, netMin, netMax uint16) {
					p.SetAddress(network, node, netMin, netMax)
				}
				// Symmetric read seam: point the port's AARP-table accessor at the
				// framer's live AMT so a diagnostic can print the resolved node→MAC
				// mappings (the framer owns the table; the port exposes it).
				p.SetAARPTableSource(claimWiring.AARPTable)
			}
		}
		return comp, nil
	})
}

// etherTalkFramer builds the EtherTalk framer from the section, resolving the station MAC
// via sectionMACFor (section mac, else interface hw_address, else the host NIC MAC).
// With a station MAC it returns the AARP-aware framer (*EtherTalkAARP) plus a handle the
// caller uses to wire OnClaimed once the port exists; with no MAC at all it returns the
// plain broadcast-only DDP framer (and a nil handle).
func etherTalkFramer(ctx *BuildContext, sec *port.Section, iface config.InterfaceSection) (link.Framer, *framing.EtherTalkAARP) {
	mac := sectionMACFor(ctx, sec, iface)
	if mac == ([6]byte{}) {
		return &framing.EtherTalk{}, nil // no station identity → plain broadcast framer
	}
	f := &framing.EtherTalkAARP{
		SrcMAC:     mac[:],
		Addr:       &framing.LiveAddr{},
		SeedNetMin: sec.SeedNetwork,
		SeedNetMax: sec.SeedNetworkEnd,
	}
	return f, f
}
