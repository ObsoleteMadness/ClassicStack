// Package config parses OmniTalk's TOML configuration and exposes it as
// a typed tree. Format is TOML, parsed via knadh/koanf with the
// pelletier/go-toml v2 parser. The package owns no CLI flag knowledge:
// main.go still handles flags and merges them over the file values.
//
// The shape here is transitional. Step (B) of the build-tag refactor
// will replace the flat Root struct with per-component LoadConfig calls
// against a koanf source, so service/afp etc. can register their own
// schemas and be omitted at build time. For now, Root preserves the
// pre-koanf field set so cmd/omnitalk keeps compiling.
package config

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"

	"github.com/pgodw/omnitalk/service/afp"
)

// Root is the parsed configuration. Fields are grouped by source
// section name for readability.
type Root struct {
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

// Defaults returns a Root populated with the same built-in defaults that
// cmd/omnitalk's flag parser uses when no config file is present.
func Defaults() Root {
	return Root{
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

// Load parses path as TOML and merges the result over Defaults(). On
// error it still returns the defaults alongside the error so callers
// may display them.
func Load(path string) (Root, error) {
	cfg := Defaults()

	k := koanf.New(".")
	if err := k.Load(file.Provider(path), toml.Parser()); err != nil {
		return cfg, err
	}

	// LToUdp
	cfg.LToUDPEnabled = boolWithDefault(k, "LToUdp.enabled", cfg.LToUDPEnabled)
	cfg.LToUDPInterface = stringWithDefault(k, "LToUdp.interface", cfg.LToUDPInterface)
	cfg.LToUDPSeedNetwork = uintWithDefault(k, "LToUdp.seed_network", cfg.LToUDPSeedNetwork)
	cfg.LToUDPSeedZone = stringWithDefault(k, "LToUdp.seed_zone", cfg.LToUDPSeedZone)

	// TashTalk
	cfg.TashTalkPort = stringWithDefault(k, "TashTalk.port", cfg.TashTalkPort)
	cfg.TashTalkSeedNetwork = uintWithDefault(k, "TashTalk.seed_network", cfg.TashTalkSeedNetwork)
	cfg.TashTalkSeedZone = stringWithDefault(k, "TashTalk.seed_zone", cfg.TashTalkSeedZone)

	// EtherTalk
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

	// MacIP
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

	// AFP
	cfg.AFPEnabled = boolWithDefault(k, "AFP.enabled", cfg.AFPEnabled)
	cfg.AFPServerName = stringWithDefault(k, "AFP.name", cfg.AFPServerName)
	cfg.AFPZone = stringWithDefault(k, "AFP.zone", cfg.AFPZone)
	cfg.AFPProtocols = stringWithDefault(k, "AFP.protocols", cfg.AFPProtocols)
	cfg.AFPTCPBinding = stringWithDefault(k, "AFP.binding", cfg.AFPTCPBinding)
	cfg.AFPExtensionMapPath = stringWithDefault(k, "AFP.extension_map", cfg.AFPExtensionMapPath)
	if cfg.AFPExtensionMapPath != "" && !filepath.IsAbs(cfg.AFPExtensionMapPath) {
		cfg.AFPExtensionMapPath = filepath.Join(filepath.Dir(path), cfg.AFPExtensionMapPath)
	}

	// Volumes.* — koanf nests these as map keys under "Volumes".
	cfg.AFPVolumes = nil
	var (
		seenDecomposed  bool
		seenCNIDBackend bool
	)
	for _, key := range k.MapKeys("Volumes") {
		base := "Volumes." + key
		sectionName := "Volumes." + key
		name := stringWithDefault(k, base+".name", key)

		vol := afp.VolumeConfig{Name: name, FSType: afp.FSTypeLocalFS}
		if k.Exists(base + ".fs_type") {
			fsType, parseErr := afp.NormalizeFSType(stringWithDefault(k, base+".fs_type", afp.FSTypeLocalFS))
			if parseErr != nil {
				return cfg, fmt.Errorf("[%s] %w", sectionName, parseErr)
			}
			vol.FSType = fsType
		}

		pathVal := stringWithDefault(k, base+".path", "")
		if strings.TrimSpace(pathVal) == "" {
			if vol.FSType == afp.FSTypeMacGarden {
				pathVal = DefaultMacGardenVolumePath(name)
			} else {
				return cfg, fmt.Errorf("[%s] path is required", sectionName)
			}
		}
		vol.Path = pathVal

		if k.Exists(base + ".rebuild_desktop_db") {
			vol.RebuildDesktopDB = k.Bool(base + ".rebuild_desktop_db")
		}
		if k.Exists(base + ".read_only") {
			vol.ReadOnly = k.Bool(base + ".read_only")
		}

		if k.Exists(base + ".use_decomposed_names") {
			v := k.Bool(base + ".use_decomposed_names")
			if seenDecomposed && v != cfg.AFPDecomposedFilenames {
				return cfg, fmt.Errorf("[%s] use_decomposed_names conflicts with another volume section", sectionName)
			}
			cfg.AFPDecomposedFilenames = v
			seenDecomposed = true
		}

		if k.Exists(base + ".cnid_backend") {
			backendVal := stringWithDefault(k, base+".cnid_backend", cfg.AFPCNIDBackend)
			if backendVal == "" {
				backendVal = cfg.AFPCNIDBackend
			}
			if seenCNIDBackend && !strings.EqualFold(backendVal, cfg.AFPCNIDBackend) {
				return cfg, fmt.Errorf("[%s] cnid_backend conflicts with another volume section", sectionName)
			}
			cfg.AFPCNIDBackend = backendVal
			seenCNIDBackend = true
		}

		if k.Exists(base + ".fork_backend") {
			fb := strings.ToLower(stringWithDefault(k, base+".fork_backend", ""))
			if fb != "" && fb != "appledouble" {
				return cfg, fmt.Errorf("[%s] fork_backend must be blank or AppleDouble", sectionName)
			}
		}

		if k.Exists(base + ".appledouble_mode") {
			modeVal := stringWithDefault(k, base+".appledouble_mode", "")
			parsedMode, parseErr := parseAppleDoubleMode(modeVal)
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

	// Logging
	cfg.LogLevel = stringWithDefault(k, "Logging.level", cfg.LogLevel)
	cfg.ParsePackets = boolWithDefault(k, "Logging.parse_packets", cfg.ParsePackets)
	cfg.LogTraffic = boolWithDefault(k, "Logging.log_traffic", cfg.LogTraffic)
	cfg.ParseOutput = stringWithDefault(k, "Logging.parse_output", cfg.ParseOutput)

	return cfg, nil
}

// DefaultMacGardenVolumePath derives a filesystem-safe default volume
// path for a MacGarden-backed volume that did not specify one.
func DefaultMacGardenVolumePath(name string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_':
			return r
		case r == ' ':
			return '_'
		default:
			return -1
		}
	}, strings.TrimSpace(name))
	if safe == "" {
		safe = "MacGarden"
	}
	return filepath.Join(".macgarden", safe)
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

func parseAppleDoubleMode(value string) (afp.AppleDoubleMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "modern", string(afp.AppleDoubleModeModern):
		return afp.AppleDoubleModeModern, nil
	case "legacy", string(afp.AppleDoubleModeLegacy):
		return afp.AppleDoubleModeLegacy, nil
	default:
		return "", fmt.Errorf("appledouble_mode must be modern or legacy, got %q", value)
	}
}
