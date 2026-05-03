package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/ObsoleteMadness/ClassicStack/config"
	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/pkg/hwaddr"
	"github.com/ObsoleteMadness/ClassicStack/pkg/logging"
	"github.com/ObsoleteMadness/ClassicStack/port"
	"github.com/ObsoleteMadness/ClassicStack/port/ethertalk"
	"github.com/ObsoleteMadness/ClassicStack/port/localtalk"
	"github.com/ObsoleteMadness/ClassicStack/port/rawlink"
	"github.com/ObsoleteMadness/ClassicStack/router"
	"github.com/ObsoleteMadness/ClassicStack/service"
	"github.com/ObsoleteMadness/ClassicStack/service/aep"
	"github.com/ObsoleteMadness/ClassicStack/service/llap"
	"github.com/ObsoleteMadness/ClassicStack/service/rtmp"
	"github.com/ObsoleteMadness/ClassicStack/service/zip"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	configPath := flag.String("config", "", "Path to TOML config file (cannot be combined with other flags)")
	showVersion := flag.Bool("version", false, "Print ClassicStack version information and exit")

	logLevel := flag.String("log-level", "info", "Minimum log level: debug, info, warn")
	logTraffic := flag.Bool("log-traffic", false, "Log network traffic at debug level (requires -log-level debug)")

	ltoudp := flag.Bool("ltoudp-enabled", true, "Enable LToUDP LocalTalk port")
	ltIface := flag.String("ltoudp-interface", "0.0.0.0", "Local IPv4 interface/address for LToUDP multicast join and send (0.0.0.0 = auto)")
	ltNet := flag.Uint("ltoudp-seed-network", 1, "LToUDP seed network")
	ltZone := flag.String("ltoudp-seed-zone", "LToUDP Network", "LToUDP seed zone")
	tashtalkSerial := flag.String("tashtalk-port", "", "TashTalk serial port (empty to disable)")
	ttNet := flag.Uint("tashtalk-seed-network", 2, "TashTalk seed network")
	ttZone := flag.String("tashtalk-seed-zone", "TashTalk Network", "TashTalk seed zone")

	pcapDev := flag.String("ethertalk-device", "", "EtherTalk pcap device (required for EtherTalk)")
	etBackend := flag.String("ethertalk-backend", "pcap", "EtherTalk backend: pcap, tap, or tun")
	pcapHWAddr := flag.String("ethertalk-hw-address", "DE:AD:BE:EF:CA:FE", "EtherTalk hardware address (6-byte MAC)")
	etBridgeMode := flag.String("ethertalk-bridge-mode", "auto", "EtherTalk bridge mode: auto, ethernet, wifi")
	etBridgeHostMAC := flag.String("ethertalk-bridge-host-mac", "", "Host adapter MAC used for Wi-Fi bridge shim (default: ethertalk-hw-address)")
	listPcap := flag.Bool("list-pcap-devices", false, "List pcap devices and exit")
	etNetMin := flag.Uint("ethertalk-seed-network-min", 3, "EtherTalk seed network min")
	etNetMax := flag.Uint("ethertalk-seed-network-max", 5, "EtherTalk seed network max")
	etZone := flag.String("ethertalk-seed-zone", "EtherTalk Network", "EtherTalk seed zone name")
	etDesiredNet := flag.Uint("ethertalk-desired-network", 3, "EtherTalk desired network")
	etDesiredNode := flag.Uint("ethertalk-desired-node", 253, "EtherTalk desired node")

	// MacIP gateway flags.
	// By default the IP side reuses the same pcap device as EtherTalk (-ethertalk-device).
	// A separate interface can be specified with -macip-interface if needed.
	macipEnable := flag.Bool("macip-enabled", false, "Enable MacIP IP-over-AppleTalk gateway (intended for NAT mode)")
	macipGWIP := flag.String("macip-nat-gw", "", "MacIP gateway IP for NAT mode (ignored in pcap mode; blank uses an APIPA-style address)")
	macipSubnet := flag.String("macip-nat-subnet", "192.168.100.0/24", "MacIP NAT subnet in CIDR notation")
	macipNameserver := flag.String("macip-nameserver", "", "Nameserver IP for MacIP clients (default: IP-side gateway)")
	macipZone := flag.String("macip-zone", "", "AppleTalk zone for NBP registration (default: use -ethertalk-seed-zone if set, otherwise first zone found)")
	macipIPGW := flag.String("macip-ip-gateway", "", "Default gateway IP on the IP-side network (auto-detected when omitted)")
	macipNAT := flag.Bool("macip-nat", false, "Enable NAPT: rewrite Mac client source IPs to the gateway IP on the physical network")
	macipDHCP := flag.Bool("macip-dhcp-relay", false, "Use DHCP to assign IPs to MacIP clients instead of the static pool (non-NAT mode)")
	macipStateFile := flag.String("macip-lease-file", "", "File to persist MacIP lease state across restarts (empty to disable)")

	// Packet parsing / capture flags.
	parsePackets := flag.Bool("parse-packets", false, "Decode and log every inbound DDP packet (ATP/ASP/AFP layers)")
	parseOutput := flag.String("parse-output", "", "File path to write parsed packet log (appended; empty = stdout only)")

	// AFP file sharing flags. Schemas live in service/afp; cmd-side
	// wiring is split between afp_enabled.go and afp_disabled.go.
	afpServerName := flag.String("afp-name", "Go File Server", "AFP server name advertised to clients")
	afpZone := flag.String("afp-zone", "", "AppleTalk zone for AFP NBP registration (default: first zone found)")
	afpProtocols := flag.String("afp-protocols", "tcp,ddp", "AFP protocols to enable: tcp, ddp, or tcp,ddp")
	afpTCPAddr := flag.String("afp-binding", ":548", "Address and port for AFP over TCP (DSI) to listen on")
	afpExtensionMap := flag.String("afp-extension-map", "", "Netatalk-compatible extension map file for Macintosh type/creator fallback")
	afpDecomposedFilenames := flag.Bool("afp-use-decomposed-names", true, "Encode host-reserved filename characters using 0xNN tokens when mapping AFP paths")
	afpCNIDBackend := flag.String("afp-cnid-backend", "sqlite", "CNID backend to use for AFP object IDs (sqlite or memory)")
	afpAppleDoubleMode := flag.String("afp-appledouble-mode", "modern", "AppleDouble metadata mode: modern or legacy")
	var afpVolumes volumeFlags
	flag.Var(&afpVolumes, "afp-volume", `AFP volume to share, format: "Name:Path" (repeatable, e.g. -afp-volume "Mac Share:c:\mac")`)

	flag.Parse()

	if *showVersion {
		fmt.Printf("classicstack %s\n", BuildVersion)
		fmt.Printf("commit: %s\n", BuildCommit)
		fmt.Printf("built: %s\n", BuildDate)
		fmt.Printf("go: %s\n", runtime.Version())
		return
	}

	nonConfigFlags := 0
	flag.Visit(func(f *flag.Flag) {
		if f.Name != "config" && f.Name != "version" {
			nonConfigFlags++
		}
	})

	if *configPath != "" && nonConfigFlags > 0 {
		log.Fatal("-config cannot be combined with other flags")
	}

	selectedConfig := *configPath
	if selectedConfig == "" && flag.NFlag() == 0 {
		if _, err := os.Stat("server.toml"); err == nil {
			selectedConfig = "server.toml"
		} else if os.IsNotExist(err) {
			flag.Usage()
			return
		} else {
			log.Fatalf("failed checking default config file server.toml: %v", err)
		}
	}

	var (
		cfg          appConfig
		configSource config.Source
	)
	fromConfigFile := selectedConfig != ""
	if fromConfigFile {
		loaded, src, err := loadConfigFromFile(selectedConfig)
		if err != nil {
			log.Fatalf("failed loading config file %q: %v", selectedConfig, err)
		}
		cfg = loaded
		configSource = src
	} else {
		cfg = flagsToConfig(flagInputs{
			LogLevel:                *logLevel,
			LogTraffic:              *logTraffic,
			ParsePackets:            *parsePackets,
			ParseOutput:             *parseOutput,
			LToUDPEnabled:           *ltoudp,
			LToUDPInterface:         *ltIface,
			LToUDPSeedNetwork:       *ltNet,
			LToUDPSeedZone:          *ltZone,
			TashTalkPort:            *tashtalkSerial,
			TashTalkSeedNetwork:     *ttNet,
			TashTalkSeedZone:        *ttZone,
			EtherTalkDevice:         *pcapDev,
			EtherTalkBackend:        *etBackend,
			EtherTalkHWAddress:      *pcapHWAddr,
			EtherTalkBridgeMode:     *etBridgeMode,
			EtherTalkBridgeHostMAC:  *etBridgeHostMAC,
			EtherTalkSeedNetworkMin: *etNetMin,
			EtherTalkSeedNetworkMax: *etNetMax,
			EtherTalkSeedZone:       *etZone,
			EtherTalkDesiredNetwork: *etDesiredNet,
			EtherTalkDesiredNode:    *etDesiredNode,
			MacIPEnabled:            *macipEnable,
			MacIPGWIP:               *macipGWIP,
			MacIPSubnet:             *macipSubnet,
			MacIPNameserver:         *macipNameserver,
			MacIPZone:               *macipZone,
			MacIPGatewayIP:          *macipIPGW,
			MacIPNAT:                *macipNAT,
			MacIPDHCPRelay:          *macipDHCP,
			MacIPLeaseFile:          *macipStateFile,
		})
	}

	if level, ok := netlog.ParseLevel(cfg.LogLevel); ok {
		netlog.SetLevel(level)
	} else {
		log.Fatalf("unknown -log-level %q (want debug, info, or warn)", cfg.LogLevel)
	}

	// Install a pkg/logging root logger as the netlog shim's target so
	// output flows through slog with source tagging and structured
	// attributes. Each service will eventually take a *slog.Logger
	// directly; until then, netlog.* calls forward here.
	slogLevel, _ := logging.ParseLevel(cfg.LogLevel)
	rootLogger := logging.New("ClassicStack", logging.Options{
		Sinks: []logging.Sink{{Writer: os.Stderr, Format: logging.FormatConsole, Level: slogLevel}},
	})
	logging.SetDefault(rootLogger)
	netlog.SetLogger(rootLogger)

	if cfg.LogTraffic {
		netlog.SetLogFunc(func(s string) { netlog.Debug("%s", s) })
	}

	cfg.EtherTalk.Backend = strings.ToLower(strings.TrimSpace(cfg.EtherTalk.Backend))
	switch cfg.EtherTalk.Backend {
	case "", "pcap", "tap", "tun":
	default:
		log.Fatalf("invalid -ethertalk-backend %q (want pcap, tap, or tun)", cfg.EtherTalk.Backend)
	}

	if *listPcap {
		names, err := rawlink.InterfaceNames()
		if err != nil {
			log.Fatalf("failed listing pcap interface names: %v", err)
		}
		netlog.Info("[MAIN] available interfaces: %v", names)
		devs, err := rawlink.ListPcapDevices()
		if err != nil {
			log.Fatalf("failed listing pcap devices: %v", err)
		}
		if len(devs) == 0 {
			netlog.Info("[MAIN] no pcap devices found")
			return
		}
		for _, d := range devs {
			netlog.Info("[MAIN] pcap device: %s", d.Name)
			if d.Description != "" {
				netlog.Info("[MAIN]   desc: %s", d.Description)
			}
			for _, addr := range d.Addresses {
				netlog.Info("[MAIN]   addr: %s", addr)
			}
		}
		return
	}

	if cfg.EtherTalk.Device == "" && cfg.EtherTalk.Backend == "pcap" {
		if detected, ok := rawlink.DetectDefaultPcapInterface(); ok {
			netlog.Info("[MAIN] auto-detected pcap interface: %s", detected)
			cfg.EtherTalk.Device = detected
		}
	}
	if cfg.EtherTalk.Device != "" && cfg.EtherTalk.Backend == "pcap" && strings.TrimSpace(cfg.EtherTalk.BridgeHostMAC) == "" {
		if hostMAC, ok := rawlink.DetectHostMACForPcapInterface(cfg.EtherTalk.Device); ok {
			cfg.EtherTalk.BridgeHostMAC = hostMAC
			netlog.Info("[MAIN] auto-detected bridge host MAC for %s: %s", cfg.EtherTalk.Device, hostMAC)
		}
	}

	var ports []port.Port
	if cfg.LToUDP.Enabled {
		ports = append(ports, localtalk.NewLtoudpPort(cfg.LToUDP.Interface, uint16(cfg.LToUDP.SeedNetwork), []byte(cfg.LToUDP.SeedZone)))
	}
	if cfg.TashTalk.Port != "" {
		ports = append(ports, localtalk.NewTashTalkPort(cfg.TashTalk.Port, uint16(cfg.TashTalk.SeedNetwork), []byte(cfg.TashTalk.SeedZone)))
	}
	if cfg.EtherTalk.Device != "" {
		hwAddr, err := hwaddr.ParseEthernet(cfg.EtherTalk.HWAddress)
		if err != nil {
			log.Fatalf("invalid -ethertalk-hw-address: %v", err)
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
		}
		if cfg.EtherTalk.BridgeHostMAC != "" {
			hostMAC, err := hwaddr.ParseEthernet(cfg.EtherTalk.BridgeHostMAC)
			if err != nil {
				log.Fatalf("invalid -ethertalk-bridge-host-mac: %v", err)
			}
			opts.BridgeHostMAC = hostMAC.Bytes()
		}
		var ep port.Port
		switch cfg.EtherTalk.Backend {
		case "", "pcap":
			ep, err = ethertalk.NewPcapPort(opts)
		case "tap", "tun":
			ep, err = ethertalk.NewTapPort(opts)
		default:
			log.Fatalf("unsupported EtherTalk backend: %q", cfg.EtherTalk.Backend)
		}
		if err != nil {
			log.Fatalf("failed creating EtherTalk port (%s): %v", cfg.EtherTalk.Backend, err)
		}
		ports = append(ports, ep)
	}
	if len(ports) == 0 {
		log.Fatal("no ports configured")
	}

	// Build the service list explicitly so we can share the NBP service reference
	// with the MacIP gateway.
	nbpSvc := zip.NewNameInformationService()
	services := []service.Service{
		llap.New(),
		aep.New(),
		nbpSvc,
		rtmp.NewRoutingTableAgingService(),
		rtmp.NewRespondingService(),
		rtmp.NewSendingService(),
		zip.NewRespondingService(),
		zip.NewSendingService(),
	}

	macIP, err := wireMacIP(MacIPConfig{
		Enabled:          cfg.MacIPEnabled,
		NATGatewayIP:     cfg.MacIPGWIP,
		NATSubnet:        cfg.MacIPSubnet,
		Nameserver:       cfg.MacIPNameserver,
		Zone:             cfg.MacIPZone,
		IPGateway:        cfg.MacIPGatewayIP,
		NAT:              cfg.MacIPNAT,
		DHCPRelay:        cfg.MacIPDHCPRelay,
		StateFile:        cfg.MacIPLeaseFile,
		PcapDevice:       cfg.EtherTalk.Device,
		BridgeHostMAC:    cfg.EtherTalk.BridgeHostMAC,
		PcapHWAddr:       cfg.EtherTalk.HWAddress,
		EtherTalkZone:    cfg.EtherTalk.SeedZone,
		EtherTalkBackend: cfg.EtherTalk.Backend,
		NBP:              nbpSvc,
	})
	if err != nil {
		log.Fatalf("MacIP wiring failed: %v", err)
	}
	if macIP != nil {
		services = append(services, macIP.Service())
	}

	afpHook, err := wireAFP(AFPWiring{
		Source:     configSource,
		FromConfig: fromConfigFile,
		NBP:        nbpSvc,
		Flags: AFPFlagInputs{
			ServerName:       *afpServerName,
			Zone:             *afpZone,
			Protocols:        *afpProtocols,
			TCPAddr:          *afpTCPAddr,
			ExtensionMap:     *afpExtensionMap,
			DecomposedNames:  *afpDecomposedFilenames,
			CNIDBackend:      *afpCNIDBackend,
			AppleDoubleMode:  *afpAppleDoubleMode,
			VolumeFlagValues: []string(afpVolumes),
		},
	})
	if err != nil {
		log.Fatalf("AFP wiring failed: %v", err)
	}
	if macIP != nil {
		afpHook.AttachMacIP(macIPAFPHooks{macIP})
	}
	services = append(services, afpHook.Services()...)

	r := router.New("router", ports, services)

	if cfg.ParsePackets {
		dumper, cleanup, err := newPacketDumper(cfg.ParseOutput)
		if err != nil {
			log.Fatalf("parse-packets: %v", err)
		}
		defer cleanup()
		for _, svc := range services {
			if aware, ok := svc.(service.PacketDumpAware); ok {
				aware.SetPacketDumper(dumper)
			}
		}
		netlog.Info("[MAIN] parse-packets enabled; output=%q", cfg.ParseOutput)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := r.Start(ctx); err != nil {
		log.Fatalf("failed to start router: %v", err)
	}
	netlog.Info("[MAIN] router away!")

	<-ctx.Done()

	if err := r.Stop(); err != nil {
		netlog.Warn("[MAIN] stop warning: %v", err)
	}
}

// broadcastAddr computes the broadcast address of an IP network.
func broadcastAddr(n *net.IPNet) net.IP {
	ip := n.IP.To4()
	bcast := make(net.IP, 4)
	for i := range bcast {
		bcast[i] = ip[i] | ^n.Mask[i]
	}
	return bcast
}

// volumeFlags is a repeatable -afp-volume flag. The raw "Name:Path"
// strings are forwarded to wireAFP, where the //go:build afp side
// parses them via afp.ParseVolumeFlag. Keeping this neutral lets
// minimal-build users still pass -afp-volume and get a clean warning.
type volumeFlags []string

func (v *volumeFlags) String() string { return "" }

func (v *volumeFlags) Set(s string) error {
	*v = append(*v, s)
	return nil
}

func detectPcapInterfaceIPv4(interfaceName string) (string, bool) {
	if strings.TrimSpace(interfaceName) == "" {
		return "", false
	}

	devs, err := rawlink.ListPcapDevices()
	if err != nil {
		return "", false
	}

	for _, d := range devs {
		if d.Name != interfaceName {
			continue
		}
		return selectPreferredIPv4(d.Addresses)
	}

	return "", false
}

func selectPreferredIPv4(addrs []string) (string, bool) {
	var linkLocal string
	for _, addr := range addrs {
		ip := net.ParseIP(strings.TrimSpace(addr)).To4()
		if ip == nil || ip.IsUnspecified() || ip.IsLoopback() {
			continue
		}
		if ip[0] == 169 && ip[1] == 254 {
			if linkLocal == "" {
				linkLocal = ip.String()
			}
			continue
		}
		return ip.String(), true
	}

	if linkLocal != "" {
		return linkLocal, true
	}

	return "", false
}

func firstUsableIPv4(n *net.IPNet) net.IP {
	if n == nil {
		return nil
	}
	base := n.IP.To4()
	if base == nil || len(n.Mask) != net.IPv4len {
		return nil
	}
	candidate := append(net.IP(nil), base...)
	for i := len(candidate) - 1; i >= 0; i-- {
		candidate[i]++
		if candidate[i] != 0 {
			break
		}
	}
	if !n.Contains(candidate) || candidate.Equal(broadcastAddr(n)) {
		return nil
	}
	return candidate.To4()
}
