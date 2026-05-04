package main

import (
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/capture"
	"github.com/ObsoleteMadness/ClassicStack/port/ethertalk"
	"github.com/ObsoleteMadness/ClassicStack/port/localtalk"
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

	CaptureLocalTalk string
	CaptureEtherTalk string
	CaptureSnaplen   uint

	IPXEnabled         bool
	IPXInterface       string
	IPXFraming         string
	IPXInternalNetwork string

	NetBEUIEnabled   bool
	NetBEUIInterface string

	NetBIOSEnabled    bool
	NetBIOSTransports string // raw csv from flag; resolveAppConfig parses
	NetBIOSScopeID    string
	NetBIOSServerName string
	NetBIOSWorkgroup  string

	SMBEnabled       bool
	SMBNBTBinding    string
	SMBDirectBinding string
	SMBGuestOk       bool
	SMBServerName    string
	SMBWorkgroup     string
	SMBShareValues   []string // raw "Name:Path" entries from -smb-share

	ShortnameEnabled bool
	ShortnameBackend string
	ShortnameDBPath  string
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

	cfg.Capture = capture.Config{
		LocalTalk: in.CaptureLocalTalk,
		EtherTalk: in.CaptureEtherTalk,
		Snaplen:   uint32(in.CaptureSnaplen),
	}

	cfg.IPXEnabled = in.IPXEnabled
	cfg.IPXInterface = in.IPXInterface
	if in.IPXFraming != "" {
		cfg.IPXFraming = in.IPXFraming
	}
	cfg.IPXInternalNetwork = in.IPXInternalNetwork

	cfg.NetBEUIEnabled = in.NetBEUIEnabled
	cfg.NetBEUIInterface = in.NetBEUIInterface

	cfg.NetBIOSEnabled = in.NetBIOSEnabled
	if in.NetBIOSTransports != "" {
		parts := splitCSV(in.NetBIOSTransports)
		if len(parts) > 0 {
			cfg.NetBIOSTransports = parts
		}
	}
	cfg.NetBIOSScopeID = in.NetBIOSScopeID
	cfg.NetBIOSServerName = in.NetBIOSServerName
	cfg.NetBIOSWorkgroup = in.NetBIOSWorkgroup

	cfg.SMBEnabled = in.SMBEnabled
	if in.SMBNBTBinding != "" {
		cfg.SMBNBTBinding = in.SMBNBTBinding
	}
	cfg.SMBDirectBinding = in.SMBDirectBinding
	cfg.SMBGuestOk = in.SMBGuestOk
	cfg.SMBServerName = in.SMBServerName
	cfg.SMBWorkgroup = in.SMBWorkgroup
	cfg.SMBShareFlags = in.SMBShareValues

	cfg.ShortnameEnabled = in.ShortnameEnabled
	if in.ShortnameBackend != "" {
		cfg.ShortnameBackend = in.ShortnameBackend
	}
	cfg.ShortnameDBPath = in.ShortnameDBPath

	return cfg
}

func splitCSV(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		p := strings.TrimSpace(part)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
