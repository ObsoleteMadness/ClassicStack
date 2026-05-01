//go:build macip || all

package main

import (
	"fmt"
	"net"
	"strings"

	"github.com/pgodw/omnitalk/netlog"
	"github.com/pgodw/omnitalk/pkg/hwaddr"
	"github.com/pgodw/omnitalk/port/rawlink"
	"github.com/pgodw/omnitalk/service"
	"github.com/pgodw/omnitalk/service/macip"
)

type macipHook struct {
	svc *macip.Service
}

func (h *macipHook) Service() service.Service                      { return h.svc }
func (h *macipHook) PinLeaseToSession(net uint16, node, sess uint8) { h.svc.PinLeaseToSession(net, node, sess) }
func (h *macipHook) UnpinLeaseFromSession(sess uint8)               { h.svc.UnpinLeaseFromSession(sess) }
func (h *macipHook) MarkSessionActivity(sess uint8)                 { h.svc.MarkSessionActivity(sess) }

func wireMacIP(cfg MacIPConfig) (MacIPHook, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if cfg.EtherTalkBackend != "" && cfg.EtherTalkBackend != "pcap" {
		return nil, fmt.Errorf("-macip-enabled currently requires -ethertalk-backend pcap (got %q)", cfg.EtherTalkBackend)
	}

	ipIface := cfg.PcapDevice
	if ipIface == "" {
		if detected, ok := rawlink.DetectDefaultPcapInterface(); ok {
			ipIface = detected
			netlog.Info("[MAIN][MacIP] auto-detected pcap interface: %s", detected)
		} else {
			return nil, fmt.Errorf("-ethertalk-device is required when -macip-enabled is set (auto-detection failed)")
		}
	}

	ipMACStr := ""
	if strings.TrimSpace(cfg.BridgeHostMAC) != "" {
		ipMACStr = cfg.BridgeHostMAC
		netlog.Info("[MAIN][MacIP] using bridge host MAC for IP-side: %s", ipMACStr)
	} else if hostMAC, ok := rawlink.DetectHostMACForPcapInterface(ipIface); ok {
		ipMACStr = hostMAC
		netlog.Info("[MAIN][MacIP] auto-detected IP-side MAC from %s: %s", ipIface, ipMACStr)
	} else {
		ipMACStr = cfg.PcapHWAddr
	}

	hostIPStr, hostIPDetected := detectPcapInterfaceIPv4(ipIface)

	if cfg.IPGateway == "" {
		if gw, ok := rawlink.DetectDefaultGatewayForPcapInterface(ipIface); ok {
			cfg.IPGateway = gw
			netlog.Info("[MAIN][MacIP] auto-detected default gateway %s for interface %s", gw, ipIface)
		} else if hostIPDetected {
			cfg.IPGateway = hostIPStr
			netlog.Warn("[MAIN][MacIP] default gateway auto-detection failed; falling back to interface IPv4 %s on %s", hostIPStr, ipIface)
		} else {
			return nil, fmt.Errorf("-macip-ip-gateway is required when -macip-enabled is set (auto-detection failed and no IPv4 address was found)")
		}
	}

	_, ipNet, err := net.ParseCIDR(cfg.NATSubnet)
	if err != nil {
		return nil, fmt.Errorf("invalid -macip-nat-subnet: %w", err)
	}
	ipMACAddr, err := hwaddr.ParseEthernet(ipMACStr)
	if err != nil {
		return nil, fmt.Errorf("invalid IP-side MAC: %w", err)
	}
	ipMAC := ipMACAddr.HardwareAddr()
	ipGW := net.ParseIP(cfg.IPGateway).To4()
	if ipGW == nil {
		return nil, fmt.Errorf("invalid -macip-ip-gateway: %q", cfg.IPGateway)
	}
	var hostIP net.IP
	if hostIPDetected {
		hostIP = net.ParseIP(hostIPStr).To4()
	}
	gwIP := resolveMacIPGatewayIP(cfg.NATGatewayIP, ipNet, ipGW, cfg.NAT)
	if gwIP == nil {
		return nil, fmt.Errorf("invalid -macip-nat-gw: %q", cfg.NATGatewayIP)
	}
	if !cfg.NAT && strings.TrimSpace(cfg.NATGatewayIP) != "" {
		netlog.Info("[MAIN][MacIP] ignoring -macip-nat-gw in non-NAT mode; using upstream gateway %s", gwIP)
	} else if !cfg.NAT {
		netlog.Info("[MAIN][MacIP] using upstream gateway %s in non-NAT mode", gwIP)
	}
	if cfg.NAT && gwIP.Equal(ipGW) {
		return nil, fmt.Errorf("invalid MacIP configuration: -macip-nat-gw (%s) conflicts with the host-side upstream gateway (%s); choose a different MacIP gateway IP", gwIP, ipGW)
	}
	nsIP := ipGW
	if cfg.Nameserver != "" {
		nsIP = net.ParseIP(cfg.Nameserver).To4()
		if nsIP == nil {
			return nil, fmt.Errorf("invalid -macip-nameserver: %q", cfg.Nameserver)
		}
	}

	broadcast := broadcastAddr(ipNet)
	var chosenZone []byte
	if cfg.Zone != "" {
		chosenZone = []byte(cfg.Zone)
	} else if cfg.EtherTalkZone != "" {
		chosenZone = []byte(cfg.EtherTalkZone)
	}

	ipLink, err := rawlink.OpenPcap(rawlink.DefaultMacIPConfig(ipIface))
	if err != nil {
		return nil, fmt.Errorf("failed opening MacIP rawlink on %s: %w", ipIface, err)
	}
	if fl, ok := ipLink.(rawlink.FilterableLink); ok {
		if err := fl.SetFilter(macipBPFFilter(ipNet, cfg.DHCPRelay)); err != nil {
			netlog.Warn("[MAIN][MacIP] could not set BPF filter on %s: %v", ipIface, err)
		}
	}

	svc := macip.New(
		gwIP, ipNet.IP, ipNet.Mask,
		nsIP, broadcast,
		chosenZone,
		cfg.NBP,
		ipLink, ipMAC, hostIP, ipGW,
		cfg.NAT,
		cfg.DHCPRelay,
		cfg.StateFile,
	)
	netlog.Info("[MAIN][MacIP] gw=%s subnet=%s iface=%s host-ip=%s ip-gw=%s zone=%q nat=%t dhcp_relay=%t",
		gwIP, cfg.NATSubnet, ipIface, hostIP, ipGW, string(chosenZone), cfg.NAT, cfg.DHCPRelay)
	return &macipHook{svc: svc}, nil
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
