package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/knadh/koanf/v2"

	"github.com/pgodw/omnitalk/config"
	"github.com/pgodw/omnitalk/service/afp"
)

// fileConfig is the cmd-local view of the config file. It's populated by
// reading sections out of a koanf.Koanf instance. The config package
// itself owns no schema knowledge; each section's keys are resolved here
// (or, for AFP volumes, in service/afp).
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

	AFPEnabled             bool
	AFPServerName          string
	AFPZone                string
	AFPProtocols           string
	AFPTCPBinding          string
	AFPExtensionMapPath    string
	AFPDecomposedFilenames bool
	AFPCNIDBackend         string
	AFPVolumes             []afp.VolumeConfig
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

		AFPEnabled:             true,
		AFPServerName:          "Go File Server",
		AFPProtocols:           "tcp,ddp",
		AFPTCPBinding:          ":548",
		AFPDecomposedFilenames: true,
		AFPCNIDBackend:         "sqlite",
	}
}

func defaultMacGardenVolumePath(name string) string { return afp.DefaultMacGardenVolumePath(name) }

// loadConfigFromFile parses the file at path as TOML and resolves it
// into a fileConfig. Section schema lives here; the config package only
// abstracts the source.
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

	cfg.AFPEnabled = boolWithDefault(k, "AFP.enabled", cfg.AFPEnabled)
	cfg.AFPServerName = stringWithDefault(k, "AFP.name", cfg.AFPServerName)
	cfg.AFPZone = stringWithDefault(k, "AFP.zone", cfg.AFPZone)
	cfg.AFPProtocols = stringWithDefault(k, "AFP.protocols", cfg.AFPProtocols)
	cfg.AFPTCPBinding = stringWithDefault(k, "AFP.binding", cfg.AFPTCPBinding)
	cfg.AFPExtensionMapPath = stringWithDefault(k, "AFP.extension_map", cfg.AFPExtensionMapPath)
	if cfg.AFPExtensionMapPath != "" && !filepath.IsAbs(cfg.AFPExtensionMapPath) && src.ConfigDir != "" {
		cfg.AFPExtensionMapPath = filepath.Join(src.ConfigDir, cfg.AFPExtensionMapPath)
	}

	vols, decomposed, cnidBackend, volErr := afp.LoadVolumes(k, src.ConfigDir)
	if volErr != nil {
		return cfg, volErr
	}
	cfg.AFPVolumes = vols
	if decomposed != nil {
		cfg.AFPDecomposedFilenames = *decomposed
	}
	if cnidBackend != "" {
		cfg.AFPCNIDBackend = cnidBackend
	}
	if !cfg.AFPEnabled {
		cfg.AFPVolumes = nil
	}

	cfg.LogLevel = stringWithDefault(k, "Logging.level", cfg.LogLevel)
	cfg.ParsePackets = boolWithDefault(k, "Logging.parse_packets", cfg.ParsePackets)
	cfg.LogTraffic = boolWithDefault(k, "Logging.log_traffic", cfg.LogTraffic)
	cfg.ParseOutput = stringWithDefault(k, "Logging.parse_output", cfg.ParseOutput)

	return cfg, nil
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
