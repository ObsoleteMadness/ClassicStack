package ethertalk

import (
	"fmt"
	"strings"
)

// Config is EtherTalk's user-facing configuration. Source-agnostic and
// populated via koanf tags by any caller that wires up a config source.
type Config struct {
	// Backend selects the link-layer driver: pcap (default), tap, tun,
	// or "" to disable EtherTalk entirely.
	Backend string `koanf:"backend"`
	// Device is the network interface or pcap device name.
	Device string `koanf:"device"`
	// HWAddress is the EtherTalk router MAC (6-byte EUI-48).
	HWAddress string `koanf:"hw_address"`
	// BridgeMode controls the bridge shim: auto, ethernet, or wifi.
	BridgeMode string `koanf:"bridge_mode"`
	// BridgeHostMAC is the host adapter's own MAC, used by the Wi-Fi
	// bridge shim. Defaults to HWAddress when blank.
	BridgeHostMAC  string `koanf:"bridge_host_mac"`
	SeedNetworkMin uint   `koanf:"seed_network_min"`
	SeedNetworkMax uint   `koanf:"seed_network_max"`
	SeedZone       string `koanf:"seed_zone"`
	DesiredNetwork uint   `koanf:"desired_network"`
	DesiredNode    uint   `koanf:"desired_node"`
}

// DefaultConfig returns EtherTalk's built-in defaults.
func DefaultConfig() Config {
	return Config{
		Backend:        "pcap",
		HWAddress:      "DE:AD:BE:EF:CA:FE",
		BridgeMode:     "auto",
		SeedNetworkMin: 3,
		SeedNetworkMax: 5,
		SeedZone:       "EtherTalk Network",
		DesiredNetwork: 3,
		DesiredNode:    253,
	}
}

// Validate checks the config for logical consistency. It does not check
// that the device is reachable — that's a runtime concern.
func (c *Config) Validate() error {
	switch strings.ToLower(strings.TrimSpace(c.Backend)) {
	case "", "pcap", "tap", "tun":
	default:
		return fmt.Errorf("EtherTalk.backend must be blank, pcap, tap, or tun, got %q", c.Backend)
	}
	if c.Backend != "" && c.SeedNetworkMin > c.SeedNetworkMax {
		return fmt.Errorf("EtherTalk.seed_network_min (%d) must be <= seed_network_max (%d)", c.SeedNetworkMin, c.SeedNetworkMax)
	}
	switch strings.ToLower(strings.TrimSpace(c.BridgeMode)) {
	case "", "auto", "ethernet", "wifi":
	default:
		return fmt.Errorf("EtherTalk.bridge_mode must be auto, ethernet, or wifi, got %q", c.BridgeMode)
	}
	return nil
}

// Enabled reports whether the EtherTalk port should be created at all.
func (c *Config) Enabled() bool {
	return strings.TrimSpace(c.Backend) != "" && strings.TrimSpace(c.Device) != ""
}
