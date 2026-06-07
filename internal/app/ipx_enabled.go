//go:build ipx || all

package app

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/capture"
	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/pkg/hwaddr"
	"github.com/ObsoleteMadness/ClassicStack/port"
	"github.com/ObsoleteMadness/ClassicStack/port/ipx"
	"github.com/ObsoleteMadness/ClassicStack/port/rawlink"
	routeripx "github.com/ObsoleteMadness/ClassicStack/router/ipx"
	ipxsvc "github.com/ObsoleteMadness/ClassicStack/service/ipx"
)

type ipxHookEnabled struct {
	router routeripx.Router
	port   ipx.Port
	rip    *ipxsvc.RIPService
	sap    *ipxsvc.SAPService

	// capturePath/captureSnaplen describe the optional frame-capture sink.
	// The sink is opened on each Start and closed on each Stop so a
	// UI-driven restart reopens it alongside the port's fresh rawlink.
	capturePath    string
	captureSnaplen uint32
	sink           *capture.PcapSink
}

func (h *ipxHookEnabled) Router() routeripx.Router { return h.router }
func (h *ipxHookEnabled) SAP() *ipxsvc.SAPService  { return h.sap }

// SetTrafficObserver forwards traffic metering to the underlying IPX port when
// it supports it, so the supervisor can publish per-port throughput
// (port.TrafficMetered).
func (h *ipxHookEnabled) SetTrafficObserver(obs port.TrafficObserver) {
	if tm, ok := h.port.(port.TrafficMetered); ok {
		tm.SetTrafficObserver(obs)
	}
}

func (h *ipxHookEnabled) Start(ctx context.Context) error {
	if h.port != nil {
		// (Re)open the capture sink before the port starts reading so no
		// frames are missed between Start and the first write.
		if h.capturePath != "" && h.sink == nil {
			sink, err := capture.NewPcapSink(h.capturePath, capture.LinkTypeEthernet, h.captureSnaplen)
			if err != nil {
				return fmt.Errorf("opening IPX capture sink %q: %w", h.capturePath, err)
			}
			h.sink = sink
			h.port.SetCaptureSink(sink)
			netlog.Info("[CAPTURE] IPX frames -> %s", h.capturePath)
		}
		if err := h.port.Start(); err != nil {
			return err
		}
	}
	if err := h.rip.Start(ctx); err != nil {
		return err
	}
	if err := h.sap.Start(ctx); err != nil {
		return err
	}
	netlog.Info("[MAIN][IPX] router up; RIP+SAP active")
	return nil
}

func (h *ipxHookEnabled) Stop() error {
	if h.rip != nil {
		_ = h.rip.Stop()
	}
	if h.sap != nil {
		_ = h.sap.Stop()
	}
	if h.port != nil {
		_ = h.port.Stop()
	}
	if h.sink != nil {
		_ = h.sink.Close()
		h.sink = nil
	}
	return nil
}

func wireIPX(cfg IPXConfig) (IPXHook, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	router := routeripx.NewRouter()
	hook := &ipxHookEnabled{
		router: router,
		rip:    ipxsvc.NewRIPService(router),
		sap:    ipxsvc.NewSAPService(router),
	}

	network, err := parseIPXNetwork(cfg.InternalNetwork)
	if err != nil {
		return nil, fmt.Errorf("parsing -ipx-internal-network: %w", err)
	}

	// openLink lazily produces a rawlink. For a configured interface it
	// opens a fresh libpcap handle on every call so the port can be stopped
	// and restarted from the UI: each Stop frees the C handle and each Start
	// reopens the interface. A pre-built cfg.Rawlink (tests, in-process
	// transports) is reused as-is. A nil factory means "no link configured".
	var openLink ipx.LinkFactory
	switch {
	case cfg.Rawlink != nil:
		prebuilt := cfg.Rawlink
		openLink = func() (rawlink.RawLink, error) { return prebuilt, nil }
	case strings.TrimSpace(cfg.Interface) != "":
		openLink = func() (rawlink.RawLink, error) {
			opened, err := openRawlink(cfg.BridgeMode, cfg.Interface, rawlinkProfileIPX)
			if err != nil {
				return nil, fmt.Errorf("opening IPX rawlink on %q: %w", cfg.Interface, err)
			}
			link := applyRawlinkBridgeFrameMode(opened, cfg.BridgeMode, cfg.BridgeFrameMode, cfg.Interface, cfg.BridgeHWAddress, "IPX")
			applyRawlinkFilter(link, cfg.BridgeMode, cfg.Interface, cfg.Filter, "ipx", "IPX")
			return link, nil
		}
	}

	if openLink != nil {
		framing := parseIPXFraming(cfg.Framing)
		hook.port = ipx.NewPortWithLinkFactory(openLink, framing)
		// The sink itself is opened on each Start (see Start) so it is
		// reopened across UI restarts; here we just record its config.
		hook.capturePath = strings.TrimSpace(cfg.CapturePath)
		hook.captureSnaplen = cfg.CaptureSnaplen
		router.AddPort(hook.port)

		node, ok := resolveIPXNodeFromInterface(cfg.Interface)
		if !ok {
			if parsed, err := hwaddr.ParseEthernet(strings.TrimSpace(cfg.BridgeHWAddress)); err == nil {
				node = [6]byte(parsed)
				ok = true
			}
		}
		if !ok {
			netlog.Warn("[MAIN][IPX] could not resolve MAC for %q; node ID left zero", cfg.Interface)
		}
		router.SetIdentity(network, node)
		netlog.Info("[MAIN][IPX] iface=%s framing=%s network=%08x node=%s",
			cfg.Interface, cfg.Framing, networkUint32(network), formatNode(node))
	} else {
		// No interface: still set the network identity so any in-process
		// caller (tests, future loopback transport) sees a configured
		// network number.
		router.SetIdentity(network, [6]byte{})
		netlog.Warn("[MAIN][IPX] enabled but no -ipx-interface configured; IPX router idle")
	}

	return hook, nil
}

// parseIPXNetwork accepts an 8-hex-digit IPX network number with an
// optional `0x` prefix. Empty input returns the router's default
// (DefaultNetwork) so the operator does not have to pick a number for
// a single-segment deployment.
func parseIPXNetwork(s string) ([4]byte, error) {
	trimmed := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(s), "0x"))
	if trimmed == "" {
		return routeripx.DefaultNetwork, nil
	}
	if len(trimmed) != 8 {
		return [4]byte{}, fmt.Errorf("want 8 hex digits, got %d", len(trimmed))
	}
	b, err := hex.DecodeString(trimmed)
	if err != nil {
		return [4]byte{}, err
	}
	var out [4]byte
	copy(out[:], b)
	return out, nil
}

// resolveIPXNodeFromInterface reads the host interface MAC and returns
// it as a 6-byte IPX node ID. Returns (zero, false) when the MAC cannot
// be detected.
func resolveIPXNodeFromInterface(iface string) ([6]byte, bool) {
	mac, ok := rawlink.DetectHostMACForPcapInterface(iface)
	if !ok {
		return [6]byte{}, false
	}
	parsed, err := hwaddr.ParseEthernet(mac)
	if err != nil {
		return [6]byte{}, false
	}
	return [6]byte(parsed), true
}

// networkUint32 renders a [4]byte network number as the big-endian
// uint32 the operator-facing logs and config expect.
func networkUint32(n [4]byte) uint32 {
	return uint32(n[0])<<24 | uint32(n[1])<<16 | uint32(n[2])<<8 | uint32(n[3])
}

// formatNode renders a 6-byte node ID as colon-separated hex.
func formatNode(n [6]byte) string {
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", n[0], n[1], n[2], n[3], n[4], n[5])
}

// parseIPXFraming maps the operator-facing framing name to the wire
// constant. Unknown values fall back to Ethernet II with a warning.
func parseIPXFraming(name string) ipx.Framing {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "ethernet_ii", "ethernet-ii", "ethernetii":
		return ipx.FramingEthernetII
	case "raw_802_3", "raw-802-3", "raw802.3":
		return ipx.FramingRaw8023
	case "llc", "802.2":
		return ipx.FramingLLC
	case "snap":
		return ipx.FramingSNAP
	default:
		netlog.Warn("[MAIN][IPX] unknown framing %q; defaulting to ethernet_ii", name)
		return ipx.FramingEthernetII
	}
}
