package app

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/config"
	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/pkg/hwaddr"
	"github.com/ObsoleteMadness/ClassicStack/pkg/status"
	"github.com/ObsoleteMadness/ClassicStack/port"
	"github.com/ObsoleteMadness/ClassicStack/port/ethertalk"
	"github.com/ObsoleteMadness/ClassicStack/port/localtalk"
	"github.com/ObsoleteMadness/ClassicStack/router"
	"github.com/ObsoleteMadness/ClassicStack/service"
	"github.com/ObsoleteMadness/ClassicStack/service/aep"
	"github.com/ObsoleteMadness/ClassicStack/service/llap"
	"github.com/ObsoleteMadness/ClassicStack/service/rtmp"
	"github.com/ObsoleteMadness/ClassicStack/service/zip"
)

// hook is the common lifecycle of the standalone (non-DDP) subsystems —
// IPX, NetBEUI, NetBIOS, SMB, and the Web UI. They each own their own
// listener/router and are driven directly by the supervisor rather than
// through the AppleTalk router's service set.
type hook interface {
	Start(ctx context.Context) error
	Stop() error
}

// Supervisor owns the whole running stack: the ports, the AppleTalk router
// (and its DDP service set), and the standalone hooks. main.go is reduced
// to building configuration and handing it here; everything that
// constructs, starts, or stops a component lives in this file so the same
// logic is reachable from process startup and from the management UI.
type Supervisor struct {
	cfg    appConfig
	source config.Source
	model  *config.Model
	reg    *status.Registry

	mu        sync.Mutex
	ctx       context.Context
	router    *router.Router
	ports     []port.Port
	portNames []string        // status-unit name per entry in ports
	hooks     map[string]hook // name -> standalone hook (ipx, netbeui, …)
	order     []string        // hook start order; stop walks it in reverse
	started   bool

	// captureSinks are closed on Stop.
	captureSinks []closer
	// parseCleanup closes the parse-packets output file, if any.
	parseCleanup func()
	// alreadyRunning marks hooks that are live before Start is called (the
	// Web UI preserved across an Apply rebuild), so Start does not restart
	// them. Cleared after the first Start.
	alreadyRunning map[string]bool

	// nbp is shared between several services; kept so restarts can re-wire.
	nbp *zip.NameInformationService

	// Cross-wired components kept so hooks/services can reference them.
	shortHook ShortnameHook
	macIP     MacIPHook
	ipxGW     IPXGWHook

	// netbios is the NetBIOS hook so the lifecycle can attach/detach
	// transports as their underlying protocol starts/stops. nil when NetBIOS
	// is disabled.
	netbios NetBIOSHook
	// transportBindings maps a transport-protocol hook name ("IPX",
	// "NetBEUI") to the NetBIOS/SMB bindings it feeds, so stopping that hook
	// detaches only its bindings rather than cascading a full teardown. See
	// supervisor_lifecycle.go.
	transportBindings map[string][]transportBinding
}

// transportBinding describes one runtime binding a transport-protocol hook
// contributes to a higher layer (NetBIOS or SMB). When the hook stops, detach
// is called; when it starts, attach re-establishes the binding against the
// freshly started protocol.
type transportBinding struct {
	// owner is the status-unit name of the layer this binding belongs to
	// ("NetBIOS" or "SMB"), used to refresh that unit's displayed transports.
	owner  string
	attach func() error
	detach func()
}

type closer interface{ Close() error }

// NewSupervisor builds the full stack from cfg (and the raw config source
// for subsystems that read their own sections lazily, like AFP/SMB). It
// constructs but does not start anything; call Start to bring it up.
func NewSupervisor(cfg appConfig, source config.Source, model *config.Model) (*Supervisor, error) {
	s := &Supervisor{
		cfg:    cfg,
		source: source,
		model:  model,
		reg:    status.Default,
		hooks:  make(map[string]hook),
	}
	if err := s.build(); err != nil {
		return nil, err
	}
	return s, nil
}

// Router exposes the AppleTalk router for diagnostics wiring.
func (s *Supervisor) Router() *router.Router { return s.router }

// build constructs ports, the router with its DDP service set, and the
// standalone hooks. It mirrors the wiring that previously lived inline in
// main.go.
func (s *Supervisor) build() error {
	ports, sinks, err := s.buildPorts()
	if err != nil {
		return err
	}
	s.ports = ports
	s.captureSinks = sinks

	services, err := s.buildServices()
	if err != nil {
		s.closeSinks()
		return err
	}

	s.router = router.New("router", ports, services)

	// Traffic logging is driven by config so toggling it from the UI takes
	// effect on Apply. Disabling clears the sink.
	if s.cfg.LogTraffic {
		netlog.SetLogFunc(func(line string) { netlog.Debug("%s", line) })
	} else {
		netlog.SetLogFunc(nil)
	}

	if s.cfg.ParsePackets {
		dumper, cleanup, err := newPacketDumper(s.cfg.ParseOutput)
		if err != nil {
			s.closeSinks()
			return fmt.Errorf("parse-packets: %w", err)
		}
		s.parseCleanup = cleanup
		for _, svc := range services {
			if aware, ok := svc.(service.PacketDumpAware); ok {
				aware.SetPacketDumper(dumper)
			}
		}
	}

	if err := s.buildHooks(); err != nil {
		s.closeSinks()
		return err
	}
	return nil
}

// buildPorts constructs the configured ports and attaches capture sinks.
func (s *Supervisor) buildPorts() ([]port.Port, []closer, error) {
	cfg := s.cfg
	var ports []port.Port
	if cfg.LToUDP.Enabled {
		p := localtalk.NewLtoudpPort(cfg.LToUDP.Interface, uint16(cfg.LToUDP.SeedNetwork), []byte(cfg.LToUDP.SeedZone))
		ports = append(ports, p)
		s.registerPortStatus("LToUDP", p, true, map[string]string{"seed_zone": cfg.LToUDP.SeedZone})
	}
	if cfg.TashTalk.Port != "" {
		p := localtalk.NewTashTalkPort(cfg.TashTalk.Port, uint16(cfg.TashTalk.SeedNetwork), []byte(cfg.TashTalk.SeedZone))
		ports = append(ports, p)
		s.registerPortStatus("TashTalk", p, true, map[string]string{"seed_zone": cfg.TashTalk.SeedZone})
	}
	if cfg.EtherTalk.Device != "" {
		ep, err := s.buildEtherTalkPort()
		if err != nil {
			return nil, nil, err
		}
		ports = append(ports, ep)
		s.registerPortStatus("EtherTalk", ep, true, map[string]string{"device": cfg.EtherTalk.Device, "seed_zone": cfg.EtherTalk.SeedZone})
	}
	if len(ports) == 0 {
		return nil, nil, fmt.Errorf("no ports configured")
	}

	if err := cfg.Capture.Validate(); err != nil {
		return nil, nil, fmt.Errorf("capture config: %w", err)
	}
	sinks := make([]closer, 0)
	for _, snk := range attachCaptureSinks(ports, cfg.Capture) {
		sinks = append(sinks, snk)
	}
	return ports, sinks, nil
}

func (s *Supervisor) buildEtherTalkPort() (port.Port, error) {
	cfg := s.cfg
	hwAddr, err := hwaddr.ParseEthernet(cfg.EtherTalk.HWAddress)
	if err != nil {
		return nil, fmt.Errorf("invalid ethertalk hw-address: %w", err)
	}
	opts := ethertalk.Options{
		InterfaceName:  cfg.EtherTalk.Device,
		HWAddr:         hwAddr.Bytes(),
		SeedNetworkMin: uint16(cfg.EtherTalk.SeedNetworkMin),
		SeedNetworkMax: uint16(cfg.EtherTalk.SeedNetworkMax),
		DesiredNetwork: uint16(cfg.EtherTalk.DesiredNetwork),
		DesiredNode:    uint8(cfg.EtherTalk.DesiredNode),
		SeedZoneNames:  [][]byte{[]byte(cfg.EtherTalk.SeedZone)},
		BridgeMode:     cfg.EtherTalk.BridgeMode,
		Filter:         cfg.EtherTalk.Filter,
	}
	if cfg.EtherTalk.BridgeHostMAC != "" {
		hostMAC, err := hwaddr.ParseEthernet(cfg.EtherTalk.BridgeHostMAC)
		if err != nil {
			return nil, fmt.Errorf("invalid ethertalk bridge-host-mac: %w", err)
		}
		opts.BridgeHostMAC = hostMAC.Bytes()
	}
	switch cfg.EtherTalk.Backend {
	case "", "pcap":
		return ethertalk.NewPcapPort(opts)
	case "tap", "tun":
		return ethertalk.NewTapPort(opts)
	default:
		return nil, fmt.Errorf("unsupported EtherTalk backend: %q", cfg.EtherTalk.Backend)
	}
}

// buildServices constructs the AppleTalk DDP service set plus the optional
// DDP services (MacIP, IPXGW, AFP) that ride the router.
func (s *Supervisor) buildServices() ([]service.Service, error) {
	cfg := s.cfg
	s.nbp = zip.NewNameInformationService()
	services := []service.Service{
		llap.New(),
		aep.New(),
		s.nbp,
		rtmp.NewRoutingTableAgingService(),
		rtmp.NewRespondingService(),
		rtmp.NewSendingService(),
		zip.NewRespondingService(),
		zip.NewSendingService(),
	}
	s.registerServiceStatus("Router", true, map[string]string{
		"zone":          cfg.EtherTalk.SeedZone,
		"parse_packets": boolStr(cfg.ParsePackets),
		"log_traffic":   boolStr(cfg.LogTraffic),
		"captures":      s.activeCaptureSummary(),
	})

	macIP, err := wireMacIP(MacIPConfig{
		Enabled:         cfg.MacIPEnabled,
		BridgeMode:      cfg.MacIPBridge.Mode,
		BridgeDevice:    cfg.MacIPBridge.Device,
		BridgeHWAddress: cfg.MacIPBridge.HWAddress,
		BridgeFrameMode: cfg.MacIPBridge.BridgeMode,
		NATGatewayIP:    cfg.MacIPGWIP,
		NATSubnet:       cfg.MacIPSubnet,
		Nameserver:      cfg.MacIPNameserver,
		Zone:            cfg.MacIPZone,
		IPGateway:       cfg.MacIPGatewayIP,
		NAT:             cfg.MacIPNAT,
		DHCPRelay:       cfg.MacIPDHCPRelay,
		StateFile:       cfg.MacIPLeaseFile,
		Filter:          cfg.MacIPFilter,
		EtherTalkZone:   cfg.EtherTalk.SeedZone,
		NBP:             s.nbp,
	})
	if err != nil {
		return nil, fmt.Errorf("MacIP wiring failed: %w", err)
	}
	if macIP != nil {
		services = append(services, macIP.Service())
		s.registerServiceStatus("MacIP", cfg.MacIPEnabled, nil)
	}

	ipxGW, err := wireIPXGW(IPXGWConfig{
		Enabled:  cfg.IPXGWEnabled,
		Bindings: cfg.IPXGWBindings,
		NBP:      s.nbp,
	})
	if err != nil {
		return nil, fmt.Errorf("IPXGW wiring failed: %w", err)
	}
	if ipxGW != nil {
		services = append(services, ipxGW.Service())
		s.registerServiceStatus("IPXGW", cfg.IPXGWEnabled, nil)
	}

	shortHook, err := wireShortname(ShortnameConfig{
		WindowsShortnames: cfg.ShortnameWindowsShortnames,
		Backend:           cfg.ShortnameBackend,
		DBPath:            cfg.ShortnameDBPath,
	})
	if err != nil {
		return nil, fmt.Errorf("shortname wiring failed: %w", err)
	}
	s.shortHook = shortHook

	// AFP is built from the editable config model (not re-read from the
	// TOML source) so volume edits made in the web UI take effect on Apply.
	afpHook, err := wireAFP(AFPWiring{
		Source:     s.source,
		FromConfig: false,
		NBP:        s.nbp,
		Shortname:  shortHook,
		Flags:      s.afpFlagInputs(),
	})
	if err != nil {
		return nil, fmt.Errorf("AFP wiring failed: %w", err)
	}
	if macIP != nil {
		afpHook.AttachMacIP(macIPAFPHooks{macIP})
	}
	services = append(services, afpHook.Services()...)
	s.registerAFPStatus()

	s.macIP = macIP
	s.ipxGW = ipxGW
	return services, nil
}

// afpFlagInputs derives AFP flag inputs from the config model so AFP wiring
// works whether the config came from a file or flags.
func (s *Supervisor) afpFlagInputs() AFPFlagInputs {
	m := s.model
	extMap := m.AFP.ExtensionMap
	if extMap != "" && !filepath.IsAbs(extMap) && s.source.ConfigDir != "" {
		extMap = filepath.Join(s.source.ConfigDir, extMap)
	}
	vols := make([]config.VolumeModel, 0, len(m.AFP.Volumes))
	for key, v := range m.AFP.Volumes {
		if v.Name == "" {
			v.Name = key
		}
		vols = append(vols, v)
	}
	return AFPFlagInputs{
		ServerName:      m.AFP.Name,
		Zone:            m.AFP.Zone,
		Protocols:       m.AFP.Protocols,
		TCPAddr:         m.AFP.Binding,
		ExtensionMap:    extMap,
		DecomposedNames: m.AFP.UseDecomposedNames,
		CNIDBackend:     m.AFP.CNIDBackend,
		AppleDoubleMode: m.AFP.AppleDoubleMode,
		VolumeModels:    vols,
	}
}

// buildHooks constructs the standalone hooks (IPX, NetBEUI, NetBIOS, SMB,
// WebUI) and records them as named units in start order.
func (s *Supervisor) buildHooks() error {
	cfg := s.cfg

	ipxResolvedIface := s.resolveIPXInterface()
	ipxHook, err := wireIPX(IPXConfig{
		Enabled:         cfg.IPXEnabled,
		BridgeMode:      cfg.IPXBridge.Mode,
		BridgeFrameMode: cfg.IPXBridge.BridgeMode,
		Interface:       ipxResolvedIface,
		BridgeHWAddress: cfg.IPXBridge.HWAddress,
		Framing:         cfg.IPXFraming,
		InternalNetwork: cfg.IPXInternalNetwork,
		Filter:          cfg.IPXFilter,
		CapturePath:     cfg.Capture.IPX,
		CaptureSnaplen:  cfg.Capture.Snaplen,
	})
	if err != nil {
		return fmt.Errorf("IPX wiring failed: %w", err)
	}
	if s.ipxGW != nil && ipxHook != nil {
		s.ipxGW.AttachIPXRouter(ipxHook.Router())
	}

	nbeuiResolvedIface := s.resolveNetBEUIInterface()
	nbeuiHook, err := wireNetBEUI(NetBEUIConfig{
		Enabled:         cfg.NetBEUIEnabled,
		BridgeMode:      cfg.NetBEUIBridge.Mode,
		BridgeFrameMode: cfg.NetBEUIBridge.BridgeMode,
		Interface:       nbeuiResolvedIface,
		BridgeHWAddress: cfg.NetBEUIBridge.HWAddress,
		Filter:          cfg.NetBEUIFilter,
		CapturePath:     cfg.Capture.NetBEUI,
		CaptureSnaplen:  cfg.Capture.Snaplen,
	})
	if err != nil {
		return fmt.Errorf("NetBEUI wiring failed: %w", err)
	}

	nbHook, err := wireNetBIOS(NetBIOSConfig{
		Enabled:    cfg.NetBIOSEnabled,
		Transports: cfg.NetBIOSTransports,
		ScopeID:    cfg.NetBIOSScopeID,
		ServerName: cfg.NetBIOSServerName,
		Workgroup:  cfg.NetBIOSWorkgroup,
		IPX:        ipxHook,
		NetBEUI:    nbeuiHook,
	})
	if err != nil {
		return fmt.Errorf("NetBIOS wiring failed: %w", err)
	}

	// SMB shares come from the editable model so UI edits apply on Apply.
	smbShareConfigs := smbSharesFromModel(s.model.SMB.Volumes)
	if len(smbShareConfigs) == 0 {
		smbShareConfigs = loadSMBShares(s.source, s.source.K != nil, cfg.SMBShareFlags)
	}
	smbHook, err := wireSMB(SMBConfig{
		Enabled:       cfg.SMBEnabled,
		NBTBinding:    cfg.SMBNBTBinding,
		DirectBinding: cfg.SMBDirectBinding,
		GuestOk:       cfg.SMBGuestOk,
		Workgroup:     cfg.SMBWorkgroup,
		ServerName:    cfg.SMBServerName,
		Shares:        smbShareConfigs,
		NetBIOS:       nbHook,
		IPX:           ipxHook,
		Shortname:     s.shortHook,
	})
	if err != nil {
		return fmt.Errorf("SMB wiring failed: %w", err)
	}

	// Register hooks in start order. NetBIOS is NOT a hard dependent of the
	// transports: IPX/NetBEUI are bindings into NetBIOS, so stopping one
	// detaches just that transport (see transportBindings) rather than
	// tearing NetBIOS (and SMB) down. SMB does depend on NetBIOS.
	s.addHook("IPX", ipxHook, cfg.IPXEnabled, nil)
	s.addHook("NetBEUI", nbeuiHook, cfg.NetBEUIEnabled, nil)
	s.addHook("NetBIOS", nbHook, cfg.NetBIOSEnabled, nil)
	s.addHook("SMB", smbHook, cfg.SMBEnabled, []string{"NetBIOS"})

	if cfg.NetBIOSEnabled && nbHook != nil {
		s.netbios = nbHook
	}
	s.registerIPXStatus(ipxHook, cfg.IPXEnabled)
	s.registerNetBEUIStatus(nbeuiHook, cfg.NetBEUIEnabled)
	if nbHook != nil {
		s.refreshNetBIOSStatus(cfg.NetBIOSEnabled)
	}
	if smbHook != nil {
		s.registerSMBStatus(cfg.SMBEnabled) // enrich the SMB unit with shares/identity
	}
	s.registerTransportBindings(ipxHook, nbeuiHook, smbHook)
	return nil
}

// registerTransportBindings records, for each transport-protocol hook, the
// runtime bindings it contributes to NetBIOS (and SMB's direct-IPX path), so
// the lifecycle can detach/reattach them when that protocol is stopped or
// started from the UI without cascading a full teardown.
func (s *Supervisor) registerTransportBindings(ipxHook IPXHook, nbeuiHook NetBEUIHook, smbHook SMBHook) {
	s.transportBindings = map[string][]transportBinding{}

	// NetBEUI -> NetBIOS "netbeui" transport.
	if nbeuiHook != nil && s.netbios != nil {
		s.transportBindings["NetBEUI"] = append(s.transportBindings["NetBEUI"],
			s.netbiosTransportBinding("netbeui"))
	}
	// IPX -> NetBIOS "ipx" transport.
	if ipxHook != nil && s.netbios != nil {
		s.transportBindings["IPX"] = append(s.transportBindings["IPX"],
			s.netbiosTransportBinding("ipx"))
	}
	// IPX -> SMB direct-IPX transport.
	if ipxHook != nil && smbHook != nil {
		if d := smbHook.IPXDirect(); d != nil {
			s.transportBindings["IPX"] = append(s.transportBindings["IPX"], transportBinding{
				owner:  "SMB",
				attach: func() error { return d.Start(s.ctx) },
				detach: func() { _ = d.Stop() },
			})
		}
	}
}

// netbiosTransportBinding builds a transportBinding that adds/removes the
// named NetBIOS transport (rebuilding it from the NetBIOS hook so it re-binds
// to the freshly started protocol).
func (s *Supervisor) netbiosTransportBinding(name string) transportBinding {
	return transportBinding{
		owner: "NetBIOS",
		attach: func() error {
			if s.netbios == nil {
				return nil
			}
			t := s.netbios.BuildTransport(name)
			if t == nil {
				return nil
			}
			return s.netbios.Service().AddTransport(name, t)
		},
		detach: func() {
			if s.netbios != nil {
				_ = s.netbios.Service().RemoveTransport(name)
			}
		},
	}
}

func (s *Supervisor) resolveIPXInterface() string {
	cfg := s.cfg
	// cfg.IPXBridge.Device already folds in the protocol's own [IPX.Custom]
	// device, the legacy scalar interface, and the shared bridge device.
	iface := cfg.IPXBridge.Device
	if cfg.IPXEnabled && strings.TrimSpace(iface) == "" && cfg.EtherTalk.Device != "" {
		iface = cfg.EtherTalk.Device
	}
	return iface
}

func (s *Supervisor) resolveNetBEUIInterface() string {
	cfg := s.cfg
	iface := cfg.NetBEUIBridge.Device
	if cfg.NetBEUIEnabled && strings.TrimSpace(iface) == "" && cfg.EtherTalk.Device != "" {
		iface = cfg.EtherTalk.Device
	}
	return iface
}

// addHook records a standalone hook as a named, restartable unit.
func (s *Supervisor) addHook(name string, h hook, enabled bool, dependsOn []string) {
	if h == nil {
		return
	}
	s.hooks[name] = h
	s.order = append(s.order, name)
	s.reg.Set(status.Unit{
		Name:      name,
		Kind:      status.KindHook,
		Enabled:   enabled,
		DependsOn: dependsOn,
	})
}

func (s *Supervisor) registerPortStatus(name string, p port.Port, enabled bool, props map[string]string) {
	if props == nil {
		props = map[string]string{}
	}
	props["range"] = fmt.Sprintf("%d-%d", p.NetworkMin(), p.NetworkMax())
	s.reg.Set(status.Unit{
		Name:       name,
		Kind:       status.KindPort,
		Enabled:    enabled,
		Binding:    p.ShortString(),
		Properties: props,
	})
	s.portNames = append(s.portNames, name)
}

func (s *Supervisor) registerServiceStatus(name string, enabled bool, props map[string]string) {
	s.reg.Set(status.Unit{
		Name:       name,
		Kind:       status.KindService,
		Enabled:    enabled,
		Properties: props,
	})
}

// registerAFPStatus records AFP's status including its advertised name,
// zone, and the list of shared volumes for the dashboard.
func (s *Supervisor) registerAFPStatus() {
	m := s.model.AFP
	shares := make([]status.ShareInfo, 0, len(m.Volumes))
	for key, v := range m.Volumes {
		name := v.Name
		if name == "" {
			name = key
		}
		shares = append(shares, status.ShareInfo{Name: name, Path: v.Path, ReadOnly: v.ReadOnly})
	}
	s.reg.Set(status.Unit{
		Name:       "AFP",
		Kind:       status.KindService,
		Enabled:    m.Enabled,
		Binding:    m.Binding,
		Properties: map[string]string{"zone": m.Zone},
		Hostnames:  []string{m.Name},
		Shares:     shares,
	})
}

// registerSMBStatus records SMB's identity and shares for the dashboard.
// SMB has no TCP listener today (NBT :139 / direct :445 are unimplemented), so
// the displayed binding is the set of transports it is actually served over:
// NetBIOS (and which NetBIOS transports are live) plus the direct-IPX path.
func (s *Supervisor) registerSMBStatus(enabled bool) {
	m := s.model.SMB
	shares := make([]status.ShareInfo, 0, len(m.Volumes))
	for key, sh := range m.Volumes {
		name := sh.Name
		if name == "" {
			name = key
		}
		shares = append(shares, status.ShareInfo{Name: name, Path: sh.Path, ReadOnly: sh.ReadOnly})
	}
	hostnames := []string{}
	if m.ServerName != "" {
		hostnames = append(hostnames, m.ServerName)
	}
	s.reg.Set(status.Unit{
		Name:    "SMB",
		Kind:    status.KindHook,
		Enabled: enabled,
		Properties: map[string]string{
			"workgroup":  m.Workgroup,
			"transports": s.smbTransportSummary(),
		},
		Hostnames: hostnames,
		Shares:    shares,
		DependsOn: []string{"NetBIOS"},
	})
}

// smbTransportSummary describes the transports SMB is currently served over,
// e.g. "NetBIOS (IPX, NetBEUI), IPX-direct". It reflects live state: NetBIOS is
// only listed while it is running, and only the transports it currently has
// bound are shown. The direct-IPX path is listed only while IPX is running.
func (s *Supervisor) smbTransportSummary() string {
	var parts []string
	if s.netbios != nil && s.unitRunning("NetBIOS") {
		if names := s.netbios.Service().Transports(); len(names) > 0 {
			parts = append(parts, "NetBIOS ("+strings.Join(prettyTransportNames(names), ", ")+")")
		} else {
			parts = append(parts, "NetBIOS")
		}
	}
	if smb, ok := s.hooks["SMB"].(SMBHook); ok && smb != nil && smb.IPXDirect() != nil && s.unitRunning("IPX") {
		parts = append(parts, "IPX-direct")
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

// unitRunning reports whether the named status unit is currently marked
// running. Unknown units are treated as not running.
func (s *Supervisor) unitRunning(name string) bool {
	for _, u := range s.reg.Snapshot() {
		if u.Name == name {
			return u.Running
		}
	}
	return false
}

// prettyTransportNames maps canonical transport keys to display labels.
func prettyTransportNames(names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		switch n {
		case "ipx":
			out = append(out, "IPX")
		case "netbeui":
			out = append(out, "NetBEUI")
		case "tcp":
			out = append(out, "TCP")
		default:
			out = append(out, n)
		}
	}
	return out
}

// ipxFramingLabel maps the configured IPX framing name to a display label,
// defaulting to Ethernet II when unset/unknown (matching parseIPXFraming).
func ipxFramingLabel(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "raw_802_3", "raw-802-3", "raw802.3":
		return "Raw 802.3"
	case "llc", "802.2":
		return "802.2 LLC"
	case "snap":
		return "SNAP"
	default:
		return "Ethernet II"
	}
}

// registerIPXStatus records the IPX hook's bound device, network number, and
// framing for the dashboard.
func (s *Supervisor) registerIPXStatus(h IPXHook, enabled bool) {
	if h == nil {
		return
	}
	cfg := s.cfg
	iface := s.resolveIPXInterface()
	props := map[string]string{"device": iface, "framing": ipxFramingLabel(cfg.IPXFraming)}
	// The IPX router carries the resolved network number (the configured
	// internal network, or the router default when unset).
	if r := h.Router(); r != nil {
		net := r.Network()
		props["network"] = fmt.Sprintf("%02x%02x%02x%02x", net[0], net[1], net[2], net[3])
	}
	s.reg.Set(status.Unit{
		Name:       "IPX",
		Kind:       status.KindHook,
		Enabled:    enabled,
		Binding:    iface,
		Properties: props,
	})
}

// registerNetBEUIStatus records the NetBEUI hook's bound device.
func (s *Supervisor) registerNetBEUIStatus(h NetBEUIHook, enabled bool) {
	if h == nil {
		return
	}
	iface := s.resolveNetBEUIInterface()
	s.reg.Set(status.Unit{
		Name:       "NetBEUI",
		Kind:       status.KindHook,
		Enabled:    enabled,
		Binding:    iface,
		Properties: map[string]string{"device": iface},
	})
}

// refreshNetBIOSStatus re-publishes the NetBIOS unit with its current bound
// transports, so the dashboard reflects detach/attach without a full rebuild.
func (s *Supervisor) refreshNetBIOSStatus(enabled bool) {
	if s.netbios == nil {
		return
	}
	// Preserve the live running flag across the re-Set. Transports are only
	// shown while running — a stopped NetBIOS serves nothing even though the
	// bindings are still recorded for the next start.
	running := s.unitRunning("NetBIOS")
	transports := "none"
	if running {
		if names := prettyTransportNames(s.netbios.Service().Transports()); len(names) > 0 {
			transports = strings.Join(names, ", ")
		}
	}
	hostnames := []string{}
	if s.cfg.NetBIOSServerName != "" {
		hostnames = append(hostnames, s.cfg.NetBIOSServerName)
	}
	s.reg.Set(status.Unit{
		Name:       "NetBIOS",
		Kind:       status.KindHook,
		Enabled:    enabled,
		Running:    running,
		Properties: map[string]string{"transports": transports},
		Hostnames:  hostnames,
	})
}

// refreshSMBStatus re-publishes SMB's status (its transport summary changes as
// NetBIOS transports come and go). It preserves the running flag.
func (s *Supervisor) refreshSMBStatus() {
	if _, ok := s.hooks["SMB"]; !ok {
		return
	}
	running := false
	enabled := false
	for _, u := range s.reg.Snapshot() {
		if u.Name == "SMB" {
			running = u.Running
			enabled = u.Enabled
			break
		}
	}
	s.registerSMBStatus(enabled)
	s.reg.SetRunning("SMB", running)
}

// AddExternalHook registers an additional named hook (e.g. the Web UI)
// built outside the standard wiring, so the supervisor starts and stops it
// with the rest of the stack. enabled records its configured state for the
// status dashboard.
func (s *Supervisor) AddExternalHook(name string, h hook, enabled bool) {
	if h == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hooks[name] = h
	s.order = append(s.order, name)
	s.reg.Set(status.Unit{Name: name, Kind: status.KindHook, Enabled: enabled})
}

// boolStr renders a bool as "on"/"off" for status properties.
func boolStr(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// activeCaptureSummary lists the transports with an active pcap capture
// path configured, for the dashboard's packet-dump status.
func (s *Supervisor) activeCaptureSummary() string {
	c := s.cfg.Capture
	var active []string
	for name, path := range map[string]string{
		"localtalk": c.LocalTalk,
		"ethertalk": c.EtherTalk,
		"ipx":       c.IPX,
		"netbeui":   c.NetBEUI,
	} {
		if strings.TrimSpace(path) != "" {
			active = append(active, name)
		}
	}
	if len(active) == 0 {
		return "none"
	}
	sort.Strings(active)
	return strings.Join(active, ",")
}

func (s *Supervisor) closeSinks() {
	for _, c := range s.captureSinks {
		_ = c.Close()
	}
	s.captureSinks = nil
	if s.parseCleanup != nil {
		s.parseCleanup()
		s.parseCleanup = nil
	}
}
