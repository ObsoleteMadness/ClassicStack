package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/ObsoleteMadness/ClassicStack/config"
	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/pkg/logging"
	"github.com/ObsoleteMadness/ClassicStack/port/rawlink"
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
	etFilter := flag.String("ethertalk-filter", "", "pcap BPF filter override for EtherTalk")
	bridgeMode := flag.String("bridge-mode", "", "Shared raw-link backend mode: pcap, tap, or tun (overrides ethertalk-backend)")
	bridgeDevice := flag.String("bridge-device", "", "Shared raw-link device/interface (overrides ethertalk-device)")
	bridgeHWAddr := flag.String("bridge-hw-address", "", "Shared raw-link host MAC (overrides ethertalk-hw-address)")
	bridgeFrameMode := flag.String("bridge-frame-mode", "", "Shared frame mode for bridge adaptation: auto, ethernet, wifi (overrides ethertalk-bridge-mode)")
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
	macipFilter := flag.String("macip-filter", "", "pcap BPF filter override for MacIP (default is auto-generated)")

	// Packet parsing / capture flags.
	parsePackets := flag.Bool("parse-packets", false, "Decode and log every inbound DDP packet (ATP/ASP/AFP layers)")
	parseOutput := flag.String("parse-output", "", "File path to write parsed packet log (appended; empty = stdout only)")

	captureLocalTalk := flag.String("capture-localtalk", "", "Write LocalTalk frames (LToUDP/TashTalk/Virtual) to a pcap file at this path (empty disables)")
	captureEtherTalk := flag.String("capture-ethertalk", "", "Write EtherTalk frames to a pcap file at this path (empty disables)")
	captureSnaplen := flag.Uint("capture-snaplen", 65535, "Per-frame snap length for pcap captures")

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

	// IPX flags. Real packet handling lands behind //go:build ipx; the
	// disabled stub logs a warning if -ipx-enabled is set without the tag.
	ipxEnable := flag.Bool("ipx-enabled", false, "Enable IPX router (requires -tags ipx)")
	ipxIface := flag.String("ipx-interface", "", "Rawlink/pcap interface for IPX (default: reuse -ethertalk-device)")
	ipxFraming := flag.String("ipx-framing", "ethernet_ii", "IPX framing: ethernet_ii, raw_802_3, llc, snap")
	ipxInternal := flag.String("ipx-internal-network", "", "IPX internal network number (8-hex-digit, e.g. DEADBEEF)")
	ipxFilter := flag.String("ipx-filter", "", "pcap BPF filter override for IPX (default: ipx)")

	// NetBEUI flags.
	netbeuiEnable := flag.Bool("netbeui-enabled", false, "Enable NetBEUI port (requires -tags netbeui)")
	netbeuiIface := flag.String("netbeui-interface", "", "Rawlink/pcap interface for NetBEUI (default: reuse -ethertalk-device)")
	netbeuiFilter := flag.String("netbeui-filter", "", "pcap BPF filter override for NetBEUI (default: llc)")

	// NetBIOS flags.
	netbiosEnable := flag.Bool("netbios-enabled", false, "Enable NetBIOS service (requires -tags netbios)")
	netbiosTransports := flag.String("netbios-transports", "tcp", "Comma-separated NetBIOS transports: any of tcp, netbeui, ipx")
	netbiosScopeID := flag.String("netbios-scope-id", "", "NetBIOS scope ID (RFC 1001/1002)")
	netbiosServerName := flag.String("netbios-server-name", "", "Deprecated: NetBIOS identity derives from SMB server/workgroup")
	netbiosWorkgroup := flag.String("netbios-workgroup", "", "Deprecated: NetBIOS identity derives from SMB server/workgroup")

	// SMB flags.
	smbEnable := flag.Bool("smb-enabled", false, "Enable SMB 1.0 server (requires -tags smb)")
	smbNBT := flag.String("smb-nbt-binding", ":139", "SMB NBT (NetBIOS over TCP) listen address")
	smbDirect := flag.String("smb-direct-binding", "", "SMB direct (TCP/445) listen address; empty disables direct SMB")
	smbGuest := flag.Bool("smb-guest-ok", false, "Accept unauthenticated SMB sessions")
	smbServerName := flag.String("smb-server-name", "CLASSICSTACK", "SMB/NetBIOS computer name")
	smbWorkgroup := flag.String("smb-workgroup", "WORKGROUP", "SMB/NetBIOS workgroup name")
	var smbShares volumeFlags
	flag.Var(&smbShares, "smb-share", `SMB share, format: "Name:Path" (repeatable)`)

	// Shortname flags.
	shortWindows := flag.Bool("shortname-windows-shortnames", false, "Enable Windows native shortnames")
	shortBackend := flag.String("shortname-backend", "memory", "Shortname store backend: memory or sqlite")
	shortDB := flag.String("shortname-db", "", "Shortname store DB path (sqlite backend)")

	// Web UI flags. The HTTP server lives behind -tags webui; the
	// disabled stub warns if -webui-enabled is set without the tag.
	webuiEnable := flag.Bool("webui-enabled", false, "Enable the management web UI (requires -tags webui)")
	webuiBind := flag.String("webui-bind", "127.0.0.1:8080", "Web UI listen address (IP:PORT)")
	webuiTLS := flag.Bool("webui-tls", true, "Serve the web UI over HTTPS (self-signed when no cert/key given)")
	webuiCert := flag.String("webui-cert-pem", "", "Path to PEM certificate for the web UI (blank: self-signed)")
	webuiKey := flag.String("webui-key-pem", "", "Path to PEM private key for the web UI (blank: self-signed)")

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
			LogLevel:            *logLevel,
			LogTraffic:          *logTraffic,
			ParsePackets:        *parsePackets,
			ParseOutput:         *parseOutput,
			LToUDPEnabled:       *ltoudp,
			LToUDPInterface:     *ltIface,
			LToUDPSeedNetwork:   *ltNet,
			LToUDPSeedZone:      *ltZone,
			TashTalkPort:        *tashtalkSerial,
			TashTalkSeedNetwork: *ttNet,
			TashTalkSeedZone:    *ttZone,
			BridgeMode:          *bridgeMode,
			BridgeDevice:        *bridgeDevice,
			BridgeHWAddress:     *bridgeHWAddr,
			BridgeBridgeMode:    *bridgeFrameMode,

			EtherTalkDevice:         *pcapDev,
			EtherTalkBackend:        *etBackend,
			EtherTalkHWAddress:      *pcapHWAddr,
			EtherTalkBridgeMode:     *etBridgeMode,
			EtherTalkBridgeHostMAC:  *etBridgeHostMAC,
			EtherTalkFilter:         *etFilter,
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
			MacIPFilter:             *macipFilter,
			CaptureLocalTalk:        *captureLocalTalk,
			CaptureEtherTalk:        *captureEtherTalk,
			CaptureSnaplen:          *captureSnaplen,

			IPXEnabled:         *ipxEnable,
			IPXInterface:       *ipxIface,
			IPXFraming:         *ipxFraming,
			IPXInternalNetwork: *ipxInternal,
			IPXFilter:          *ipxFilter,

			NetBEUIEnabled:   *netbeuiEnable,
			NetBEUIInterface: *netbeuiIface,
			NetBEUIFilter:    *netbeuiFilter,

			NetBIOSEnabled:    *netbiosEnable,
			NetBIOSTransports: *netbiosTransports,
			NetBIOSScopeID:    *netbiosScopeID,
			NetBIOSServerName: *netbiosServerName,
			NetBIOSWorkgroup:  *netbiosWorkgroup,

			SMBEnabled:       *smbEnable,
			SMBNBTBinding:    *smbNBT,
			SMBDirectBinding: *smbDirect,
			SMBGuestOk:       *smbGuest,
			SMBServerName:    *smbServerName,
			SMBWorkgroup:     *smbWorkgroup,
			SMBShareValues:   []string(smbShares),

			ShortnameWindowsShortnames: *shortWindows,
			ShortnameBackend:           *shortBackend,
			ShortnameDBPath:            *shortDB,

			WebUIEnabled: *webuiEnable,
			WebUIBind:    *webuiBind,
			WebUITLS:     *webuiTLS,
			WebUICertPEM: *webuiCert,
			WebUIKeyPEM:  *webuiKey,
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

	cfg.Bridge.Mode = strings.ToLower(strings.TrimSpace(cfg.Bridge.Mode))
	switch cfg.Bridge.Mode {
	case "", "pcap", "tap", "tun":
	default:
		log.Fatalf("invalid bridge mode %q (want pcap, tap, or tun)", cfg.Bridge.Mode)
	}
	syncBridgeToEtherTalk(&cfg)

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

	if cfg.EtherTalk.Device == "" && cfg.Bridge.Mode == "pcap" {
		if detected, ok := rawlink.DetectDefaultPcapInterface(); ok {
			netlog.Info("[MAIN] auto-detected pcap interface: %s", detected)
			cfg.Bridge.Device = detected
			syncBridgeToEtherTalk(&cfg)
		}
	}
	if cfg.EtherTalk.Device != "" && cfg.Bridge.Mode == "pcap" && strings.TrimSpace(cfg.EtherTalk.BridgeHostMAC) == "" {
		if hostMAC, ok := rawlink.DetectHostMACForPcapInterface(cfg.EtherTalk.Device); ok {
			cfg.EtherTalk.BridgeHostMAC = hostMAC
			if strings.TrimSpace(cfg.Bridge.HWAddress) == "" {
				cfg.Bridge.HWAddress = hostMAC
				syncBridgeToEtherTalk(&cfg)
			}
			netlog.Info("[MAIN] auto-detected bridge host MAC for %s: %s", cfg.EtherTalk.Device, hostMAC)
		}
	}

	// From here on, the build and lifecycle of every component lives in the
	// Supervisor. main.go's remaining job is to project the resolved config
	// into a config.Model, construct the supervisor and the management
	// plane, wire the (optional) web UI on top, run, and tear down.
	model := buildModel(cfg, configSource, fromConfigFile, afpFlagOptions{
		ServerName:      *afpServerName,
		Zone:            *afpZone,
		Protocols:       *afpProtocols,
		Binding:         *afpTCPAddr,
		ExtensionMap:    *afpExtensionMap,
		DecomposedNames: *afpDecomposedFilenames,
		CNIDBackend:     *afpCNIDBackend,
		AppleDoubleMode: *afpAppleDoubleMode,
		Volumes:         []string(afpVolumes),
	})
	sup, err := NewSupervisor(cfg, configSource, model)
	if err != nil {
		log.Fatalf("failed to build stack: %v", err)
	}

	plane := newControlPlane(sup, model, selectedConfig)
	wireDiagnostics(plane, sup)

	if err := installWebUI(sup, cfg.WebUI, plane); err != nil {
		log.Fatalf("failed to wire web UI: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := sup.Start(ctx); err != nil {
		log.Fatalf("failed to start stack: %v", err)
	}

	<-ctx.Done()

	if err := sup.Stop(); err != nil {
		netlog.Warn("[MAIN] stop warning: %v", err)
	}
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
