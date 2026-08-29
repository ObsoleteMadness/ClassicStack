package main

import (
	"fmt"

	"github.com/ObsoleteMadness/ClassicStack/adapter/link/framing"
	"github.com/ObsoleteMadness/ClassicStack/adapter/link/ltoudp"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
	"github.com/ObsoleteMadness/ClassicStack/core/port/localtalk"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

// seedNetwork/seedZone are the tentative AppleTalk address range and zone name
// this node claims on its segment. With no RTMP/ZIP running (this example
// deliberately skips both — see the package doc comment), there is no seed
// router to learn a real range from, so the node claims within this fixed
// non-extended network directly. A production deployment normally inherits
// these from a seed router on the segment instead of hardcoding them.
const (
	seedNetwork uint16 = 0xFF00
	seedZone           = "Demo Zone"
)

// openLToUDP builds the LToUDP (LocalTalk-over-UDP-multicast) router member:
// the default, no-root-required transport. iface is the local IPv4 address to
// bind/join on ("" = every multicast-capable interface). This mirrors
// compose/registry/reg_localtalk.go's production wiring, minus the config
// Section plumbing an operator-configured deployment needs.
func openLToUDP(iface string, rtr router.Router, logger log.Logger) (router.RoutedPort, error) {
	sec := &port.Section{
		SKey:        localtalk.NameLToUDP,
		IsEnabled:   true,
		Iface:       iface,
		SeedNetwork: seedNetwork,
		SeedZone:    seedZone,
	}

	cfg := ltoudp.DefaultConfig(iface)
	cfg.Logger = logger
	open := func() (link.FrameLink, error) { return ltoudp.Open(cfg) }

	live := &framing.LiveAddr{}
	framer := &framing.LocalTalk{
		Addr: live,
		Live: live,
		// CalcChecksum stamps a DDP checksum on outbound long-header frames — optional
		// per spec, but on by every real ClassicStack deployment.
		CalcChecksum: true,
		EnableClaim:  true,
		// RespondToEnq: true because LToUDP is a shared *simulated* segment — a claimed
		// node must answer ENQ probes itself so new joiners see the address is taken
		// (spec/09 §"respondToEnq Flag"). A physical medium defends this in hardware.
		RespondToEnq: true,
		SeedNetwork:  seedNetwork,
		Logger:       logger,
	}

	comp, err := localtalk.NewInstanceFromOpener(sec, open, framer, rtr, logger)
	if err != nil {
		return nil, fmt.Errorf("ltoudp: %w", err)
	}
	p, ok := comp.(router.RoutedPort)
	if !ok {
		return nil, fmt.Errorf("ltoudp: built component does not satisfy router.RoutedPort")
	}
	// Late-bind the claim → SetAddress hook now that the port exists, so the router
	// learns the claimed node once the LLAP claim dance succeeds.
	if setter, ok := comp.(interface {
		SetAddress(network uint16, node uint8, netMin, netMax uint16)
	}); ok {
		framer.OnClaimed = func(network uint16, node uint8, netMin, netMax uint16) {
			setter.SetAddress(network, node, netMin, netMax)
		}
	}
	return p, nil
}
