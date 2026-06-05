package app

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/config"
)

// TestResolveProtocolInterface_BridgeInheritance verifies the Bridge vs Custom
// model: a protocol with no Custom interface inherits the shared Bridge; the
// legacy scalar interface string overrides only the device.
func TestResolveProtocolInterface_BridgeInheritance(t *testing.T) {
	bridge := BridgeConfig{Mode: "pcap", Device: "br0", HWAddress: "aa:bb", BridgeMode: "auto"}

	// No custom, no scalar iface -> exactly the bridge.
	if got := resolveProtocolInterface(bridge, nil, ""); got != bridge {
		t.Fatalf("inherit: got %+v, want %+v", got, bridge)
	}

	// Scalar iface overrides only the device.
	got := resolveProtocolInterface(bridge, nil, "eth9")
	want := bridge
	want.Device = "eth9"
	if got != want {
		t.Fatalf("scalar override: got %+v, want %+v", got, want)
	}
}

// TestResolveProtocolInterface_Custom verifies a Custom interface overrides the
// bridge field-by-field, with the scalar iface as device fallback.
func TestResolveProtocolInterface_Custom(t *testing.T) {
	bridge := BridgeConfig{Mode: "pcap", Device: "br0", HWAddress: "aa:bb", BridgeMode: "auto"}

	got := resolveProtocolInterface(bridge, &config.InterfaceModel{
		Mode:       "tap",
		Device:     "tap0",
		HWAddress:  "cc:dd",
		BridgeMode: "ethernet",
	}, "")
	want := BridgeConfig{Mode: "tap", Device: "tap0", HWAddress: "cc:dd", BridgeMode: "ethernet"}
	if got != want {
		t.Fatalf("custom: got %+v, want %+v", got, want)
	}

	// Empty custom fields fall back to bridge; empty Custom.Device falls back
	// to the scalar iface.
	got = resolveProtocolInterface(bridge, &config.InterfaceModel{Mode: "tun"}, "eth5")
	want = BridgeConfig{Mode: "tun", Device: "eth5", HWAddress: "aa:bb", BridgeMode: "auto"}
	if got != want {
		t.Fatalf("custom partial: got %+v, want %+v", got, want)
	}
}

// TestInterfaceRoundTrip verifies a Model with a Custom IPX interface survives
// appConfigFromModel -> modelFromAppConfig, and that a Bridge-only protocol
// stays Custom-free (clean config).
func TestInterfaceRoundTrip(t *testing.T) {
	m := config.Defaults()
	m.Bridge = config.InterfaceModel{Mode: "pcap", Device: "br0", HWAddress: "aa:bb", BridgeMode: "auto"}
	m.IPX.Enabled = true
	m.IPX.Custom = &config.InterfaceModel{Mode: "pcap", Device: "ipx0", BridgeMode: "wifi"}
	m.NetBEUI.Enabled = true // Bridge-inheriting (no Custom)

	cfg, err := appConfigFromModel(m)
	if err != nil {
		t.Fatalf("appConfigFromModel: %v", err)
	}
	if cfg.IPXBridge.Device != "ipx0" || cfg.IPXBridge.BridgeMode != "wifi" {
		t.Fatalf("IPX resolved interface = %+v, want device ipx0 / bridge_mode wifi", cfg.IPXBridge)
	}
	if cfg.NetBEUIBridge.Device != "br0" {
		t.Fatalf("NetBEUI should inherit bridge device br0, got %q", cfg.NetBEUIBridge.Device)
	}

	back := modelFromAppConfig(cfg)
	if back.IPX.Custom == nil {
		t.Fatal("round-trip lost IPX.Custom")
	}
	if back.IPX.Custom.Device != "ipx0" {
		t.Fatalf("round-trip IPX.Custom.Device = %q, want ipx0", back.IPX.Custom.Device)
	}
	if back.NetBEUI.Custom != nil {
		t.Fatalf("Bridge-inheriting NetBEUI should have no Custom, got %+v", back.NetBEUI.Custom)
	}
}
