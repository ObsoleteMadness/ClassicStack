//go:build netbeui || all

package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/capture"
	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/pkg/hwaddr"
	"github.com/ObsoleteMadness/ClassicStack/port"
	"github.com/ObsoleteMadness/ClassicStack/port/netbeui"
	"github.com/ObsoleteMadness/ClassicStack/port/rawlink"
)

type netbeuiHookEnabled struct {
	port netbeui.Port
	mac  [6]byte

	// capture sink config; reopened on each Start so a UI restart reopens
	// it alongside the port's fresh rawlink.
	capturePath    string
	captureSnaplen uint32
	sink           *capture.PcapSink
}

// SetTrafficObserver forwards traffic metering to the underlying NetBEUI port
// when it supports it, so the supervisor can publish per-port throughput
// (port.TrafficMetered).
func (h *netbeuiHookEnabled) SetTrafficObserver(obs port.TrafficObserver) {
	if tm, ok := h.port.(port.TrafficMetered); ok {
		tm.SetTrafficObserver(obs)
	}
}

func (h *netbeuiHookEnabled) Start(_ context.Context) error {
	if h.port != nil {
		if h.capturePath != "" && h.sink == nil {
			sink, err := capture.NewPcapSink(h.capturePath, capture.LinkTypeEthernet, h.captureSnaplen)
			if err != nil {
				return fmt.Errorf("opening NetBEUI capture sink %q: %w", h.capturePath, err)
			}
			h.sink = sink
			h.port.SetCaptureSink(sink)
			netlog.Info("[CAPTURE] NetBEUI frames -> %s", h.capturePath)
		}
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
	// openLink opens a fresh rawlink per Start (see the IPX hook) so the
	// port can be stopped and restarted from the UI. A pre-built
	// cfg.Rawlink is reused as-is.
	var openLink netbeui.LinkFactory
	switch {
	case cfg.Rawlink != nil:
		prebuilt := cfg.Rawlink
		openLink = func() (rawlink.RawLink, error) { return prebuilt, nil }
	case strings.TrimSpace(cfg.Interface) != "":
		openLink = func() (rawlink.RawLink, error) {
			opened, err := openRawlink(cfg.BridgeMode, cfg.Interface, rawlinkProfileNetBEUI)
			if err != nil {
				return nil, fmt.Errorf("opening NetBEUI rawlink on %q: %w", cfg.Interface, err)
			}
			link := applyRawlinkBridgeFrameMode(opened, cfg.BridgeMode, cfg.BridgeFrameMode, cfg.Interface, cfg.BridgeHWAddress, "NetBEUI")
			applyRawlinkFilter(link, cfg.BridgeMode, cfg.Interface, cfg.Filter, "llc", "NetBEUI")
			return link, nil
		}
	}
	if openLink == nil {
		netlog.Warn("[MAIN][NetBEUI] enabled but no -netbeui-interface configured; NetBEUI idle")
		return &netbeuiHookEnabled{}, nil
	}
	netlog.Info("[MAIN][NetBEUI] pcap interface=%s", cfg.Interface)
	p := netbeui.NewPortWithLinkFactory(openLink)
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

	hook := &netbeuiHookEnabled{
		port:           p,
		mac:            mac,
		capturePath:    strings.TrimSpace(cfg.CapturePath),
		captureSnaplen: cfg.CaptureSnaplen,
	}
	return hook, nil
}
