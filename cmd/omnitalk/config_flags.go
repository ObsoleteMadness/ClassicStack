package main

import (
	"github.com/pgodw/omnitalk/port/ethertalk"
	"github.com/pgodw/omnitalk/port/localtalk"
)

// flagInputs collects raw values from the CLI flags. main.go derefs each
// pointer once and passes them here so flag-driven runs and config-file
// runs both produce a single appConfig that downstream wiring reads.
type flagInputs struct {
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
	EtherTalkHWAddress      string
	EtherTalkBridgeMode     string
	EtherTalkBridgeHostMAC  string
	EtherTalkSeedNetworkMin uint
	EtherTalkSeedNetworkMax uint
	EtherTalkSeedZone       string
	EtherTalkDesiredNetwork uint
	EtherTalkDesiredNode    uint

	MacIPEnabled    bool
	MacIPGWIP       string
	MacIPSubnet     string
	MacIPNameserver string
	MacIPZone       string
	MacIPGatewayIP  string
	MacIPNAT        bool
	MacIPDHCPRelay  bool
	MacIPLeaseFile  string
}

// flagsToConfig builds an appConfig from CLI flag values. It is the
// flag-driven counterpart to loadConfigFromFile and is the only place
// that translates flag pointers into the unified config struct.
func flagsToConfig(in flagInputs) appConfig {
	cfg := defaultAppConfig()

	cfg.LogLevel = in.LogLevel
	cfg.LogTraffic = in.LogTraffic
	cfg.ParsePackets = in.ParsePackets
	cfg.ParseOutput = in.ParseOutput

	cfg.LToUDP = localtalk.LToUDPConfig{
		Enabled:     in.LToUDPEnabled,
		Interface:   in.LToUDPInterface,
		SeedNetwork: in.LToUDPSeedNetwork,
		SeedZone:    in.LToUDPSeedZone,
	}

	cfg.TashTalk = localtalk.TashTalkConfig{
		Port:        in.TashTalkPort,
		SeedNetwork: in.TashTalkSeedNetwork,
		SeedZone:    in.TashTalkSeedZone,
	}

	cfg.EtherTalk = ethertalk.Config{
		Device:         in.EtherTalkDevice,
		Backend:        in.EtherTalkBackend,
		HWAddress:      in.EtherTalkHWAddress,
		BridgeMode:     in.EtherTalkBridgeMode,
		BridgeHostMAC:  in.EtherTalkBridgeHostMAC,
		SeedNetworkMin: in.EtherTalkSeedNetworkMin,
		SeedNetworkMax: in.EtherTalkSeedNetworkMax,
		SeedZone:       in.EtherTalkSeedZone,
		DesiredNetwork: in.EtherTalkDesiredNetwork,
		DesiredNode:    in.EtherTalkDesiredNode,
	}

	cfg.MacIPEnabled = in.MacIPEnabled
	cfg.MacIPGWIP = in.MacIPGWIP
	cfg.MacIPSubnet = in.MacIPSubnet
	cfg.MacIPNameserver = in.MacIPNameserver
	cfg.MacIPZone = in.MacIPZone
	cfg.MacIPGatewayIP = in.MacIPGatewayIP
	cfg.MacIPNAT = in.MacIPNAT
	cfg.MacIPDHCPRelay = in.MacIPDHCPRelay
	cfg.MacIPLeaseFile = in.MacIPLeaseFile

	return cfg
}
