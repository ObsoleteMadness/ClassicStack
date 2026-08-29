//go:build pcap

package main

import (
	"fmt"

	"github.com/ObsoleteMadness/ClassicStack/adapter/link/framing"
	"github.com/ObsoleteMadness/ClassicStack/adapter/link/pcap"
	"github.com/ObsoleteMadness/ClassicStack/core/hostinfo"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
	"github.com/ObsoleteMadness/ClassicStack/core/port/ethertalk"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

// openPcap builds the EtherTalk (raw Ethernet, via libpcap/Npcap) router
// member. iface is the NIC device name (e.g. "en0", "eth0", or an Npcap
// "\Device\NPF_{GUID}"). Requires the 'pcap' build tag (see transport_nopcap.go
// for the stub used otherwise) since it links libpcap/Npcap.
func openPcap(iface string, rtr router.Router, logger log.Logger) (router.RoutedPort, error) {
	if iface == "" {
		return nil, fmt.Errorf("pcap: -iface is required")
	}
	sec := &port.Section{
		SKey:        ethertalk.Name,
		IsEnabled:   true,
		Iface:       iface,
		SeedNetwork: seedNetwork,
		SeedZone:    seedZone,
	}

	mac, err := hostinfo.HardwareAddrForDevice(iface, nil)
	if err != nil {
		return nil, fmt.Errorf("pcap: resolve MAC for %q: %w", iface, err)
	}

	cfg := pcap.DefaultEtherTalkConfig(iface)
	open := func() (link.FrameLink, error) { return pcap.Open(cfg) }

	live := &framing.LiveAddr{}
	framer := &framing.EtherTalkAARP{
		SrcMAC:     mac[:],
		Addr:       live,
		SeedNetMin: seedNetwork,
		SeedNetMax: seedNetwork,
	}

	comp, err := ethertalk.NewInstanceFromOpener(sec, open, framer, rtr, logger)
	if err != nil {
		return nil, fmt.Errorf("pcap: %w", err)
	}
	p, ok := comp.(router.RoutedPort)
	if !ok {
		return nil, fmt.Errorf("pcap: built component does not satisfy router.RoutedPort")
	}
	if setter, ok := comp.(interface {
		SetAddress(network uint16, node uint8, netMin, netMax uint16)
	}); ok {
		framer.OnClaimed = func(network uint16, node uint8, netMin, netMax uint16) {
			setter.SetAddress(network, node, netMin, netMax)
		}
	}
	return p, nil
}
