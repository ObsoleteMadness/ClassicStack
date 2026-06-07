package config

import (
	"strings"

	"github.com/knadh/koanf/v2"
)

// FromSource builds a Model from a parsed koanf Source. It reads the same
// keys the cmd-layer loader consumes so a Model produced here is equivalent
// to the running configuration. Unknown keys are ignored; missing keys keep
// the Model's zero values (callers seed defaults via Defaults first).
func FromSource(src Source) *Model {
	m := Defaults()
	k := src.K
	if k == nil {
		return m
	}

	m.Logging.Level = str(k, "Logging.level", m.Logging.Level)
	m.Logging.ParsePackets = boolv(k, "Logging.parse_packets", m.Logging.ParsePackets)
	m.Logging.LogTraffic = boolv(k, "Logging.log_traffic", m.Logging.LogTraffic)
	m.Logging.ParseOutput = str(k, "Logging.parse_output", m.Logging.ParseOutput)

	if k.Exists("Router.ports") {
		m.Router.Ports = k.Strings("Router.ports")
	}

	m.Bridge.Mode = str(k, "Bridge.mode", m.Bridge.Mode)
	m.Bridge.Device = str(k, "Bridge.device", m.Bridge.Device)
	m.Bridge.HWAddress = str(k, "Bridge.hw_address", m.Bridge.HWAddress)
	m.Bridge.BridgeMode = str(k, "Bridge.bridge_mode", m.Bridge.BridgeMode)

	m.LToUDP.Enabled = boolv(k, "LToUdp.enabled", m.LToUDP.Enabled)
	m.LToUDP.Interface = str(k, "LToUdp.interface", m.LToUDP.Interface)
	m.LToUDP.SeedNetwork = uintv(k, "LToUdp.seed_network", m.LToUDP.SeedNetwork)
	m.LToUDP.SeedZone = str(k, "LToUdp.seed_zone", m.LToUDP.SeedZone)

	m.TashTalk.Port = str(k, "TashTalk.port", m.TashTalk.Port)
	m.TashTalk.SeedNetwork = uintv(k, "TashTalk.seed_network", m.TashTalk.SeedNetwork)
	m.TashTalk.SeedZone = str(k, "TashTalk.seed_zone", m.TashTalk.SeedZone)

	m.EtherTalk.BridgeHostMAC = str(k, "EtherTalk.bridge_host_mac", m.EtherTalk.BridgeHostMAC)
	m.EtherTalk.Filter = str(k, "EtherTalk.filter", m.EtherTalk.Filter)
	m.EtherTalk.SeedNetworkMin = uintv(k, "EtherTalk.seed_network_min", m.EtherTalk.SeedNetworkMin)
	m.EtherTalk.SeedNetworkMax = uintv(k, "EtherTalk.seed_network_max", m.EtherTalk.SeedNetworkMax)
	m.EtherTalk.SeedZone = str(k, "EtherTalk.seed_zone", m.EtherTalk.SeedZone)
	m.EtherTalk.DesiredNetwork = uintv(k, "EtherTalk.desired_network", m.EtherTalk.DesiredNetwork)
	m.EtherTalk.DesiredNode = uintv(k, "EtherTalk.desired_node", m.EtherTalk.DesiredNode)

	m.Capture.LocalTalk = str(k, "Capture.localtalk", m.Capture.LocalTalk)
	m.Capture.EtherTalk = str(k, "Capture.ethertalk", m.Capture.EtherTalk)
	m.Capture.IPX = str(k, "Capture.ipx", m.Capture.IPX)
	m.Capture.NetBEUI = str(k, "Capture.netbeui", m.Capture.NetBEUI)
	if k.Exists("Capture.snaplen") {
		m.Capture.Snaplen = uint32(k.Int64("Capture.snaplen"))
	}

	m.MacIP.Enabled = boolv(k, "MacIP.enabled", m.MacIP.Enabled)
	m.MacIP.Mode = str(k, "MacIP.mode", m.MacIP.Mode)
	m.MacIP.Zone = str(k, "MacIP.zone", m.MacIP.Zone)
	m.MacIP.NATSubnet = str(k, "MacIP.nat_subnet", m.MacIP.NATSubnet)
	m.MacIP.NATGW = str(k, "MacIP.nat_gw", m.MacIP.NATGW)
	m.MacIP.LeaseFile = str(k, "MacIP.lease_file", m.MacIP.LeaseFile)
	m.MacIP.IPGateway = str(k, "MacIP.ip_gateway", m.MacIP.IPGateway)
	m.MacIP.DHCPRelay = boolv(k, "MacIP.dhcp_relay", m.MacIP.DHCPRelay)
	m.MacIP.Nameserver = str(k, "MacIP.nameserver", m.MacIP.Nameserver)
	m.MacIP.Filter = str(k, "MacIP.filter", m.MacIP.Filter)
	m.MacIP.Custom = loadCustomInterface(k, "MacIP")

	m.IPX.Enabled = boolv(k, "IPX.enabled", m.IPX.Enabled)
	m.IPX.Interface = str(k, "IPX.interface", m.IPX.Interface)
	m.IPX.Framing = str(k, "IPX.framing", m.IPX.Framing)
	m.IPX.InternalNetwork = str(k, "IPX.internal_network", m.IPX.InternalNetwork)
	m.IPX.Filter = str(k, "IPX.filter", m.IPX.Filter)
	m.IPX.Custom = loadCustomInterface(k, "IPX")

	m.IPXGW.Enabled = boolv(k, "IPXGW.enabled", m.IPXGW.Enabled)
	if k.Exists("IPXGW.bindings") {
		m.IPXGW.Bindings = k.Strings("IPXGW.bindings")
	}

	m.NetBEUI.Enabled = boolv(k, "NetBEUI.enabled", m.NetBEUI.Enabled)
	m.NetBEUI.Interface = str(k, "NetBEUI.interface", m.NetBEUI.Interface)
	m.NetBEUI.Filter = str(k, "NetBEUI.filter", m.NetBEUI.Filter)
	m.NetBEUI.Custom = loadCustomInterface(k, "NetBEUI")

	m.NetBIOS.Enabled = boolv(k, "NetBIOS.enabled", m.NetBIOS.Enabled)
	if k.Exists("NetBIOS.transports") {
		m.NetBIOS.Transports = k.Strings("NetBIOS.transports")
	}
	m.NetBIOS.ScopeID = str(k, "NetBIOS.scope_id", m.NetBIOS.ScopeID)

	m.SMB.Enabled = boolv(k, "SMB.enabled", m.SMB.Enabled)
	m.SMB.NBTBinding = str(k, "SMB.nbt_binding", m.SMB.NBTBinding)
	m.SMB.DirectBinding = str(k, "SMB.direct_binding", m.SMB.DirectBinding)
	m.SMB.GuestOk = boolv(k, "SMB.guest_ok", m.SMB.GuestOk)
	m.SMB.ServerName = str(k, "SMB.server_name", m.SMB.ServerName)
	m.SMB.Workgroup = str(k, "SMB.workgroup", m.SMB.Workgroup)
	m.SMB.Volumes = loadShares(k)

	m.AFP.Enabled = boolv(k, "AFP.enabled", m.AFP.Enabled)
	m.AFP.Name = str(k, "AFP.name", m.AFP.Name)
	m.AFP.Zone = str(k, "AFP.zone", m.AFP.Zone)
	m.AFP.Protocols = str(k, "AFP.protocols", m.AFP.Protocols)
	m.AFP.Binding = str(k, "AFP.binding", m.AFP.Binding)
	m.AFP.ExtensionMap = str(k, "AFP.extension_map", m.AFP.ExtensionMap)
	m.AFP.CNIDBackend = str(k, "AFP.cnid_backend", m.AFP.CNIDBackend)
	m.AFP.UseDecomposedNames = boolv(k, "AFP.use_decomposed_names", m.AFP.UseDecomposedNames)
	m.AFP.AppleDoubleMode = str(k, "AFP.appledouble_mode", m.AFP.AppleDoubleMode)
	m.AFP.Volumes = loadVolumes(k)

	m.Shortname.WindowsShortnames = boolv(k, "Shortname.windows_shortnames", m.Shortname.WindowsShortnames)
	m.Shortname.Backend = str(k, "Shortname.backend", m.Shortname.Backend)
	m.Shortname.DBPath = str(k, "Shortname.db_path", m.Shortname.DBPath)

	m.WebUI.Enabled = boolv(k, "WebUI.enabled", m.WebUI.Enabled)
	m.WebUI.Bind = str(k, "WebUI.bind", m.WebUI.Bind)
	m.WebUI.TLS = boolv(k, "WebUI.tls", m.WebUI.TLS)
	m.WebUI.CertPEM = str(k, "WebUI.cert_pem", m.WebUI.CertPEM)
	m.WebUI.KeyPEM = str(k, "WebUI.key_pem", m.WebUI.KeyPEM)

	return m
}

func loadShares(k *koanf.Koanf) map[string]ShareModel {
	prefix := ""
	switch {
	case k.Exists("SMB.Volumes"):
		prefix = "SMB.Volumes"
	case k.Exists("SMB.Shares"):
		prefix = "SMB.Shares"
	default:
		return nil
	}
	keys := k.MapKeys(prefix)
	if len(keys) == 0 {
		return nil
	}
	out := make(map[string]ShareModel, len(keys))
	for _, key := range keys {
		base := prefix + "." + key
		out[key] = ShareModel{
			Name:     str(k, base+".name", key),
			Path:     str(k, base+".path", ""),
			FSType:   str(k, base+".fs_type", "local_fs"),
			ReadOnly: boolv(k, base+".read_only", false),
		}
	}
	return out
}

func loadVolumes(k *koanf.Koanf) map[string]VolumeModel {
	if !k.Exists("AFP.Volumes") {
		return nil
	}
	keys := k.MapKeys("AFP.Volumes")
	if len(keys) == 0 {
		return nil
	}
	out := make(map[string]VolumeModel, len(keys))
	for _, key := range keys {
		base := "AFP.Volumes." + key
		out[key] = VolumeModel{
			Name:             str(k, base+".name", key),
			Path:             str(k, base+".path", ""),
			FSType:           str(k, base+".fs_type", ""),
			Password:         str(k, base+".password", ""),
			ReadOnly:         boolv(k, base+".read_only", false),
			RebuildDesktopDB: boolv(k, base+".rebuild_desktop_db", false),
			AppleDoubleMode:  str(k, base+".appledouble_mode", ""),
		}
	}
	return out
}

// loadCustomInterface reads a protocol's [<Section>.Custom] sub-table into an
// InterfaceModel. It returns nil when the sub-table is absent, meaning the
// protocol inherits the shared [Bridge] interface.
func loadCustomInterface(k *koanf.Koanf, section string) *InterfaceModel {
	base := section + ".Custom"
	if !k.Exists(base) {
		return nil
	}
	return &InterfaceModel{
		Mode:       str(k, base+".mode", ""),
		Device:     str(k, base+".device", ""),
		HWAddress:  str(k, base+".hw_address", ""),
		BridgeMode: str(k, base+".bridge_mode", ""),
	}
}

func str(k *koanf.Koanf, path, def string) string {
	if !k.Exists(path) {
		return def
	}
	v := strings.TrimSpace(k.String(path))
	if v == "" {
		return def
	}
	return v
}

func boolv(k *koanf.Koanf, path string, def bool) bool {
	if !k.Exists(path) {
		return def
	}
	return k.Bool(path)
}

func uintv(k *koanf.Koanf, path string, def uint) uint {
	if !k.Exists(path) {
		return def
	}
	return uint(k.Int64(path))
}
