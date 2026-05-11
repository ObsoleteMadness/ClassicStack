package main

import (
	"fmt"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/pkg/hwaddr"
	"github.com/ObsoleteMadness/ClassicStack/port/rawlink"
)

type rawlinkProfile string

const (
	rawlinkProfileEtherTalk rawlinkProfile = "ethertalk"
	rawlinkProfileMacIP     rawlinkProfile = "macip"
	rawlinkProfileIPX       rawlinkProfile = "ipx"
	rawlinkProfileNetBEUI   rawlinkProfile = "netbeui"
)

func openRawlink(mode, device string, profile rawlinkProfile) (rawlink.RawLink, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "pcap"
	}
	device = strings.TrimSpace(device)
	if device == "" {
		return nil, fmt.Errorf("bridge device is required")
	}

	switch mode {
	case "pcap":
		cfg := rawlink.DefaultEtherTalkConfig(device)
		switch profile {
		case rawlinkProfileMacIP:
			cfg = rawlink.DefaultMacIPConfig(device)
		case rawlinkProfileIPX:
			cfg = rawlink.DefaultIPXConfig(device)
		case rawlinkProfileNetBEUI:
			cfg = rawlink.DefaultNetBEUIConfig(device)
		}
		return rawlink.OpenPcap(cfg)
	case "tap", "tun":
		return rawlink.OpenTAP(device)
	default:
		return nil, fmt.Errorf("unsupported bridge mode %q (want pcap, tap, or tun)", mode)
	}
}

func applyRawlinkFilter(link rawlink.RawLink, mode, iface, overrideExpr, defaultExpr, protocol string) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "pcap" {
		if strings.TrimSpace(overrideExpr) != "" {
			netlog.Warn("[MAIN][%s] ignoring filter override in non-pcap bridge mode %q", protocol, mode)
		}
		return
	}

	filterExpr := strings.TrimSpace(overrideExpr)
	if filterExpr == "" {
		filterExpr = strings.TrimSpace(defaultExpr)
	}
	if filterExpr == "" {
		return
	}

	fl, ok := link.(rawlink.FilterableLink)
	if !ok {
		netlog.Warn("[MAIN][%s] rawlink backend on %s does not support filter programming", protocol, iface)
		return
	}
	if err := fl.SetFilter(filterExpr); err != nil {
		netlog.Warn("[MAIN][%s] could not set BPF filter on %s: %v", protocol, iface, err)
	}
}

func applyRawlinkBridgeFrameMode(link rawlink.RawLink, bridgeMode, frameMode, iface, bridgeHWAddr, protocol string) rawlink.RawLink {
	bridgeMode = strings.ToLower(strings.TrimSpace(bridgeMode))
	if bridgeMode != "pcap" {
		return link
	}

	virtual, err := hwaddr.ParseEthernet(strings.TrimSpace(bridgeHWAddr))
	if err != nil {
		netlog.Warn("[MAIN][%s] shared bridge hw_address is invalid, skipping bridge adapter: %v", protocol, err)
		return link
	}

	hostMAC := virtual.HardwareAddr()
	if detected, ok := rawlink.DetectHostMACForPcapInterface(iface); ok {
		parsed, err := hwaddr.ParseEthernet(detected)
		if err == nil {
			hostMAC = parsed.HardwareAddr()
		}
	}

	wrapped, err := rawlink.WrapWithBridgeMode(link, rawlink.BridgeLinkOptions{
		Mode:       frameMode,
		HostMAC:    hostMAC,
		VirtualMAC: virtual.HardwareAddr(),
	})
	if err != nil {
		netlog.Warn("[MAIN][%s] could not enable shared bridge frame adapter on %s: %v", protocol, iface, err)
		return link
	}
	if wrapped != link {
		netlog.Info("[MAIN][%s] shared rawlink bridge adapter active on %s (mode=%s)", protocol, iface, strings.TrimSpace(frameMode))
	}
	return wrapped
}
