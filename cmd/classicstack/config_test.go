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

[Bridge]
mode = "pcap"
device = "eth0"
hw_address = "DE:AD:BE:EF:CA:FE"
bridge_mode = "wifi"

[TashTalk]
port = "COM1"
seed_network = 12
seed_zone = "TashTalk Network"

[EtherTalk]
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

func TestLoadConfig_RejectsLegacyEtherTalkBridgeKeys(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "server.toml")
	content := `[Bridge]
mode = "pcap"

[EtherTalk]
backend = "pcap"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, _, err := loadConfigFromFile(cfgPath); err == nil {
		t.Fatal("expected error for legacy EtherTalk bridge keys")
	}
}

func TestLoadConfig_NetBIOSIdentityInheritedFromSMB(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "server.toml")
	content := `[NetBIOS]
enabled = true
server_name = "LEGACYNB"
workgroup = "LEGACYWG"

[SMB]
enabled = true
server_name = "MACHINE1"
workgroup = "GROUP1"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, _, err := loadConfigFromFile(cfgPath)
	if err != nil {
		t.Fatalf("loadConfigFromFile error: %v", err)
	}

	if cfg.SMBServerName != "MACHINE1" || cfg.SMBWorkgroup != "GROUP1" {
		t.Fatalf("unexpected SMB identity: server=%q workgroup=%q", cfg.SMBServerName, cfg.SMBWorkgroup)
	}
	if cfg.NetBIOSServerName != cfg.SMBServerName || cfg.NetBIOSWorkgroup != cfg.SMBWorkgroup {
		t.Fatalf("NetBIOS identity not inherited from SMB: netbios=(%q,%q) smb=(%q,%q)",
			cfg.NetBIOSServerName, cfg.NetBIOSWorkgroup, cfg.SMBServerName, cfg.SMBWorkgroup)
	}
}

func TestLoadConfig_SharedBridgeAndProtocolFilters(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "server.toml")
	content := `[Bridge]
mode = "pcap"
device = "eth99"
hw_address = "00:11:22:33:44:55"
bridge_mode = "wifi"

[MacIP]
enabled = true
filter = "arp or ip"

[EtherTalk]
filter = "ether proto 0x809b"

[IPX]
enabled = true
filter = "ipx"

[NetBEUI]
enabled = true
filter = "llc"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, _, err := loadConfigFromFile(cfgPath)
	if err != nil {
		t.Fatalf("loadConfigFromFile error: %v", err)
	}

	if cfg.Bridge.Mode != "pcap" || cfg.Bridge.Device != "eth99" || cfg.Bridge.HWAddress != "00:11:22:33:44:55" || cfg.Bridge.BridgeMode != "wifi" {
		t.Fatalf("unexpected bridge config: %#v", cfg.Bridge)
	}
	if cfg.EtherTalk.Backend != cfg.Bridge.Mode || cfg.EtherTalk.Device != cfg.Bridge.Device || cfg.EtherTalk.HWAddress != cfg.Bridge.HWAddress || cfg.EtherTalk.BridgeMode != cfg.Bridge.BridgeMode {
		t.Fatalf("EtherTalk did not sync from Bridge: bridge=%#v ethertalk=%#v", cfg.Bridge, cfg.EtherTalk)
	}
	if cfg.EtherTalk.Filter != "ether proto 0x809b" {
		t.Fatalf("unexpected EtherTalk filter: %q", cfg.EtherTalk.Filter)
	}
	if cfg.MacIPFilter != "arp or ip" || cfg.IPXFilter != "ipx" || cfg.NetBEUIFilter != "llc" {
		t.Fatalf("unexpected protocol filters: macip=%q ipx=%q netbeui=%q", cfg.MacIPFilter, cfg.IPXFilter, cfg.NetBEUIFilter)
	}
}

func TestFlagsToConfig_NetBIOSIdentityInheritedFromSMB(t *testing.T) {
	cfg := flagsToConfig(flagInputs{
		NetBIOSEnabled:    true,
		NetBIOSServerName: "LEGACYNB",
		NetBIOSWorkgroup:  "LEGACYWG",
		SMBEnabled:        true,
		SMBServerName:     "MACHINE2",
		SMBWorkgroup:      "GROUP2",
	})

	if cfg.SMBServerName != "MACHINE2" || cfg.SMBWorkgroup != "GROUP2" {
		t.Fatalf("unexpected SMB identity: server=%q workgroup=%q", cfg.SMBServerName, cfg.SMBWorkgroup)
	}
	if cfg.NetBIOSServerName != cfg.SMBServerName || cfg.NetBIOSWorkgroup != cfg.SMBWorkgroup {
		t.Fatalf("NetBIOS identity not inherited from SMB: netbios=(%q,%q) smb=(%q,%q)",
			cfg.NetBIOSServerName, cfg.NetBIOSWorkgroup, cfg.SMBServerName, cfg.SMBWorkgroup)
	}
}
