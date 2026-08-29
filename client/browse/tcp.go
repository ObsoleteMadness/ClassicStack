package browse

import (
	"fmt"
	"net"
	"strings"
	"time"

	clientlink "github.com/ObsoleteMadness/ClassicStack/client/link"
	"github.com/ObsoleteMadness/ClassicStack/client/netbios"
	clientsmb "github.com/ObsoleteMadness/ClassicStack/client/smb"
)

// EnumerateTCP is the TCP/IP half of "net view": locate the workgroup master browser
// via a broadcast NBNS query (UDP 137) from the browse NIC's IPv4, then ask that
// master for the authoritative server list over SMB-over-TCP (:445). NBF/NBIPX stay
// on Enumerate — they ride raw Ethernet, not IP. A quiet segment or a master that
// only speaks NBT :139 (no direct :445) yields an empty list, not a fatal error.
func EnumerateTCP(opts Options) ([]Server, Result) {
	res := Result{Protocol: netbios.TCP}
	src := ipv4ForDevice(opts.Device)
	if src == nil {
		res.Err = fmt.Errorf("no IPv4 on %s", opts.Device)
		tracef(opts, "[tcp] skip: %v", res.Err)
		return nil, res
	}
	window := opts.Window
	if window <= 0 {
		window = 2 * time.Second
	}
	workgroup := strings.TrimSpace(opts.Workgroup)
	tracef(opts, "[tcp] NBNS lookup for workgroup %q from %s ...", workgroup, src)
	masters, err := netbios.LookupMasterBrowser(src, workgroup, window)
	if err != nil {
		res.Err = err
		tracef(opts, "[tcp] NBNS: %v", err)
		return nil, res
	}
	if len(masters) == 0 {
		tracef(opts, "[tcp] no master browser answered NBNS")
		return nil, res
	}

	agg := map[string]*Server{}
	for _, m := range masters {
		res.MasterName = m.Name
		tracef(opts, "[tcp] master browser: %s (%s)", m.Name, m.IP)
		merge(agg, m.Name, netbios.TCP, SourceMaster, "master browser", "", "", m.IP.String())
		called := m.Name
		if called == "" {
			called = m.IP.String()
		}
		opener := clientlink.NewOpener(clientlink.Spec{Kind: clientlink.KindTCP, Name: m.IP.String()})
		servers, err := clientsmb.EnumServers(opener, called, workgroup, "", "")
		if err != nil {
			tracef(opts, "[tcp] %s: NetServerEnum2 over :445 failed: %v", called, err)
			continue
		}
		tracef(opts, "[tcp] %s returned %d servers (NetServerEnum2)", called, len(servers))
		for _, s := range servers {
			merge(agg, s.Name, netbios.TCP, SourceBrowseList, serverRole(s), s.Comment, "", "")
		}
		break
	}
	return sortedServers(agg), res
}

// ipv4ForDevice returns the first IPv4 bound on the pcap device (or OS interface of
// the same name). Windows Npcap names are matched via client/link.ListInterfaces
// addresses; a Unix pcap name is usually the OS interface name.
func ipv4ForDevice(device string) net.IP {
	device = strings.TrimSpace(device)
	if device == "" {
		return nil
	}
	if devs, err := clientlink.ListInterfaces(); err == nil {
		for _, d := range devs {
			if !strings.EqualFold(d.Name, device) {
				continue
			}
			if ip := firstIPv4(d.Addresses); ip != nil {
				return ip
			}
		}
	}
	return osInterfaceIPv4(device)
}

func firstIPv4(addrs []string) net.IP {
	for _, a := range addrs {
		s, _, _ := strings.Cut(a, "/")
		ip := net.ParseIP(strings.TrimSpace(s))
		if ip4 := ip.To4(); ip4 != nil && !ip4.IsLoopback() {
			return ip4
		}
	}
	return nil
}
