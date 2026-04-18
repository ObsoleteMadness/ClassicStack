package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/pgodw/omnitalk/go/netlog"
	"github.com/pgodw/omnitalk/go/port"
	"github.com/pgodw/omnitalk/go/port/ethertalk"
	"github.com/pgodw/omnitalk/go/port/localtalk"
	"github.com/pgodw/omnitalk/go/port/rawlink"
	"github.com/pgodw/omnitalk/go/router"
	"github.com/pgodw/omnitalk/go/service"
	"github.com/pgodw/omnitalk/go/service/aep"
	"github.com/pgodw/omnitalk/go/service/afp"
	"github.com/pgodw/omnitalk/go/service/asp"
	"github.com/pgodw/omnitalk/go/service/dsi"
	"github.com/pgodw/omnitalk/go/service/llap"
	"github.com/pgodw/omnitalk/go/service/macip"
	"github.com/pgodw/omnitalk/go/service/rtmp"
	"github.com/pgodw/omnitalk/go/service/zip"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	configPath := flag.String("config", "", "Path to INI config file (cannot be combined with other flags)")
	showVersion := flag.Bool("version", false, "Print OmniTalk version information and exit")

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

	// AFP file sharing flags.
	afpServerName := flag.String("afp-name", "Go File Server", "AFP server name advertised to clients")
	afpZone := flag.String("afp-zone", "", "AppleTalk zone for AFP NBP registration (default: first zone found)")
	afpProtocols := flag.String("afp-protocols", "tcp,ddp", "AFP protocols to enable: tcp, ddp, or tcp,ddp")
	afpTCPAddr := flag.String("afp-binding", ":548", "Address and port for AFP over TCP (DSI) to listen on")
	afpExtensionMap := flag.String("afp-extension-map", "", "Netatalk-compatible extension map file for Macintosh type/creator fallback")
	afpDecomposedFilenames := flag.Bool("afp-use-decomposed-names", true, "Encode host-reserved filename characters using 0xNN tokens when mapping AFP paths")
	afpCNIDBackend := flag.String("afp-cnid-backend", "sqlite", "CNID backend to use for AFP object IDs (sqlite or memory)")
	afpAppleDoubleMode := flag.String("afp-appledouble-mode", string(afp.AppleDoubleModeModern), "AppleDouble metadata mode: modern or legacy")
	var afpVolumes volumeFlags
	flag.Var(&afpVolumes, "afp-volume", `AFP volume to share, format: "Name:Path" (repeatable, e.g. -afp-volume "Mac Share:c:\mac")`)

	flag.Parse()

	if *showVersion {
		fmt.Printf("omnitalk %s\n", BuildVersion)
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
		if _, err := os.Stat("server.ini"); err == nil {
			selectedConfig = "server.ini"
		} else if os.IsNotExist(err) {
			flag.Usage()
			return
		} else {
			log.Fatalf("failed checking default config file server.ini: %v", err)
		}
	}

	if selectedConfig != "" {
		cfg, err := loadConfigFromINI(selectedConfig)
		if err != nil {
			log.Fatalf("failed loading config file %q: %v", selectedConfig, err)
		}

		*logLevel = cfg.LogLevel
		*logTraffic = cfg.LogTraffic

		*ltoudp = cfg.LToUDPEnabled
		*ltIface = cfg.LToUDPInterface
		*ltNet = cfg.LToUDPSeedNetwork
		*ltZone = cfg.LToUDPSeedZone

		*tashtalkSerial = cfg.TashTalkPort
		*ttNet = cfg.TashTalkSeedNetwork
		*ttZone = cfg.TashTalkSeedZone

		*pcapDev = cfg.EtherTalkDevice
		*etBackend = cfg.EtherTalkBackend
		*pcapHWAddr = cfg.EtherTalkHWAddr
		*etBridgeMode = cfg.EtherTalkBridgeMode
		*etBridgeHostMAC = cfg.EtherTalkBridgeHostMAC
		*etNetMin = cfg.EtherTalkSeedNetworkMin
		*etNetMax = cfg.EtherTalkSeedNetworkMax
		*etZone = cfg.EtherTalkSeedZone

		*macipEnable = cfg.MacIPEnabled
		*macipGWIP = cfg.MacIPGWIP
		*macipSubnet = cfg.MacIPSubnet
		*macipNameserver = cfg.MacIPNameserver
		*macipZone = cfg.MacIPZone
		*macipIPGW = cfg.MacIPGatewayIP
		*macipNAT = cfg.MacIPNAT
		*macipDHCP = cfg.MacIPDHCPRelay
		*macipStateFile = cfg.MacIPLeaseFile

		*parsePackets = cfg.ParsePackets
		*parseOutput = cfg.ParseOutput

		*afpServerName = cfg.AFPServerName
		*afpZone = cfg.AFPZone
		*afpProtocols = cfg.AFPProtocols
		*afpTCPAddr = cfg.AFPTCPBinding
		*afpExtensionMap = cfg.AFPExtensionMapPath
		*afpDecomposedFilenames = cfg.AFPDecomposedFilenames
		*afpCNIDBackend = cfg.AFPCNIDBackend
		afpVolumes = volumeFlags(cfg.AFPVolumes)
	}

	if level, ok := netlog.ParseLevel(*logLevel); ok {
		netlog.SetLevel(level)
	} else {
		log.Fatalf("unknown -log-level %q (want debug, info, or warn)", *logLevel)
	}

	if *logTraffic {
		netlog.SetLogFunc(func(s string) { netlog.Debug("%s", s) })
	}

	*etBackend = strings.ToLower(strings.TrimSpace(*etBackend))
	switch *etBackend {
	case "", "pcap", "tap", "tun":
	default:
		log.Fatalf("invalid -ethertalk-backend %q (want pcap, tap, or tun)", *etBackend)
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

	if *pcapDev == "" && *etBackend == "pcap" {
		if detected, ok := rawlink.DetectDefaultPcapInterface(); ok {
			netlog.Info("[MAIN] auto-detected pcap interface: %s", detected)
			*pcapDev = detected
		}
	}
	if *pcapDev != "" && *etBackend == "pcap" && strings.TrimSpace(*etBridgeHostMAC) == "" {
		if hostMAC, ok := rawlink.DetectHostMACForPcapInterface(*pcapDev); ok {
			*etBridgeHostMAC = hostMAC
			netlog.Info("[MAIN] auto-detected bridge host MAC for %s: %s", *pcapDev, hostMAC)
		}
	}

	var ports []port.Port
	if *ltoudp {
		ports = append(ports, localtalk.NewLtoudpPort(*ltIface, uint16(*ltNet), []byte(*ltZone)))
	}
	if *tashtalkSerial != "" {
		ports = append(ports, localtalk.NewTashTalkPort(*tashtalkSerial, uint16(*ttNet), []byte(*ttZone)))
	}
	if *pcapDev != "" {
		hwAddr, err := parseMAC(*pcapHWAddr)
		if err != nil {
			log.Fatalf("invalid -ethertalk-hw-address: %v", err)
		}
		var ep *ethertalk.PcapPort
		switch *etBackend {
		case "", "pcap":
			ep, err = ethertalk.NewPcapPort(*pcapDev, hwAddr, uint16(*etNetMin), uint16(*etNetMax), uint16(*etDesiredNet), uint8(*etDesiredNode), [][]byte{[]byte(*etZone)})
		case "tap", "tun":
			ep, err = ethertalk.NewTapPort(*pcapDev, hwAddr, uint16(*etNetMin), uint16(*etNetMax), uint16(*etDesiredNet), uint8(*etDesiredNode), [][]byte{[]byte(*etZone)})
		default:
			log.Fatalf("unsupported EtherTalk backend: %q", *etBackend)
		}
		if err != nil {
			log.Fatalf("failed creating EtherTalk port (%s): %v", *etBackend, err)
		}
		if err := ep.SetBridgeModeString(*etBridgeMode); err != nil {
			log.Fatalf("invalid -ethertalk-bridge-mode: %v", err)
		}
		if *etBridgeHostMAC != "" {
			hostMAC, err := parseMAC(*etBridgeHostMAC)
			if err != nil {
				log.Fatalf("invalid -ethertalk-bridge-host-mac: %v", err)
			}
			if err := ep.SetBridgeHostMAC(hostMAC); err != nil {
				log.Fatalf("invalid -ethertalk-bridge-host-mac: %v", err)
			}
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

	var macipSvc *macip.Service

	if *macipEnable {
		if *etBackend != "" && *etBackend != "pcap" {
			log.Fatalf("-macip-enabled currently requires -ethertalk-backend pcap (got %q)", *etBackend)
		}

		// MacIP shares the EtherTalk pcap interface; fall back to auto-detection.
		ipIface := *pcapDev
		if ipIface == "" {
			if detected, ok := rawlink.DetectDefaultPcapInterface(); ok {
				ipIface = detected
				netlog.Info("[MAIN][MacIP] auto-detected pcap interface: %s", detected)
			} else {
				log.Fatal("-ethertalk-device is required when -macip-enabled is set (auto-detection failed)")
			}
		}

		// Auto-detect IP-side MAC from the bridge host MAC or the interface itself.
		ipMACStr := ""
		if strings.TrimSpace(*etBridgeHostMAC) != "" {
			ipMACStr = *etBridgeHostMAC
			netlog.Info("[MAIN][MacIP] using bridge host MAC for IP-side: %s", ipMACStr)
		} else if hostMAC, ok := rawlink.DetectHostMACForPcapInterface(ipIface); ok {
			ipMACStr = hostMAC
			netlog.Info("[MAIN][MacIP] auto-detected IP-side MAC from %s: %s", ipIface, ipMACStr)
		} else {
			ipMACStr = *pcapHWAddr
		}

		hostIPStr, hostIPDetected := detectPcapInterfaceIPv4(ipIface)

		if *macipIPGW == "" {
			if gw, ok := rawlink.DetectDefaultGatewayForPcapInterface(ipIface); ok {
				*macipIPGW = gw
				netlog.Info("[MAIN][MacIP] auto-detected default gateway %s for interface %s", gw, ipIface)
			} else if hostIPDetected {
				*macipIPGW = hostIPStr
				netlog.Warn("[MAIN][MacIP] default gateway auto-detection failed; falling back to interface IPv4 %s on %s", hostIPStr, ipIface)
			} else {
				log.Fatal("-macip-ip-gateway is required when -macip-enabled is set (auto-detection failed and no IPv4 address was found)")
			}
		}

		_, ipNet, err := net.ParseCIDR(*macipSubnet)
		if err != nil {
			log.Fatalf("invalid -macip-nat-subnet: %v", err)
		}
		ipMAC, err := parseMAC(ipMACStr)
		if err != nil {
			log.Fatalf("invalid IP-side MAC: %v", err)
		}
		ipGW := net.ParseIP(*macipIPGW).To4()
		if ipGW == nil {
			log.Fatalf("invalid -macip-ip-gateway: %q", *macipIPGW)
		}
		var hostIP net.IP
		if hostIPDetected {
			hostIP = net.ParseIP(hostIPStr).To4()
		}
		gwIP := resolveMacIPGatewayIP(*macipGWIP, ipNet, ipGW, *macipNAT)
		if gwIP == nil {
			log.Fatalf("invalid -macip-nat-gw: %q", *macipGWIP)
		}
		if !*macipNAT && strings.TrimSpace(*macipGWIP) != "" {
			netlog.Info("[MAIN][MacIP] ignoring -macip-nat-gw in non-NAT mode; using upstream gateway %s", gwIP)
		} else if !*macipNAT {
			netlog.Info("[MAIN][MacIP] using upstream gateway %s in non-NAT mode", gwIP)
		}
		if *macipNAT && gwIP.Equal(ipGW) {
			log.Fatalf("invalid MacIP configuration: -macip-nat-gw (%s) conflicts with the host-side upstream gateway (%s); choose a different MacIP gateway IP", gwIP, ipGW)
		}
		nsIP := ipGW // default: physical gateway typically also serves DNS
		if *macipNameserver != "" {
			nsIP = net.ParseIP(*macipNameserver).To4()
			if nsIP == nil {
				log.Fatalf("invalid -macip-nameserver: %q", *macipNameserver)
			}
		}

		broadcast := broadcastAddr(ipNet)
		// Choose the NBP zone: explicit -macip-zone wins, then EtherTalk seed zone,
		// otherwise leave empty so the service picks the first zone found at start.
		var chosenZone []byte
		if *macipZone != "" {
			chosenZone = []byte(*macipZone)
		} else if *etZone != "" {
			chosenZone = []byte(*etZone)
		}

		// Open MacIP rawlink and apply BPF filter before injecting into the service.
		ipLink, err := rawlink.OpenPcap(rawlink.DefaultMacIPConfig(ipIface))
		if err != nil {
			log.Fatalf("failed opening MacIP rawlink on %s: %v", ipIface, err)
		}
		if fl, ok := ipLink.(rawlink.FilterableLink); ok {
			if err := fl.SetFilter(macipBPFFilter(ipNet, *macipDHCP)); err != nil {
				netlog.Warn("[MAIN][MacIP] could not set BPF filter on %s: %v", ipIface, err)
			}
		}

		macipSvc = macip.New(
			gwIP, ipNet.IP, ipNet.Mask,
			nsIP, broadcast,
			chosenZone,
			nbpSvc,
			ipLink, ipMAC, hostIP, ipGW,
			*macipNAT,
			*macipDHCP,
			*macipStateFile,
		)
		services = append(services, macipSvc)
		netlog.Info("[MAIN][MacIP] gw=%s subnet=%s iface=%s host-ip=%s ip-gw=%s zone=%q nat=%t dhcp_relay=%t",
			gwIP, *macipSubnet, ipIface, hostIP, ipGW, string(chosenZone), *macipNAT, *macipDHCP)
	}

	if len(afpVolumes) > 0 {
		var transports []afp.Transport
		var extMap *afp.ExtensionMap
		if *afpExtensionMap != "" {
			loadedMap, err := loadAFPExtensionMap(*afpExtensionMap)
			if err != nil {
				log.Fatalf("failed loading AFP extension map %q: %v", *afpExtensionMap, err)
			}
			extMap = loadedMap
		}

		protocols := strings.Split(*afpProtocols, ",")
		hasDDP := false
		hasTCP := false
		for _, p := range protocols {
			p = strings.TrimSpace(p)
			if strings.EqualFold(p, "ddp") {
				hasDDP = true
			} else if strings.EqualFold(p, "tcp") {
				hasTCP = true
			}
		}

		if hasDDP {
			aspSvc := asp.New(*afpServerName, nil, nbpSvc, []byte(*afpZone))
			if macipSvc != nil {
				aspSvc.SetSessionLifecycleHooks(
					func(sess *asp.Session) {
						macipSvc.PinLeaseToSession(sess.WSNet, sess.WSNode, sess.ID)
					},
					func(sess *asp.Session) {
						macipSvc.UnpinLeaseFromSession(sess.ID)
					},
					func(sess *asp.Session) {
						macipSvc.MarkSessionActivity(sess.ID)
					},
				)
			}
			transports = append(transports, aspSvc)
			netlog.Info("[MAIN][AFP] enabled DDP transport on socket %d", asp.ServerSocket)
		}

		if hasTCP {
			dsiSvc := dsi.NewServer(*afpServerName, *afpTCPAddr, nil)
			transports = append(transports, dsiSvc)
			netlog.Info("[MAIN][AFP] enabled TCP transport on %s", *afpTCPAddr)
		}

		afpSvc := afp.NewAFPService(
			*afpServerName,
			[]afp.VolumeConfig(afpVolumes),
			&afp.LocalFileSystem{},
			transports,
			afp.AFPOptions{DecomposedFilenames: *afpDecomposedFilenames, CNIDBackend: *afpCNIDBackend, AppleDoubleMode: parseAppleDoubleMode(*afpAppleDoubleMode), ExtensionMap: extMap},
		)

		// Wire up the circular dependencies for handlers
		for _, t := range transports {
			switch transport := t.(type) {
			case *asp.Service:
				transport.SetCommandHandler(afpSvc)
			case *dsi.Server:
				transport.SetCommandHandler(afpSvc)
			}
		}

		services = append(services, afpSvc)
		netlog.Info("[MAIN][AFP] server=%q volumes=%d zone=%q protocols=%q", *afpServerName, len(afpVolumes), *afpZone, *afpProtocols)
	}

	r := router.New("router", ports, services)

	if *parsePackets {
		dumper, cleanup, err := newPacketDumper(*parseOutput)
		if err != nil {
			log.Fatalf("parse-packets: %v", err)
		}
		defer cleanup()
		for _, svc := range services {
			if aware, ok := svc.(service.PacketDumpAware); ok {
				aware.SetPacketDumper(dumper)
			}
		}
		netlog.Info("[MAIN] parse-packets enabled; output=%q", *parseOutput)
	}

	if err := r.Start(); err != nil {
		log.Fatalf("failed to start router: %v", err)
	}
	netlog.Info("[MAIN] router away!")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig

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

// volumeFlags is a repeatable -afp-volume flag.
type volumeFlags []afp.VolumeConfig

func (v *volumeFlags) String() string { return "" }

func (v *volumeFlags) Set(s string) error {
	cfg, err := afp.ParseVolumeFlag(s)
	if err != nil {
		return err
	}
	*v = append(*v, cfg)
	return nil
}

func parseMAC(s string) ([]byte, error) {
	normalized := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(s), ":", ""), "-", "")
	if len(normalized) != 12 {
		return nil, fmt.Errorf("want 12 hex digits, got %d", len(normalized))
	}
	b, err := hex.DecodeString(normalized)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func parseAppleDoubleMode(mode string) afp.AppleDoubleMode {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "legacy", string(afp.AppleDoubleModeLegacy):
		return afp.AppleDoubleModeLegacy
	default:
		return afp.AppleDoubleModeModern
	}
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

func resolveMacIPGatewayIP(configured string, natSubnet *net.IPNet, upstreamGateway net.IP, natMode bool) net.IP {
	if !natMode {
		return append(net.IP(nil), upstreamGateway.To4()...)
	}
	trimmed := strings.TrimSpace(configured)
	if trimmed != "" {
		return net.ParseIP(trimmed).To4()
	}
	return firstUsableIPv4(natSubnet)
}

func macipBPFFilter(ipNet *net.IPNet, dhcpMode bool) string {
	if dhcpMode {
		return "(arp) or (ip) or (udp dst port 68)"
	}
	return fmt.Sprintf("(arp) or (dst net %s)", ipNet.String())
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
