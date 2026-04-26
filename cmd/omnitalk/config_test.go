package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pgodw/omnitalk/service/afp"
)

func TestLoadConfig_ParsesSections(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "server.toml")
	content := `[LToUdp]
enabled = true
interface = "192.168.0.103"
seed_network = 11
seed_zone = "LToUDP Network"

[TashTalk]
port = "COM1"
seed_network = 12
seed_zone = "TashTalk Network"

[EtherTalk]
backend = "pcap"
device = "eth0"
hw_address = "DE:AD:BE:EF:CA:FE"
bridge_mode = "wifi"
bridge_host_mac = "AA:BB:CC:DD:EE:FF"
seed_network_min = 3
seed_network_max = 9
seed_zone = "EtherTalk Network"

[MacIP]
enabled = true
mode = "nat"
nameserver = "1.1.1.1"
nat_subnet = "10.1.0.0/24"
nat_gw = "10.1.0.1"
ip_gateway = "192.168.0.1"
dhcp_relay = true
lease_file = "leases.txt"
zone = "MacIP Zone"

[AFP]
enabled = true
name = "OmniTalk"
zone = "EtherTalk Network"
protocols = "ddp,tcp"
binding = ":548"
extension_map = "extmap.conf"
cnid_backend = "memory"
use_decomposed_names = true

[AFP.Volumes.Main]
name = "Main"
path = 'C:\Mac'
appledouble_mode = "legacy"

[Logging]
level = "debug"
parse_packets = true
log_traffic = true
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := loadConfigFromFile(cfgPath)
	if err != nil {
		t.Fatalf("loadConfigFromFile error: %v", err)
	}

	if cfg.LogLevel != "debug" || !cfg.LogTraffic || !cfg.ParsePackets {
		t.Fatalf("unexpected logging config: %#v", cfg)
	}
	if cfg.LToUDP.Interface != "192.168.0.103" || cfg.LToUDP.SeedNetwork != 11 || cfg.TashTalk.Port != "COM1" {
		t.Fatalf("unexpected LocalTalk/TashTalk config: %#v", cfg)
	}
	if cfg.EtherTalk.Device != "eth0" || cfg.EtherTalk.SeedNetworkMax != 9 {
		t.Fatalf("unexpected EtherTalk config: %#v", cfg)
	}
	if cfg.EtherTalk.Backend != "pcap" {
		t.Fatalf("unexpected EtherTalk backend: %q", cfg.EtherTalk.Backend)
	}
	if cfg.EtherTalk.BridgeMode != "wifi" || cfg.EtherTalk.BridgeHostMAC != "AA:BB:CC:DD:EE:FF" {
		t.Fatalf("unexpected EtherTalk bridge config: %#v", cfg)
	}
	if !cfg.MacIPEnabled || !cfg.MacIPNAT || cfg.MacIPGWIP != "10.1.0.1" || cfg.MacIPGatewayIP != "192.168.0.1" || cfg.MacIPNameserver != "1.1.1.1" {
		t.Fatalf("unexpected MacIP config: %#v", cfg)
	}
	if cfg.AFP.ExtensionMap != filepath.Join(dir, "extmap.conf") {
		t.Fatalf("AFP.ExtensionMap = %q, want %q", cfg.AFP.ExtensionMap, filepath.Join(dir, "extmap.conf"))
	}
	if cfg.AFP.CNIDBackend != "memory" {
		t.Fatalf("AFP.CNIDBackend = %q, want %q", cfg.AFP.CNIDBackend, "memory")
	}
	if !cfg.AFP.UseDecomposedNames {
		t.Fatal("AFP.UseDecomposedNames = false, want true")
	}
	vols, err := cfg.AFP.ResolvedVolumes()
	if err != nil {
		t.Fatalf("ResolvedVolumes: %v", err)
	}
	if len(vols) != 1 || vols[0].Path != `C:\Mac` {
		t.Fatalf("unexpected AFP volumes: %#v", vols)
	}
	if vols[0].AppleDoubleMode != afp.AppleDoubleModeLegacy {
		t.Fatalf("expected volume to have legacy AppleDouble mode, got %q", vols[0].AppleDoubleMode)
	}
}

func TestLoadConfig_BlankNatGatewayKeepsDefault(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "server.toml")
	content := `[MacIP]
enabled = true
mode = "nat"
nat_subnet = ""
nat_gw = ""
ip_gateway = "192.168.0.1"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := loadConfigFromFile(cfgPath)
	if err != nil {
		t.Fatalf("loadConfigFromFile error: %v", err)
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

func TestLoadConfig_PerVolumeAppleDoubleMode(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "server.toml")
	content := `[AFP.Volumes.Modern]
name = "Modern"
path = "/tmp/modern"
appledouble_mode = "modern"

[AFP.Volumes.Legacy]
name = "Legacy"
path = "/tmp/legacy"
appledouble_mode = "legacy"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := loadConfigFromFile(cfgPath)
	if err != nil {
		t.Fatalf("loadConfigFromFile error: %v", err)
	}

	vols, err := cfg.AFP.ResolvedVolumes()
	if err != nil {
		t.Fatalf("ResolvedVolumes: %v", err)
	}
	if len(vols) != 2 {
		t.Fatalf("expected 2 volumes, got %d", len(vols))
	}

	volsByName := make(map[string]afp.VolumeConfig)
	for _, v := range vols {
		volsByName[v.Name] = v
	}

	if volsByName["Modern"].AppleDoubleMode != afp.AppleDoubleModeModern {
		t.Fatalf("Modern AppleDoubleMode = %q", volsByName["Modern"].AppleDoubleMode)
	}
	if volsByName["Legacy"].AppleDoubleMode != afp.AppleDoubleModeLegacy {
		t.Fatalf("Legacy AppleDoubleMode = %q", volsByName["Legacy"].AppleDoubleMode)
	}
}

func TestLoadConfig_PerVolumeFSType(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "server.toml")
	content := `[AFP.Volumes.Local]
name = "Local"
path = 'C:\Mac\Local'
fs_type = "local_fs"

[AFP.Volumes.Garden]
name = "Garden"
path = 'C:\Mac\Garden'
fs_type = "macgarden"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := loadConfigFromFile(cfgPath)
	if err != nil {
		t.Fatalf("loadConfigFromFile error: %v", err)
	}
	vols, err := cfg.AFP.ResolvedVolumes()
	if err != nil {
		t.Fatalf("ResolvedVolumes: %v", err)
	}
	if len(vols) != 2 {
		t.Fatalf("expected 2 volumes, got %d", len(vols))
	}
	byName := map[string]afp.VolumeConfig{}
	for _, v := range vols {
		byName[v.Name] = v
	}
	if byName["Local"].FSType != afp.FSTypeLocalFS {
		t.Fatalf("Local fs_type = %q", byName["Local"].FSType)
	}
	if byName["Garden"].FSType != afp.FSTypeMacGarden {
		t.Fatalf("Garden fs_type = %q", byName["Garden"].FSType)
	}
}

func TestLoadConfig_InvalidFSType(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "server.toml")
	content := `[AFP.Volumes.Bad]
name = "Bad"
path = 'C:\Mac\Bad'
fs_type = "bananas"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := loadConfigFromFile(cfgPath); err == nil {
		t.Fatal("expected invalid fs_type error")
	}
}

func TestLoadConfig_MacGardenWithoutPath(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "server.toml")
	content := `[AFP.Volumes.MacGarden]
name = "Mac Garden"
fs_type = "macgarden"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := loadConfigFromFile(cfgPath)
	if err != nil {
		t.Fatalf("loadConfigFromFile error: %v", err)
	}
	vols, err := cfg.AFP.ResolvedVolumes()
	if err != nil {
		t.Fatalf("ResolvedVolumes: %v", err)
	}
	if len(vols) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(vols))
	}
	if vols[0].FSType != afp.FSTypeMacGarden {
		t.Fatalf("fs_type = %q", vols[0].FSType)
	}
	if got, want := filepath.ToSlash(vols[0].Path), ".macgarden/Mac_Garden"; got != want {
		t.Fatalf("generated path = %q, want %q", got, want)
	}
}

func TestLoadConfig_LocalFSWithoutPathStillFails(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "server.toml")
	content := `[AFP.Volumes.Local]
name = "Local"
fs_type = "local_fs"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := loadConfigFromFile(cfgPath); err == nil {
		t.Fatal("expected path required error for local_fs")
	}
}
