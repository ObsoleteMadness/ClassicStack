package main

import (
	"github.com/ObsoleteMadness/ClassicStack/config"
	"github.com/ObsoleteMadness/ClassicStack/port/ethertalk"
	"github.com/ObsoleteMadness/ClassicStack/port/localtalk"
)

// appConfigFromModel converts a config.Model (the UI/serialisation view)
// into the cmd-local appConfig that the wiring functions consume. It is the
// inverse of modelFromAppConfig and lets the supervisor rebuild the stack
// from an edited model.
func appConfigFromModel(m *config.Model) (appConfig, error) {
	cfg := defaultAppConfig()

	cfg.LogLevel = m.Logging.Level
	cfg.LogTraffic = m.Logging.LogTraffic
	cfg.ParsePackets = m.Logging.ParsePackets
	cfg.ParseOutput = m.Logging.ParseOutput

	cfg.Bridge = BridgeConfig{
		Mode:       m.Bridge.Mode,
		Device:     m.Bridge.Device,
		HWAddress:  m.Bridge.HWAddress,
		BridgeMode: m.Bridge.BridgeMode,
	}

	cfg.LToUDP = localtalk.LToUDPConfig{
		Enabled:     m.LToUDP.Enabled,
		Interface:   m.LToUDP.Interface,
		SeedNetwork: m.LToUDP.SeedNetwork,
		SeedZone:    m.LToUDP.SeedZone,
	}
	cfg.TashTalk = localtalk.TashTalkConfig{
		Port:        m.TashTalk.Port,
		SeedNetwork: m.TashTalk.SeedNetwork,
		SeedZone:    m.TashTalk.SeedZone,
	}
	cfg.EtherTalk = ethertalk.Config{
		BridgeHostMAC:  m.EtherTalk.BridgeHostMAC,
		Filter:         m.EtherTalk.Filter,
		SeedNetworkMin: m.EtherTalk.SeedNetworkMin,
		SeedNetworkMax: m.EtherTalk.SeedNetworkMax,
		SeedZone:       m.EtherTalk.SeedZone,
		DesiredNetwork: m.EtherTalk.DesiredNetwork,
		DesiredNode:    m.EtherTalk.DesiredNode,
	}

	cfg.Capture.LocalTalk = m.Capture.LocalTalk
	cfg.Capture.EtherTalk = m.Capture.EtherTalk
	cfg.Capture.IPX = m.Capture.IPX
	cfg.Capture.NetBEUI = m.Capture.NetBEUI
	if m.Capture.Snaplen != 0 {
		cfg.Capture.Snaplen = m.Capture.Snaplen
	}

	cfg.MacIPEnabled = m.MacIP.Enabled
	cfg.MacIPNAT = m.MacIP.Mode == "nat"
	cfg.MacIPSubnet = orDefault(m.MacIP.NATSubnet, cfg.MacIPSubnet)
	cfg.MacIPGWIP = m.MacIP.NATGW
	cfg.MacIPNameserver = m.MacIP.Nameserver
	cfg.MacIPGatewayIP = m.MacIP.IPGateway
	cfg.MacIPDHCPRelay = m.MacIP.DHCPRelay
	cfg.MacIPLeaseFile = m.MacIP.LeaseFile
	cfg.MacIPZone = m.MacIP.Zone
	cfg.MacIPFilter = m.MacIP.Filter

	cfg.IPXEnabled = m.IPX.Enabled
	cfg.IPXInterface = m.IPX.Interface
	cfg.IPXFraming = orDefault(m.IPX.Framing, cfg.IPXFraming)
	cfg.IPXInternalNetwork = m.IPX.InternalNetwork
	cfg.IPXFilter = m.IPX.Filter

	cfg.IPXGWEnabled = m.IPXGW.Enabled
	cfg.IPXGWBindings = parseIPXGWBindings(m.IPXGW.Bindings)

	cfg.NetBEUIEnabled = m.NetBEUI.Enabled
	cfg.NetBEUIInterface = m.NetBEUI.Interface
	cfg.NetBEUIFilter = m.NetBEUI.Filter

	cfg.NetBIOSEnabled = m.NetBIOS.Enabled
	if len(m.NetBIOS.Transports) > 0 {
		cfg.NetBIOSTransports = m.NetBIOS.Transports
	}
	cfg.NetBIOSScopeID = m.NetBIOS.ScopeID

	cfg.SMBEnabled = m.SMB.Enabled
	cfg.SMBNBTBinding = orDefault(m.SMB.NBTBinding, cfg.SMBNBTBinding)
	cfg.SMBDirectBinding = m.SMB.DirectBinding
	cfg.SMBGuestOk = m.SMB.GuestOk
	cfg.SMBServerName = orDefault(m.SMB.ServerName, cfg.SMBServerName)
	cfg.SMBWorkgroup = orDefault(m.SMB.Workgroup, cfg.SMBWorkgroup)

	cfg.ShortnameWindowsShortnames = m.Shortname.WindowsShortnames
	cfg.ShortnameBackend = orDefault(m.Shortname.Backend, cfg.ShortnameBackend)
	cfg.ShortnameDBPath = m.Shortname.DBPath

	cfg.WebUI = WebUIConfigOptions{
		Enabled: m.WebUI.Enabled,
		Bind:    orDefault(m.WebUI.Bind, cfg.WebUI.Bind),
		TLS:     m.WebUI.TLS,
		CertPEM: m.WebUI.CertPEM,
		KeyPEM:  m.WebUI.KeyPEM,
	}

	normalizeSMBIdentity(&cfg)
	syncBridgeToEtherTalk(&cfg)
	return cfg, nil
}

// modelFromAppConfig is the inverse of appConfigFromModel: it projects the
// resolved cmd-local appConfig back into a config.Model so the management
// plane has a serialisable, editable view that matches what is running.
// AFP/SMB volume maps are sourced from the model the caller already holds
// (when loaded from file) since appConfig does not carry them.
func modelFromAppConfig(cfg appConfig) *config.Model {
	m := config.Defaults()

	m.Logging.Level = cfg.LogLevel
	m.Logging.LogTraffic = cfg.LogTraffic
	m.Logging.ParsePackets = cfg.ParsePackets
	m.Logging.ParseOutput = cfg.ParseOutput

	m.Bridge.Mode = cfg.Bridge.Mode
	m.Bridge.Device = cfg.Bridge.Device
	m.Bridge.HWAddress = cfg.Bridge.HWAddress
	m.Bridge.BridgeMode = cfg.Bridge.BridgeMode

	m.LToUDP.Enabled = cfg.LToUDP.Enabled
	m.LToUDP.Interface = cfg.LToUDP.Interface
	m.LToUDP.SeedNetwork = cfg.LToUDP.SeedNetwork
	m.LToUDP.SeedZone = cfg.LToUDP.SeedZone

	m.TashTalk.Port = cfg.TashTalk.Port
	m.TashTalk.SeedNetwork = cfg.TashTalk.SeedNetwork
	m.TashTalk.SeedZone = cfg.TashTalk.SeedZone

	m.EtherTalk.BridgeHostMAC = cfg.EtherTalk.BridgeHostMAC
	m.EtherTalk.Filter = cfg.EtherTalk.Filter
	m.EtherTalk.SeedNetworkMin = cfg.EtherTalk.SeedNetworkMin
	m.EtherTalk.SeedNetworkMax = cfg.EtherTalk.SeedNetworkMax
	m.EtherTalk.SeedZone = cfg.EtherTalk.SeedZone
	m.EtherTalk.DesiredNetwork = cfg.EtherTalk.DesiredNetwork
	m.EtherTalk.DesiredNode = cfg.EtherTalk.DesiredNode

	m.Capture.LocalTalk = cfg.Capture.LocalTalk
	m.Capture.EtherTalk = cfg.Capture.EtherTalk
	m.Capture.IPX = cfg.Capture.IPX
	m.Capture.NetBEUI = cfg.Capture.NetBEUI
	m.Capture.Snaplen = cfg.Capture.Snaplen

	m.MacIP.Enabled = cfg.MacIPEnabled
	if cfg.MacIPNAT {
		m.MacIP.Mode = "nat"
	} else {
		m.MacIP.Mode = "pcap"
	}
	m.MacIP.NATSubnet = cfg.MacIPSubnet
	m.MacIP.NATGW = cfg.MacIPGWIP
	m.MacIP.Nameserver = cfg.MacIPNameserver
	m.MacIP.IPGateway = cfg.MacIPGatewayIP
	m.MacIP.DHCPRelay = cfg.MacIPDHCPRelay
	m.MacIP.LeaseFile = cfg.MacIPLeaseFile
	m.MacIP.Zone = cfg.MacIPZone
	m.MacIP.Filter = cfg.MacIPFilter

	m.IPX.Enabled = cfg.IPXEnabled
	m.IPX.Interface = cfg.IPXInterface
	m.IPX.Framing = cfg.IPXFraming
	m.IPX.InternalNetwork = cfg.IPXInternalNetwork
	m.IPX.Filter = cfg.IPXFilter

	m.IPXGW.Enabled = cfg.IPXGWEnabled
	for _, b := range cfg.IPXGWBindings {
		m.IPXGW.Bindings = append(m.IPXGW.Bindings, b.Object+":"+b.Zone)
	}

	m.NetBEUI.Enabled = cfg.NetBEUIEnabled
	m.NetBEUI.Interface = cfg.NetBEUIInterface
	m.NetBEUI.Filter = cfg.NetBEUIFilter

	m.NetBIOS.Enabled = cfg.NetBIOSEnabled
	m.NetBIOS.Transports = cfg.NetBIOSTransports
	m.NetBIOS.ScopeID = cfg.NetBIOSScopeID

	m.SMB.Enabled = cfg.SMBEnabled
	m.SMB.NBTBinding = cfg.SMBNBTBinding
	m.SMB.DirectBinding = cfg.SMBDirectBinding
	m.SMB.GuestOk = cfg.SMBGuestOk
	m.SMB.ServerName = cfg.SMBServerName
	m.SMB.Workgroup = cfg.SMBWorkgroup

	m.Shortname.WindowsShortnames = cfg.ShortnameWindowsShortnames
	m.Shortname.Backend = cfg.ShortnameBackend
	m.Shortname.DBPath = cfg.ShortnameDBPath

	m.WebUI.Enabled = cfg.WebUI.Enabled
	m.WebUI.Bind = cfg.WebUI.Bind
	m.WebUI.TLS = cfg.WebUI.TLS
	m.WebUI.CertPEM = cfg.WebUI.CertPEM
	m.WebUI.KeyPEM = cfg.WebUI.KeyPEM

	return m
}

func parseIPXGWBindings(raw []string) []IPXGWZoneBinding {
	var out []IPXGWZoneBinding
	for _, b := range raw {
		parts := splitColon(b)
		if len(parts) == 2 {
			out = append(out, IPXGWZoneBinding{Object: parts[0], Zone: parts[1]})
		}
	}
	return out
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func splitColon(s string) []string {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return []string{s[:i], s[i+1:]}
		}
	}
	return []string{s}
}
