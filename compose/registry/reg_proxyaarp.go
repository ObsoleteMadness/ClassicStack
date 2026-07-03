//go:build ethertalk || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/adapter/bridge"
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
)

func init() {
	// Register the ProxyAARP section schema so a TOML/UCI codec round-trips the
	// [ProxyAARP] singleton (tunnel/egress interfaces + egress MAC). Gated by the same
	// tag as the factory, so a build without EtherTalk neither builds nor round-trips it.
	bridge.RegisterSection()

	// ProxyAARP is the Wi-Fi/tunnel bridge: it forwards AppleTalk frames between the
	// tunnel and egress interfaces, rewriting AARP Replies crossing toward egress so
	// remote Wi-Fi stations route AppleTalk via the proxy's MAC (jcs/atalk-proxy). It is
	// a standalone adapter component (no router socket — it moves raw L2 frames), so it
	// registers as a plain singleton.
	Register(bridge.Name, func(ctx *BuildContext) (component.Component, error) {
		sec := bridge.SectionFromModel(ctx.Model)
		if sec == nil || !sec.Enabled {
			return nil, nil // no section / disabled → nothing built
		}
		logger := ctx.Logger(bridge.Name)

		// Resolve the egress MAC: the configured value, or the zero MAC when unset
		// (the device-link builder falls back to the interface's own hardware address).
		egressMAC := [6]byte{}
		if sec.EgressMAC != "" {
			if mac, err := port.ParseMAC(sec.EgressMAC); err == nil {
				egressMAC = mac
			}
		}

		// Bind each named interface to a per-Start pcap opener via the injected NIC
		// opener (the same seam EtherTalk uses). A nil ctx.Opener (no NIC backend in
		// this build / a unit test) yields nil openers → the bridge comes up inert but
		// satisfies the lifecycle, the same graceful degradation as the inert ports.
		tunOpener := proxyAARPSideOpener(ctx, sec.TunnelInterface)
		egrOpener := proxyAARPSideOpener(ctx, sec.EgressInterface)

		return bridge.New(bridge.Name, tunOpener, egrOpener, egressMAC, logger), nil
	})
}

// proxyAARPSideOpener resolves one bridge side's interface name to a per-Start FrameLink
// opener over the injected NIC opener (pcap at the cmd edge). It resolves the name through
// the interface namespace (so a bridge side may point at a named [Interface]) and reuses
// the nicLinkOpener dispatch. Returns nil when no NIC backend is injected or the resolved
// interface is not a pcap-backed NIC — the bridge then comes up inert.
func proxyAARPSideOpener(ctx *BuildContext, ifaceName string) bridge.LinkOpener {
	if ctx.Opener == nil || ifaceName == "" {
		return nil
	}
	// Resolve the bare interface name against the [Interface] namespace: a declared
	// entry wins (its Kind/Backend), otherwise the name is a plain pcap NIC.
	iface := ctx.Model.ResolveInterface(config.InterfaceSection{Name: ifaceName})
	// The proxy-AARP bridge sides are not ports with a config Section, so they carry no
	// per-port capture (nil sec); a bridge-side capture would be its own config if wanted.
	open := nicLinkOpener(ctx, nil, iface)
	if open == nil {
		return nil
	}
	return func() (link.FrameLink, error) { return open() }
}
