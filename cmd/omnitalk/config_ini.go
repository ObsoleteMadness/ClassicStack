package main

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pgodw/omnitalk/go/service/afp"
	"gopkg.in/ini.v1"
)

type iniConfig struct {
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

func defaultINIConfig() iniConfig {
	return iniConfig{
		LogLevel:     "info",
		LogTraffic:   false,
		ParsePackets: false,
		ParseOutput:  "",

		LToUDPEnabled:     true,
		LToUDPInterface:   "0.0.0.0",
		LToUDPSeedNetwork: 1,
		LToUDPSeedZone:    "LToUDP Network",

		TashTalkPort:        "",
		TashTalkSeedNetwork: 2,
		TashTalkSeedZone:    "TashTalk Network",

		EtherTalkDevice:         "",
		EtherTalkBackend:        "pcap",
		EtherTalkHWAddr:         "DE:AD:BE:EF:CA:FE",
		EtherTalkBridgeMode:     "auto",
		EtherTalkBridgeHostMAC:  "",
		EtherTalkSeedNetworkMin: 3,
		EtherTalkSeedNetworkMax: 5,
		EtherTalkSeedZone:       "EtherTalk Network",

		MacIPEnabled:    false,
		MacIPNAT:        false,
		MacIPSubnet:     "192.168.100.0/24",
		MacIPGWIP:       "",
		MacIPNameserver: "",
		MacIPGatewayIP:  "",
		MacIPDHCPRelay:  false,
		MacIPLeaseFile:  "",
		MacIPZone:       "",

		AFPEnabled:             true,
		AFPServerName:          "Go File Server",
		AFPZone:                "",
		AFPProtocols:           "tcp,ddp",
		AFPTCPBinding:          ":548",
		AFPExtensionMapPath:    "",
		AFPDecomposedFilenames: true,
		AFPCNIDBackend:         "sqlite",
		AFPVolumes:             nil,
	}
}

func loadConfigFromINI(path string) (iniConfig, error) {
	cfg := defaultINIConfig()

	f, err := ini.Load(path)
	if err != nil {
		return cfg, err
	}

	lt := f.Section("LToUdp")
	if cfg.LToUDPEnabled, err = parseBoolKey(lt, "enabled", cfg.LToUDPEnabled); err != nil {
		return cfg, err
	}
	cfg.LToUDPInterface = parseStringKey(lt, "interface", cfg.LToUDPInterface)
	if cfg.LToUDPSeedNetwork, err = parseUintKey(lt, "seed_network", cfg.LToUDPSeedNetwork); err != nil {
		return cfg, err
	}
	cfg.LToUDPSeedZone = parseStringKey(lt, "seed_zone", cfg.LToUDPSeedZone)

	tt := f.Section("TashTalk")
	cfg.TashTalkPort = parseStringKey(tt, "port", cfg.TashTalkPort)
	if cfg.TashTalkSeedNetwork, err = parseUintKey(tt, "seed_network", cfg.TashTalkSeedNetwork); err != nil {
		return cfg, err
	}
	cfg.TashTalkSeedZone = parseStringKey(tt, "seed_zone", cfg.TashTalkSeedZone)

	et := f.Section("EtherTalk")
	backend := strings.ToLower(parseStringKey(et, "backend", cfg.EtherTalkBackend))
	switch backend {
	case "", "pcap", "tap", "tun":
		// Empty backend disables EtherTalk and is handled by empty device string.
	default:
		return cfg, fmt.Errorf("[EtherTalk] backend must be blank, pcap, tap, or tun, got %q", backend)
	}
	cfg.EtherTalkBackend = backend
	cfg.EtherTalkDevice = parseStringKey(et, "device", cfg.EtherTalkDevice)
	if backend == "" {
		cfg.EtherTalkDevice = ""
	}
	cfg.EtherTalkHWAddr = parseStringKey(et, "hw_address", cfg.EtherTalkHWAddr)
	cfg.EtherTalkBridgeMode = parseStringKey(et, "bridge_mode", cfg.EtherTalkBridgeMode)
	cfg.EtherTalkBridgeHostMAC = parseStringKey(et, "bridge_host_mac", cfg.EtherTalkBridgeHostMAC)
	if cfg.EtherTalkSeedNetworkMin, err = parseUintKey(et, "seed_network_min", cfg.EtherTalkSeedNetworkMin); err != nil {
		return cfg, err
	}
	if cfg.EtherTalkSeedNetworkMax, err = parseUintKey(et, "seed_network_max", cfg.EtherTalkSeedNetworkMax); err != nil {
		return cfg, err
	}
	cfg.EtherTalkSeedZone = parseStringKey(et, "seed_zone", cfg.EtherTalkSeedZone)

	macipSection := f.Section("MacIP")
	if cfg.MacIPEnabled, err = parseBoolKey(macipSection, "enabled", cfg.MacIPEnabled); err != nil {
		return cfg, err
	}
	mode := strings.ToLower(parseStringKey(macipSection, "mode", ""))
	switch mode {
	case "", "pcap":
		cfg.MacIPNAT = false
	case "nat":
		cfg.MacIPNAT = true
	default:
		return cfg, fmt.Errorf("[MacIP] mode must be pcap or nat, got %q", mode)
	}
	cfg.MacIPNameserver = parseStringKey(macipSection, "nameserver", cfg.MacIPNameserver)
	cfg.MacIPSubnet = parseStringKey(macipSection, "nat_subnet", cfg.MacIPSubnet)
	// nat_gw maps to -macip-nat-gw.
	cfg.MacIPGWIP = parseStringKey(macipSection, "nat_gw", cfg.MacIPGWIP)
	cfg.MacIPLeaseFile = parseStringKey(macipSection, "lease_file", cfg.MacIPLeaseFile)
	// ip_gateway maps to -macip-ip-gateway.
	cfg.MacIPGatewayIP = parseStringKey(macipSection, "ip_gateway", cfg.MacIPGatewayIP)
	if cfg.MacIPDHCPRelay, err = parseBoolKey(macipSection, "dhcp_relay", cfg.MacIPDHCPRelay); err != nil {
		return cfg, err
	}
	cfg.MacIPZone = parseStringKey(macipSection, "zone", cfg.MacIPZone)

	afpSection := f.Section("AFP")
	if cfg.AFPEnabled, err = parseBoolKey(afpSection, "enabled", cfg.AFPEnabled); err != nil {
		return cfg, err
	}
	cfg.AFPServerName = parseStringKey(afpSection, "name", cfg.AFPServerName)
	cfg.AFPZone = parseStringKey(afpSection, "zone", cfg.AFPZone)
	cfg.AFPProtocols = parseStringKey(afpSection, "protocols", cfg.AFPProtocols)
	cfg.AFPTCPBinding = parseStringKey(afpSection, "binding", cfg.AFPTCPBinding)
	cfg.AFPExtensionMapPath = parseStringKey(afpSection, "extension_map", cfg.AFPExtensionMapPath)
	if cfg.AFPExtensionMapPath != "" && !filepath.IsAbs(cfg.AFPExtensionMapPath) {
		cfg.AFPExtensionMapPath = filepath.Join(filepath.Dir(path), cfg.AFPExtensionMapPath)
	}
	cfg.AFPVolumes = nil
	var (
		seenDecomposed  bool
		seenCNIDBackend bool
	)
	for _, sec := range f.Sections() {
		if !strings.HasPrefix(strings.ToLower(sec.Name()), "volumes.") {
			continue
		}

		sectionName := sec.Name()
		defaultVolumeName := strings.TrimPrefix(sectionName, "Volumes.")
		if defaultVolumeName == sectionName {
			defaultVolumeName = strings.TrimPrefix(sectionName, "volumes.")
		}
		name := parseStringKey(sec, "name", defaultVolumeName)
		pathVal := parseStringKey(sec, "path", "")
		if strings.TrimSpace(pathVal) == "" {
			return cfg, fmt.Errorf("[%s] path is required", sectionName)
		}

		vol := afp.VolumeConfig{Name: name, Path: pathVal}
		if sec.HasKey("rebuild_desktop_db") {
			v, parseErr := parseBoolKey(sec, "rebuild_desktop_db", false)
			if parseErr != nil {
				return cfg, parseErr
			}
			vol.RebuildDesktopDB = v
		}

		if sec.HasKey("read_only") {
			v, parseErr := parseBoolKey(sec, "read_only", false)
			if parseErr != nil {
				return cfg, parseErr
			}
			vol.ReadOnly = v
		}

		if sec.HasKey("use_decomposed_names") {
			v, parseErr := parseBoolKey(sec, "use_decomposed_names", cfg.AFPDecomposedFilenames)
			if parseErr != nil {
				return cfg, parseErr
			}
			if seenDecomposed && v != cfg.AFPDecomposedFilenames {
				return cfg, fmt.Errorf("[%s] use_decomposed_names conflicts with another volume section", sectionName)
			}
			cfg.AFPDecomposedFilenames = v
			seenDecomposed = true
		}

		if sec.HasKey("cnid_backend") {
			backendVal := parseStringKey(sec, "cnid_backend", cfg.AFPCNIDBackend)
			if backendVal == "" {
				backendVal = cfg.AFPCNIDBackend
			}
			if seenCNIDBackend && !strings.EqualFold(backendVal, cfg.AFPCNIDBackend) {
				return cfg, fmt.Errorf("[%s] cnid_backend conflicts with another volume section", sectionName)
			}
			cfg.AFPCNIDBackend = backendVal
			seenCNIDBackend = true
		}

		if sec.HasKey("fork_backend") {
			forkBackend := strings.ToLower(parseStringKey(sec, "fork_backend", ""))
			if forkBackend != "" && forkBackend != "appledouble" {
				return cfg, fmt.Errorf("[%s] fork_backend must be blank or AppleDouble", sectionName)
			}
		}

		if sec.HasKey("appledouble_mode") {
			modeVal := strings.ToLower(parseStringKey(sec, "appledouble_mode", ""))
			parsedMode, parseErr := parseINIAppleDoubleMode(modeVal)
			if parseErr != nil {
				return cfg, fmt.Errorf("[%s] %w", sectionName, parseErr)
			}
			vol.AppleDoubleMode = parsedMode
		}

		cfg.AFPVolumes = append(cfg.AFPVolumes, vol)
	}

	if !cfg.AFPEnabled {
		cfg.AFPVolumes = nil
	}

	loggingSection := f.Section("Logging")
	cfg.LogLevel = parseStringKey(loggingSection, "level", cfg.LogLevel)
	if cfg.ParsePackets, err = parseBoolKey(loggingSection, "parse_packets", cfg.ParsePackets); err != nil {
		return cfg, err
	}
	if cfg.LogTraffic, err = parseBoolKey(loggingSection, "log_traffic", cfg.LogTraffic); err != nil {
		return cfg, err
	}
	cfg.ParseOutput = parseStringKey(loggingSection, "parse_output", cfg.ParseOutput)

	return cfg, nil
}

func parseStringKey(sec *ini.Section, key, defaultVal string) string {
	if !sec.HasKey(key) {
		return defaultVal
	}
	v := stripOptionalQuotes(sec.Key(key).String())
	if strings.TrimSpace(v) == "" {
		return defaultVal
	}
	return v
}

func parseBoolKey(sec *ini.Section, key string, defaultVal bool) (bool, error) {
	if !sec.HasKey(key) {
		return defaultVal, nil
	}
	v := strings.TrimSpace(stripOptionalQuotes(sec.Key(key).String()))
	if v == "" {
		return defaultVal, nil
	}
	parsed, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("[%s] invalid bool for %q: %q", sec.Name(), key, v)
	}
	return parsed, nil
}

func parseUintKey(sec *ini.Section, key string, defaultVal uint) (uint, error) {
	if !sec.HasKey(key) {
		return defaultVal, nil
	}
	v := strings.TrimSpace(stripOptionalQuotes(sec.Key(key).String()))
	if v == "" {
		return defaultVal, nil
	}
	parsed, err := strconv.ParseUint(v, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("[%s] invalid uint for %q: %q", sec.Name(), key, v)
	}
	return uint(parsed), nil
}

func stripOptionalQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			return strings.TrimSpace(s[1 : len(s)-1])
		}
	}
	return s
}

func parseINIAppleDoubleMode(value string) (afp.AppleDoubleMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "modern", string(afp.AppleDoubleModeModern):
		return afp.AppleDoubleModeModern, nil
	case "legacy", string(afp.AppleDoubleModeLegacy):
		return afp.AppleDoubleModeLegacy, nil
	default:
		return "", fmt.Errorf("appledouble_mode must be modern or legacy, got %q", value)
	}
}
