package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/config"
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
	s.registerServiceStatus("Router", true, map[string]string{"zone": cfg.EtherTalk.SeedZone})

	macIP, err := wireMacIP(MacIPConfig{
		Enabled:         cfg.MacIPEnabled,
		BridgeMode:      cfg.Bridge.Mode,
		BridgeDevice:    cfg.Bridge.Device,
		BridgeHWAddress: cfg.Bridge.HWAddress,
		BridgeFrameMode: cfg.Bridge.BridgeMode,
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
		return nil, fmt.Errorf("Shortname wiring failed: %w", err)
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
		BridgeMode:      cfg.Bridge.Mode,
		BridgeFrameMode: cfg.Bridge.BridgeMode,
		Interface:       ipxResolvedIface,
		BridgeHWAddress: cfg.Bridge.HWAddress,
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
		BridgeMode:      cfg.Bridge.Mode,
		BridgeFrameMode: cfg.Bridge.BridgeMode,
		Interface:       nbeuiResolvedIface,
		BridgeHWAddress: cfg.Bridge.HWAddress,
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

	// Register hooks in dependency/start order. NetBIOS depends on the
	// transports (IPX/NetBEUI); SMB depends on NetBIOS.
	s.addHook("IPX", ipxHook, cfg.IPXEnabled, nil)
	s.addHook("NetBEUI", nbeuiHook, cfg.NetBEUIEnabled, nil)
	s.addHook("NetBIOS", nbHook, cfg.NetBIOSEnabled, []string{"IPX", "NetBEUI"})
	s.addHook("SMB", smbHook, cfg.SMBEnabled, []string{"NetBIOS"})
	if smbHook != nil {
		s.registerSMBStatus(cfg.SMBEnabled) // enrich the SMB unit with shares/identity
	}
	return nil
}

func (s *Supervisor) resolveIPXInterface() string {
	cfg := s.cfg
	iface := cfg.IPXInterface
	if cfg.IPXEnabled && strings.TrimSpace(iface) == "" && cfg.EtherTalk.Device != "" {
		if cfg.Bridge.Device != "" {
			iface = cfg.Bridge.Device
		} else {
			iface = cfg.EtherTalk.Device
		}
	}
	return iface
}

func (s *Supervisor) resolveNetBEUIInterface() string {
	cfg := s.cfg
	iface := cfg.NetBEUIInterface
	if cfg.NetBEUIEnabled && strings.TrimSpace(iface) == "" && cfg.EtherTalk.Device != "" {
		if cfg.Bridge.Device != "" {
			iface = cfg.Bridge.Device
		} else {
			iface = cfg.EtherTalk.Device
		}
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
		Name:       "SMB",
		Kind:       status.KindHook,
		Enabled:    enabled,
		Binding:    m.NBTBinding,
		Properties: map[string]string{"workgroup": m.Workgroup},
		Hostnames:  hostnames,
		Shares:     shares,
		DependsOn:  []string{"NetBIOS"},
	})
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
