package main

import (
	"os"
	"path/filepath"
	"testing"
)

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

	cfg, _, err := loadConfigFromFile(cfgPath)
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

func TestLoadConfig_LoggingAndPortsSections(t *testing.T) {
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

[Logging]
level = "debug"
parse_packets = true
log_traffic = true
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, _, err := loadConfigFromFile(cfgPath)
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
}
