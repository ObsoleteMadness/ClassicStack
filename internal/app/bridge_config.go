package app

import (
	"fmt"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/port/ethertalk"
)

// BridgeConfig defines shared raw-link settings used by all Ethernet-like
// transports (EtherTalk, MacIP, IPX, NetBEUI).
type BridgeConfig struct {
	Mode       string `koanf:"mode"`
	Device     string `koanf:"device"`
	HWAddress  string `koanf:"hw_address"`
	BridgeMode string `koanf:"bridge_mode"`
}

// bridgeWithDevice returns a copy of base with Device overridden by iface when
// iface is non-empty. Used to apply a protocol's scalar interface override to
// the shared bridge in the CLI/flag path.
func bridgeWithDevice(base BridgeConfig, iface string) BridgeConfig {
	if strings.TrimSpace(iface) != "" {
		base.Device = iface
	}
	return base
}

func defaultBridgeConfig() BridgeConfig {
	et := ethertalk.DefaultConfig()
	return BridgeConfig{
		Mode:       et.Backend,
		Device:     et.Device,
		HWAddress:  et.HWAddress,
		BridgeMode: et.BridgeMode,
	}
}

func (c *BridgeConfig) Validate() error {
	switch strings.ToLower(strings.TrimSpace(c.Mode)) {
	case "", "pcap", "tap", "tun":
	default:
		return fmt.Errorf("bridge.mode must be blank, pcap, tap, or tun, got %q", c.Mode)
	}
	switch strings.ToLower(strings.TrimSpace(c.BridgeMode)) {
	case "", "auto", "ethernet", "wifi":
	default:
		return fmt.Errorf("bridge.bridge_mode must be auto, ethernet, or wifi, got %q", c.BridgeMode)
	}
	return nil
}
