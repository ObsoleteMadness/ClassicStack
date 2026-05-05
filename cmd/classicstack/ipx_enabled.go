//go:build ipx || all

package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/pkg/hwaddr"
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
}

func (h *ipxHookEnabled) Router() routeripx.Router { return h.router }

func (h *ipxHookEnabled) Start(ctx context.Context) error {
	if h.port != nil {
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
	netlog.Info("[MAIN][IPX] router up; RIP+SAP registered (stub)")
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

	link := cfg.Rawlink
	if link == nil && strings.TrimSpace(cfg.Interface) != "" {
		opened, err := rawlink.OpenPcap(rawlink.DefaultIPXConfig(cfg.Interface))
		if err != nil {
			return nil, fmt.Errorf("opening IPX rawlink on %q: %w", cfg.Interface, err)
		}
		link = opened
	}
	if link != nil {
		framing := parseIPXFraming(cfg.Framing)
		hook.port = ipx.NewPortWithFraming(link, framing)
		router.AddPort(hook.port)

		node, ok := resolveIPXNodeFromInterface(cfg.Interface)
		if !ok {
			netlog.Warn("[MAIN][IPX] could not auto-detect MAC for %q; node ID left zero", cfg.Interface)
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
