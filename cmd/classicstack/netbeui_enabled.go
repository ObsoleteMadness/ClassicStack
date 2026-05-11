//go:build netbeui || all

package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/capture"
	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/pkg/hwaddr"
	"github.com/ObsoleteMadness/ClassicStack/port/netbeui"
	"github.com/ObsoleteMadness/ClassicStack/port/rawlink"
)

type netbeuiHookEnabled struct {
	port netbeui.Port
	mac  [6]byte
	sink *capture.PcapSink
}

func (h *netbeuiHookEnabled) Start(_ context.Context) error {
	if h.port != nil {
		if err := h.port.Start(); err != nil {
			return err
		}
	}
	netlog.Info("[MAIN][NetBEUI] port up")
	return nil
}
func (h *netbeuiHookEnabled) Stop() error {
	if h.port != nil {
		_ = h.port.Stop()
	}
	if h.sink != nil {
		_ = h.sink.Close()
		h.sink = nil
	}
	return nil
}
func (h *netbeuiHookEnabled) Port() netbeui.Port { return h.port }
func (h *netbeuiHookEnabled) MAC() [6]byte       { return h.mac }

func wireNetBEUI(cfg NetBEUIConfig) (NetBEUIHook, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	link := cfg.Rawlink
	if link == nil && strings.TrimSpace(cfg.Interface) != "" {
		opened, err := openRawlink(cfg.BridgeMode, cfg.Interface, rawlinkProfileNetBEUI)
		if err != nil {
			return nil, fmt.Errorf("opening NetBEUI rawlink on %q: %w", cfg.Interface, err)
		}
		link = applyRawlinkBridgeFrameMode(opened, cfg.BridgeMode, cfg.BridgeFrameMode, cfg.Interface, cfg.BridgeHWAddress, "NetBEUI")
		applyRawlinkFilter(link, cfg.BridgeMode, cfg.Interface, cfg.Filter, "llc", "NetBEUI")
	}
	if link == nil {
		netlog.Warn("[MAIN][NetBEUI] enabled but no -netbeui-interface configured; NetBEUI idle")
		return &netbeuiHookEnabled{}, nil
	}
	netlog.Info("[MAIN][NetBEUI] pcap interface=%s", cfg.Interface)
	p := netbeui.NewPort(link)
	var mac [6]byte
	if macStr, ok := rawlink.DetectHostMACForPcapInterface(cfg.Interface); ok {
		if parsed, err := hwaddr.ParseEthernet(macStr); err == nil {
			mac = [6]byte(parsed)
			p.SetSourceMAC(mac)
		}
	} else if parsed, err := hwaddr.ParseEthernet(strings.TrimSpace(cfg.BridgeHWAddress)); err == nil {
		mac = [6]byte(parsed)
		p.SetSourceMAC(mac)
	}

	hook := &netbeuiHookEnabled{port: p, mac: mac}

	if strings.TrimSpace(cfg.CapturePath) != "" {
		sink, err := capture.NewPcapSink(cfg.CapturePath, capture.LinkTypeEthernet, cfg.CaptureSnaplen)
		if err != nil {
			return nil, fmt.Errorf("opening NetBEUI capture sink %q: %w", cfg.CapturePath, err)
		}
		hook.sink = sink
		p.SetCaptureSink(sink)
		netlog.Info("[CAPTURE] NetBEUI frames -> %s", cfg.CapturePath)
	}

	return hook, nil
}
