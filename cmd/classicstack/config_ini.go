package main

import (
	"fmt"
	"strings"

	"github.com/knadh/koanf/v2"

	"github.com/ObsoleteMadness/ClassicStack/capture"
	"github.com/ObsoleteMadness/ClassicStack/config"
	"github.com/ObsoleteMadness/ClassicStack/port/ethertalk"
	"github.com/ObsoleteMadness/ClassicStack/port/localtalk"
)

// appConfig is the cmd-local view of resolved configuration. Each
// section is a typed Config struct owned by the package that consumes
// it. The same struct is populated either from a TOML file (via
// loadConfigFromFile) or from CLI flags (via flagsToConfig); downstream
// wiring reads only from this struct, never from flag pointers. AFP
// lives behind //go:build afp and is wired up separately via wireAFP.
type appConfig struct {
	LogLevel     string
	LogTraffic   bool
	ParsePackets bool
	ParseOutput  string

	Bridge    BridgeConfig
	LToUDP    localtalk.LToUDPConfig
	TashTalk  localtalk.TashTalkConfig
	EtherTalk ethertalk.Config
	Capture   capture.Config

	MacIPEnabled    bool
	MacIPNAT        bool
	MacIPSubnet     string
	MacIPGWIP       string
	MacIPNameserver string
	MacIPGatewayIP  string
	MacIPDHCPRelay  bool
	MacIPLeaseFile  string
	MacIPZone       string
	MacIPFilter     string

	IPXEnabled         bool
	IPXInterface       string
	IPXFraming         string
	IPXInternalNetwork string
	IPXFilter          string

	IPXGWEnabled  bool
	IPXGWBindings []IPXGWZoneBinding

	NetBEUIEnabled   bool
	NetBEUIInterface string
	NetBEUIFilter    string

	NetBIOSEnabled    bool
	NetBIOSTransports []string
	NetBIOSScopeID    string
	NetBIOSServerName string
	NetBIOSWorkgroup  string

	SMBEnabled       bool
	SMBNBTBinding    string
	SMBDirectBinding string
	SMBGuestOk       bool
	SMBServerName    string
	SMBWorkgroup     string
	SMBShareFlags    []string // raw "Name:Path" entries from -smb-share (flag mode only)

	ShortnameWindowsShortnames bool
	ShortnameBackend           string
	ShortnameDBPath            string
}

const (
	defaultSMBServerName = "CLASSICSTACK"
	defaultSMBWorkgroup  = "WORKGROUP"
)

func defaultAppConfig() appConfig {
	return appConfig{
		LogLevel: "info",

		Bridge:    defaultBridgeConfig(),
		LToUDP:    localtalk.DefaultLToUDPConfig(),
		TashTalk:  localtalk.DefaultTashTalkConfig(),
		EtherTalk: ethertalk.DefaultConfig(),
		Capture:   capture.DefaultConfig(),

		MacIPSubnet: "192.168.100.0/24",

		IPXFraming:        "ethernet_ii",
		NetBIOSTransports: []string{"tcp"},
		SMBNBTBinding:     ":139",
		SMBServerName:     defaultSMBServerName,
		SMBWorkgroup:      defaultSMBWorkgroup,
		ShortnameBackend:  "memory",
	}
}

// loadConfigFromFile loads and resolves the cmd-neutral sections of the
// TOML config. The raw config.Source is also returned so optional
// subsystems (currently AFP, behind //go:build afp) can lazily read
// their own sections without appConfig having to know about them.
func loadConfigFromFile(path string) (appConfig, config.Source, error) {
	src, err := config.Load(path)
	if err != nil {
		return defaultAppConfig(), config.Source{}, err
	}
	cfg, err := resolveAppConfig(src)
	if err != nil {
		return defaultAppConfig(), src, err
	}
	return cfg, src, nil
}

func resolveAppConfig(src config.Source) (appConfig, error) {
	cfg := defaultAppConfig()
	k := src.K

	if err := loadSection(k, "LToUdp", &cfg.LToUDP); err != nil {
		return cfg, err
	}
	if err := loadSection(k, "Bridge", &cfg.Bridge); err != nil {
		return cfg, err
	}
	if err := loadSection(k, "TashTalk", &cfg.TashTalk); err != nil {
		return cfg, err
	}
	if err := loadSection(k, "EtherTalk", &cfg.EtherTalk); err != nil {
		return cfg, err
	}
	if err := loadSection(k, "Capture", &cfg.Capture); err != nil {
		return cfg, err
	}
	if err := rejectLegacyBridgeKeys(k); err != nil {
		return cfg, err
	}
	syncBridgeToEtherTalk(&cfg)

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
	cfg.MacIPFilter = strings.TrimSpace(k.String("MacIP.filter"))

	cfg.LogLevel = stringWithDefault(k, "Logging.level", cfg.LogLevel)
	cfg.ParsePackets = boolWithDefault(k, "Logging.parse_packets", cfg.ParsePackets)
	cfg.LogTraffic = boolWithDefault(k, "Logging.log_traffic", cfg.LogTraffic)
	cfg.ParseOutput = stringWithDefault(k, "Logging.parse_output", cfg.ParseOutput)

	cfg.IPXEnabled = boolWithDefault(k, "IPX.enabled", cfg.IPXEnabled)
	cfg.IPXInterface = stringWithDefault(k, "IPX.interface", cfg.IPXInterface)
	cfg.IPXFraming = stringWithDefault(k, "IPX.framing", cfg.IPXFraming)
	cfg.IPXInternalNetwork = stringWithDefault(k, "IPX.internal_network", cfg.IPXInternalNetwork)
	cfg.IPXFilter = strings.TrimSpace(k.String("IPX.filter"))

	cfg.IPXGWEnabled = boolWithDefault(k, "IPXGW.enabled", cfg.IPXGWEnabled)
	if k.Exists("IPXGW.bindings") {
		for _, raw := range k.Strings("IPXGW.bindings") {
			parts := strings.SplitN(raw, ":", 2)
			if len(parts) != 2 {
				return cfg, fmt.Errorf("[IPXGW] bindings entry must be \"Object:Zone\", got %q", raw)
			}
			cfg.IPXGWBindings = append(cfg.IPXGWBindings, IPXGWZoneBinding{
				Object: strings.TrimSpace(parts[0]),
				Zone:   strings.TrimSpace(parts[1]),
			})
		}
	}

	cfg.NetBEUIEnabled = boolWithDefault(k, "NetBEUI.enabled", cfg.NetBEUIEnabled)
	cfg.NetBEUIInterface = stringWithDefault(k, "NetBEUI.interface", cfg.NetBEUIInterface)
	cfg.NetBEUIFilter = strings.TrimSpace(k.String("NetBEUI.filter"))

	cfg.NetBIOSEnabled = boolWithDefault(k, "NetBIOS.enabled", cfg.NetBIOSEnabled)
	if k.Exists("NetBIOS.transports") {
		cfg.NetBIOSTransports = k.Strings("NetBIOS.transports")
	}
	cfg.NetBIOSScopeID = stringWithDefault(k, "NetBIOS.scope_id", cfg.NetBIOSScopeID)
	cfg.NetBIOSServerName = stringWithDefault(k, "NetBIOS.server_name", cfg.NetBIOSServerName)
	cfg.NetBIOSWorkgroup = stringWithDefault(k, "NetBIOS.workgroup", cfg.NetBIOSWorkgroup)

	cfg.SMBEnabled = boolWithDefault(k, "SMB.enabled", cfg.SMBEnabled)
	cfg.SMBNBTBinding = stringWithDefault(k, "SMB.nbt_binding", cfg.SMBNBTBinding)
	cfg.SMBDirectBinding = stringWithDefault(k, "SMB.direct_binding", cfg.SMBDirectBinding)
	cfg.SMBGuestOk = boolWithDefault(k, "SMB.guest_ok", cfg.SMBGuestOk)
	cfg.SMBServerName = stringWithDefault(k, "SMB.server_name", cfg.SMBServerName)
	cfg.SMBWorkgroup = stringWithDefault(k, "SMB.workgroup", cfg.SMBWorkgroup)

	cfg.ShortnameWindowsShortnames = boolWithDefault(k, "Shortname.windows_shortnames", cfg.ShortnameWindowsShortnames)
	cfg.ShortnameBackend = stringWithDefault(k, "Shortname.backend", cfg.ShortnameBackend)
	cfg.ShortnameDBPath = stringWithDefault(k, "Shortname.db_path", cfg.ShortnameDBPath)

	normalizeSMBIdentity(&cfg)

	return cfg, nil
}

func rejectLegacyBridgeKeys(k *koanf.Koanf) error {
	legacy := []string{
		"EtherTalk.backend",
		"EtherTalk.device",
		"EtherTalk.hw_address",
		"EtherTalk.bridge_mode",
	}
	for _, key := range legacy {
		if k.Exists(key) {
			return fmt.Errorf("[%s] is no longer supported in config files; use [Bridge] keys instead", key)
		}
	}
	return nil
}

func syncBridgeToEtherTalk(cfg *appConfig) {
	cfg.Bridge.Mode = strings.ToLower(strings.TrimSpace(cfg.Bridge.Mode))
	cfg.Bridge.Device = strings.TrimSpace(cfg.Bridge.Device)
	cfg.Bridge.HWAddress = strings.TrimSpace(cfg.Bridge.HWAddress)
	cfg.Bridge.BridgeMode = strings.ToLower(strings.TrimSpace(cfg.Bridge.BridgeMode))

	cfg.EtherTalk.Backend = cfg.Bridge.Mode
	cfg.EtherTalk.Device = cfg.Bridge.Device
	cfg.EtherTalk.HWAddress = cfg.Bridge.HWAddress
	cfg.EtherTalk.BridgeMode = cfg.Bridge.BridgeMode
	if cfg.EtherTalk.Backend == "" {
		cfg.EtherTalk.Device = ""
	}
}

// normalizeSMBIdentity makes SMB identity canonical and keeps NetBIOS
// aligned with it while NetBIOS is enabled.
func normalizeSMBIdentity(cfg *appConfig) {
	cfg.SMBServerName = strings.TrimSpace(cfg.SMBServerName)
	if cfg.SMBServerName == "" {
		cfg.SMBServerName = defaultSMBServerName
	}
	cfg.SMBWorkgroup = strings.TrimSpace(cfg.SMBWorkgroup)
	if cfg.SMBWorkgroup == "" {
		cfg.SMBWorkgroup = defaultSMBWorkgroup
	}

	cfg.NetBIOSServerName = strings.TrimSpace(cfg.NetBIOSServerName)
	cfg.NetBIOSWorkgroup = strings.TrimSpace(cfg.NetBIOSWorkgroup)
	if cfg.NetBIOSEnabled {
		cfg.NetBIOSServerName = cfg.SMBServerName
		cfg.NetBIOSWorkgroup = cfg.SMBWorkgroup
	}
}

// validatable is the shape that every package's Config struct exposes:
// koanf-tagged fields, defaults via the package's DefaultConfig(), and a
// Validate method that enforces logical (not syntactic) rules.
type validatable interface {
	Validate() error
}

// loadSection unmarshals a single subtree of the koanf instance onto an
// already-defaulted target, then runs the target's Validate. The target
// must be a pointer to a struct with koanf tags; it must also satisfy
// the validatable interface.
func loadSection(k *koanf.Koanf, key string, target validatable) error {
	if !k.Exists(key) {
		return target.Validate()
	}
	if err := k.UnmarshalWithConf(key, target, koanf.UnmarshalConf{Tag: "koanf"}); err != nil {
		return fmt.Errorf("[%s] %w", key, err)
	}
	if err := target.Validate(); err != nil {
		return fmt.Errorf("[%s] %w", key, err)
	}
	return nil
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
