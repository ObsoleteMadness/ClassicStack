package main

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/pgodw/omnitalk/go/service/afp"
)

func TestLoadConfigFromINI_ParsesSections(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "server.ini")
	content := `[LToUdp]
enabled = true
	interface = 192.168.0.103
seed_network = 11
seed_zone = "LToUDP Network"

[TashTalk]
port = COM1
seed_network = 12
seed_zone = "TashTalk Network"

[EtherTalk]
backend = pcap
device = "eth0"
hw_address = "DE:AD:BE:EF:CA:FE"
bridge_mode = wifi
bridge_host_mac = "AA:BB:CC:DD:EE:FF"
seed_network_min = 3
seed_network_max = 9
seed_zone = "EtherTalk Network"

[MacIP]
enabled = true
mode = nat
nameserver = 1.1.1.1
nat_subnet = 10.1.0.0/24
nat_gw = 10.1.0.1
ip_gateway = 192.168.0.1
dhcp_relay = true
lease_file = leases.txt
zone = "MacIP Zone"

[AFP]
enabled = true
name = "OmniTalk"
zone = "EtherTalk Network"
protocols = ddp,tcp
binding = ":548"
extension_map = "extmap.conf"

[Volumes.Main]
name = "Main"
path = "C:\Mac"
cnid_backend = memory
use_decomposed_names = true
fork_backend = AppleDouble
appledouble_mode = legacy

[Logging]
level = debug
parse_packets = true
log_traffic = true
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := loadConfigFromINI(cfgPath)
	if err != nil {
		t.Fatalf("loadConfigFromINI error: %v", err)
	}

	if cfg.LogLevel != "debug" || !cfg.LogTraffic || !cfg.ParsePackets {
		t.Fatalf("unexpected logging config: %#v", cfg)
	}
	if cfg.LToUDPInterface != "192.168.0.103" || cfg.LToUDPSeedNetwork != 11 || cfg.TashTalkPort != "COM1" {
		t.Fatalf("unexpected LocalTalk/TashTalk config: %#v", cfg)
	}
	if cfg.EtherTalkDevice != "eth0" || cfg.EtherTalkSeedNetworkMax != 9 {
		t.Fatalf("unexpected EtherTalk config: %#v", cfg)
	}
	if cfg.EtherTalkBackend != "pcap" {
		t.Fatalf("unexpected EtherTalk backend: %q", cfg.EtherTalkBackend)
	}
	if cfg.EtherTalkBridgeMode != "wifi" || cfg.EtherTalkBridgeHostMAC != "AA:BB:CC:DD:EE:FF" {
		t.Fatalf("unexpected EtherTalk bridge config: %#v", cfg)
	}
	if !cfg.MacIPEnabled || !cfg.MacIPNAT || cfg.MacIPGWIP != "10.1.0.1" || cfg.MacIPGatewayIP != "192.168.0.1" || cfg.MacIPNameserver != "1.1.1.1" {
		t.Fatalf("unexpected MacIP config: %#v", cfg)
	}
	if cfg.AFPExtensionMapPath != filepath.Join(dir, "extmap.conf") {
		t.Fatalf("AFPExtensionMapPath = %q, want %q", cfg.AFPExtensionMapPath, filepath.Join(dir, "extmap.conf"))
	}
	if len(cfg.AFPVolumes) != 1 || cfg.AFPVolumes[0].Path != "C:\\Mac" {
		t.Fatalf("unexpected AFP volumes: %#v", cfg.AFPVolumes)
	}
	if cfg.AFPVolumes[0].AppleDoubleMode != afp.AppleDoubleModeLegacy {
		t.Fatalf("expected volume to have legacy AppleDouble mode, got %q", cfg.AFPVolumes[0].AppleDoubleMode)
	}
}

func TestLoadConfigFromINI_ConflictingVolumeOptions(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "server.ini")
	content := `[Volumes.One]
name = "One"
path = "/tmp/one"
use_decomposed_names = true

[Volumes.Two]
name = "Two"
path = "/tmp/two"
use_decomposed_names = false
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := loadConfigFromINI(cfgPath); err == nil {
		t.Fatal("expected conflict error, got nil")
	}
}

func TestLoadConfigFromINI_BlankNatGatewayKeepsDefault(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "server.ini")
	content := `[MacIP]
enabled = true
mode = nat
	nat_subnet =
nat_gw =
ip_gateway = 192.168.0.1
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := loadConfigFromINI(cfgPath)
	if err != nil {
		t.Fatalf("loadConfigFromINI error: %v", err)
	}

	if cfg.MacIPGWIP != "" {
		t.Fatalf("MacIPGWIP = %q, want blank default", cfg.MacIPGWIP)
	}
	if cfg.MacIPSubnet != "192.168.100.0/24" {
		t.Fatalf("MacIPSubnet = %q, want default %q", cfg.MacIPSubnet, "192.168.100.0/24")
	}
	if cfg.MacIPGatewayIP != "192.168.0.1" {
		t.Fatalf("MacIPGatewayIP = %q, want %q", cfg.MacIPGatewayIP, "192.168.0.1")
	}
}

func TestResolveMacIPGatewayIP_PcapModeUsesUpstreamGateway(t *testing.T) {
	_, subnet, err := net.ParseCIDR("10.1.0.0/24")
	if err != nil {
		t.Fatalf("ParseCIDR: %v", err)
	}
	got := resolveMacIPGatewayIP("192.168.100.1", subnet, net.ParseIP("192.168.100.1"), false)
	if got == nil || got.String() != "192.168.100.1" {
		t.Fatalf("resolveMacIPGatewayIP pcap = %v, want 192.168.100.1", got)
	}
}

func TestResolveMacIPGatewayIP_NATModeUsesConfiguredOrSubnetDefault(t *testing.T) {
	_, subnet, err := net.ParseCIDR("10.1.0.0/24")
	if err != nil {
		t.Fatalf("ParseCIDR: %v", err)
	}
	configured := resolveMacIPGatewayIP("10.1.0.1", subnet, net.ParseIP("192.168.1.1"), true)
	if configured == nil || configured.String() != "10.1.0.1" {
		t.Fatalf("resolveMacIPGatewayIP configured = %v, want 10.1.0.1", configured)
	}

	fallback := resolveMacIPGatewayIP("", subnet, net.ParseIP("192.168.1.1"), true)
	if fallback == nil || fallback.String() != "10.1.0.1" {
		t.Fatalf("resolveMacIPGatewayIP fallback = %v, want 10.1.0.1", fallback)
	}
}

// TestLoadConfigFromINI_PerVolumeAppleDoubleMode verifies that two volumes in the
// same config file can independently specify different AppleDouble modes, and that
// each volume carries its own setting rather than a shared global one.
func TestLoadConfigFromINI_PerVolumeAppleDoubleMode(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "server.ini")
	content := `[Volumes.Modern]
name = "Modern"
path = "/tmp/modern"
appledouble_mode = modern

[Volumes.Legacy]
name = "Legacy"
path = "/tmp/legacy"
appledouble_mode = legacy
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := loadConfigFromINI(cfgPath)
	if err != nil {
		t.Fatalf("loadConfigFromINI error: %v", err)
	}

	if len(cfg.AFPVolumes) != 2 {
		t.Fatalf("expected 2 volumes, got %d", len(cfg.AFPVolumes))
	}

	// Find volumes by name regardless of parse order.
	volsByName := make(map[string]afp.VolumeConfig)
	for _, v := range cfg.AFPVolumes {
		volsByName[v.Name] = v
	}

	modernVol, ok := volsByName["Modern"]
	if !ok {
		t.Fatal("volume \"Modern\" not found")
	}
	if modernVol.AppleDoubleMode != afp.AppleDoubleModeModern {
		t.Fatalf("Modern volume AppleDoubleMode = %q, want %q", modernVol.AppleDoubleMode, afp.AppleDoubleModeModern)
	}

	legacyVol, ok := volsByName["Legacy"]
	if !ok {
		t.Fatal("volume \"Legacy\" not found")
	}
	if legacyVol.AppleDoubleMode != afp.AppleDoubleModeLegacy {
		t.Fatalf("Legacy volume AppleDoubleMode = %q, want %q", legacyVol.AppleDoubleMode, afp.AppleDoubleModeLegacy)
	}
}
