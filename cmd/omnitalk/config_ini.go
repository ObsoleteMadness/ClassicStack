package main

import (
	"fmt"
	"strings"

	"github.com/knadh/koanf/v2"

	"github.com/pgodw/omnitalk/config"
	"github.com/pgodw/omnitalk/port/ethertalk"
	"github.com/pgodw/omnitalk/port/localtalk"
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

	LToUDP    localtalk.LToUDPConfig
	TashTalk  localtalk.TashTalkConfig
	EtherTalk ethertalk.Config

	MacIPEnabled    bool
	MacIPNAT        bool
	MacIPSubnet     string
	MacIPGWIP       string
	MacIPNameserver string
	MacIPGatewayIP  string
	MacIPDHCPRelay  bool
	MacIPLeaseFile  string
	MacIPZone       string
}

func defaultAppConfig() appConfig {
	return appConfig{
		LogLevel: "info",

		LToUDP:    localtalk.DefaultLToUDPConfig(),
		TashTalk:  localtalk.DefaultTashTalkConfig(),
		EtherTalk: ethertalk.DefaultConfig(),

		MacIPSubnet: "192.168.100.0/24",
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
	if err := loadSection(k, "TashTalk", &cfg.TashTalk); err != nil {
		return cfg, err
	}
	if err := loadSection(k, "EtherTalk", &cfg.EtherTalk); err != nil {
		return cfg, err
	}
	cfg.EtherTalk.Backend = strings.ToLower(strings.TrimSpace(cfg.EtherTalk.Backend))
	if cfg.EtherTalk.Backend == "" {
		cfg.EtherTalk.Device = ""
	}

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

	cfg.LogLevel = stringWithDefault(k, "Logging.level", cfg.LogLevel)
	cfg.ParsePackets = boolWithDefault(k, "Logging.parse_packets", cfg.ParsePackets)
	cfg.LogTraffic = boolWithDefault(k, "Logging.log_traffic", cfg.LogTraffic)
	cfg.ParseOutput = stringWithDefault(k, "Logging.parse_output", cfg.ParseOutput)

	return cfg, nil
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
