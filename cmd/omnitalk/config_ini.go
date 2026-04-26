package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/knadh/koanf/v2"

	"github.com/pgodw/omnitalk/config"
	"github.com/pgodw/omnitalk/service/afp"
)

// fileConfig is the cmd-local view of the config file. AFP owns its own
// schema (afp.Config); other sections are still flattened here pending
// the same per-service treatment.
type fileConfig struct {
	LogLevel     string
	LogTraffic   bool
	ParsePackets bool
	ParseOutput  string

	LToUDPEnabled     bool
	LToUDPInterface   string
	LToUDPSeedNetwork uint
	LToUDPSeedZone    string

	TashTalkPort        string
	TashTalkSeedNetwork uint
	TashTalkSeedZone    string

	EtherTalkDevice         string
	EtherTalkBackend        string
	EtherTalkHWAddr         string
	EtherTalkBridgeMode     string
	EtherTalkBridgeHostMAC  string
	EtherTalkSeedNetworkMin uint
	EtherTalkSeedNetworkMax uint
	EtherTalkSeedZone       string

	MacIPEnabled    bool
	MacIPNAT        bool
	MacIPSubnet     string
	MacIPGWIP       string
	MacIPNameserver string
	MacIPGatewayIP  string
	MacIPDHCPRelay  bool
	MacIPLeaseFile  string
	MacIPZone       string

	AFP afp.Config
}

func defaultFileConfig() fileConfig {
	return fileConfig{
		LogLevel: "info",

		LToUDPEnabled:     true,
		LToUDPInterface:   "0.0.0.0",
		LToUDPSeedNetwork: 1,
		LToUDPSeedZone:    "LToUDP Network",

		TashTalkSeedNetwork: 2,
		TashTalkSeedZone:    "TashTalk Network",

		EtherTalkBackend:        "pcap",
		EtherTalkHWAddr:         "DE:AD:BE:EF:CA:FE",
		EtherTalkBridgeMode:     "auto",
		EtherTalkSeedNetworkMin: 3,
		EtherTalkSeedNetworkMax: 5,
		EtherTalkSeedZone:       "EtherTalk Network",

		MacIPSubnet: "192.168.100.0/24",

		AFP: afp.DefaultConfig(),
	}
}

func defaultMacGardenVolumePath(name string) string { return afp.DefaultMacGardenVolumePath(name) }

func loadConfigFromFile(path string) (fileConfig, error) {
	src, err := config.Load(path)
	if err != nil {
		return defaultFileConfig(), err
	}
	cfg, err := resolveFileConfig(src)
	if err != nil {
		return defaultFileConfig(), err
	}
	return cfg, nil
}

func resolveFileConfig(src config.Source) (fileConfig, error) {
	cfg := defaultFileConfig()
	k := src.K

	cfg.LToUDPEnabled = boolWithDefault(k, "LToUdp.enabled", cfg.LToUDPEnabled)
	cfg.LToUDPInterface = stringWithDefault(k, "LToUdp.interface", cfg.LToUDPInterface)
	cfg.LToUDPSeedNetwork = uintWithDefault(k, "LToUdp.seed_network", cfg.LToUDPSeedNetwork)
	cfg.LToUDPSeedZone = stringWithDefault(k, "LToUdp.seed_zone", cfg.LToUDPSeedZone)

	cfg.TashTalkPort = stringWithDefault(k, "TashTalk.port", cfg.TashTalkPort)
	cfg.TashTalkSeedNetwork = uintWithDefault(k, "TashTalk.seed_network", cfg.TashTalkSeedNetwork)
	cfg.TashTalkSeedZone = stringWithDefault(k, "TashTalk.seed_zone", cfg.TashTalkSeedZone)

	backend := strings.ToLower(stringWithDefault(k, "EtherTalk.backend", cfg.EtherTalkBackend))
	switch backend {
	case "", "pcap", "tap", "tun":
	default:
		return cfg, fmt.Errorf("[EtherTalk] backend must be blank, pcap, tap, or tun, got %q", backend)
	}
	cfg.EtherTalkBackend = backend
	cfg.EtherTalkDevice = stringWithDefault(k, "EtherTalk.device", cfg.EtherTalkDevice)
	if backend == "" {
		cfg.EtherTalkDevice = ""
	}
	cfg.EtherTalkHWAddr = stringWithDefault(k, "EtherTalk.hw_address", cfg.EtherTalkHWAddr)
	cfg.EtherTalkBridgeMode = stringWithDefault(k, "EtherTalk.bridge_mode", cfg.EtherTalkBridgeMode)
	cfg.EtherTalkBridgeHostMAC = stringWithDefault(k, "EtherTalk.bridge_host_mac", cfg.EtherTalkBridgeHostMAC)
	cfg.EtherTalkSeedNetworkMin = uintWithDefault(k, "EtherTalk.seed_network_min", cfg.EtherTalkSeedNetworkMin)
	cfg.EtherTalkSeedNetworkMax = uintWithDefault(k, "EtherTalk.seed_network_max", cfg.EtherTalkSeedNetworkMax)
	cfg.EtherTalkSeedZone = stringWithDefault(k, "EtherTalk.seed_zone", cfg.EtherTalkSeedZone)

	cfg.MacIPEnabled = boolWithDefault(k, "MacIP.enabled", cfg.MacIPEnabled)
	mode := strings.ToLower(stringWithDefault(k, "MacIP.mode", ""))
	switch mode {
	case "", "pcap":
		cfg.MacIPNAT = false
	case "nat":
		cfg.MacIPNAT = true
	default:
		return cfg, fmt.Errorf("[MacIP] mode must be pcap or nat, got %q", mode)
	}
	cfg.MacIPNameserver = stringWithDefault(k, "MacIP.nameserver", cfg.MacIPNameserver)
	cfg.MacIPSubnet = stringWithDefault(k, "MacIP.nat_subnet", cfg.MacIPSubnet)
	cfg.MacIPGWIP = stringWithDefault(k, "MacIP.nat_gw", cfg.MacIPGWIP)
	cfg.MacIPLeaseFile = stringWithDefault(k, "MacIP.lease_file", cfg.MacIPLeaseFile)
	cfg.MacIPGatewayIP = stringWithDefault(k, "MacIP.ip_gateway", cfg.MacIPGatewayIP)
	cfg.MacIPDHCPRelay = boolWithDefault(k, "MacIP.dhcp_relay", cfg.MacIPDHCPRelay)
	cfg.MacIPZone = stringWithDefault(k, "MacIP.zone", cfg.MacIPZone)

	if err := loadAFP(k, src.ConfigDir, &cfg.AFP); err != nil {
		return cfg, err
	}

	cfg.LogLevel = stringWithDefault(k, "Logging.level", cfg.LogLevel)
	cfg.ParsePackets = boolWithDefault(k, "Logging.parse_packets", cfg.ParsePackets)
	cfg.LogTraffic = boolWithDefault(k, "Logging.log_traffic", cfg.LogTraffic)
	cfg.ParseOutput = stringWithDefault(k, "Logging.parse_output", cfg.ParseOutput)

	return cfg, nil
}

// loadAFP unmarshals the [AFP] subtree onto an already-defaulted target,
// then runs the service's own validation. Defaults are seeded by the
// caller so unset keys preserve them rather than zeroing.
func loadAFP(k *koanf.Koanf, configDir string, target *afp.Config) error {
	if !k.Exists("AFP") {
		return nil
	}
	if err := k.UnmarshalWithConf("AFP", target, koanf.UnmarshalConf{Tag: "koanf"}); err != nil {
		return fmt.Errorf("[AFP] %w", err)
	}
	if target.ExtensionMap != "" && !filepath.IsAbs(target.ExtensionMap) && configDir != "" {
		target.ExtensionMap = filepath.Join(configDir, target.ExtensionMap)
	}
	if !target.Enabled {
		target.Volumes = nil
	}
	return target.Validate()
}

func stringWithDefault(k *koanf.Koanf, path, def string) string {
	if !k.Exists(path) {
		return def
	}
	v := strings.TrimSpace(k.String(path))
	if v == "" {
		return def
	}
	return v
}

func boolWithDefault(k *koanf.Koanf, path string, def bool) bool {
	if !k.Exists(path) {
		return def
	}
	return k.Bool(path)
}

func uintWithDefault(k *koanf.Koanf, path string, def uint) uint {
	if !k.Exists(path) {
		return def
	}
	v := k.Int(path)
	if v < 0 {
		return def
	}
	return uint(v)
}
