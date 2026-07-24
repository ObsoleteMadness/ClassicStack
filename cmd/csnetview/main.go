// Command csnetview enumerates the NetBIOS hosts on a segment — an enhanced "net view".
// Unlike a passive sniffer, it ACTIVELY solicits: over each supported datagram carrier it
// broadcasts a browser AnnouncementRequest so every listening browser re-announces
// immediately, then collects the HostAnnouncement / LocalMasterAnnouncement /
// DomainAnnouncement frames and prints the discovered hosts GROUPED BY the protocol they
// were heard on.
//
// It is a THIN consumer of the client SDK's connectionless-datagram carrier
// (client/netbios): it parses flags and calls netbios.BrowseAll, which opens each carrier
// (nbf = NetBEUI over 802.2 LLC, nbipx = NetBIOS-over-IPX / NWLink), solicits, listens,
// and returns the hosts per protocol. All the wire work — the AnnouncementRequest, the
// \MAILSLOT\BROWSE envelope, the announcement decode, and the browse-list aggregation —
// lives in the SDK, so this file is an example of how a client enumerates a legacy segment.
//
// It needs the 'pcap' build tag (libpcap/Npcap) and privilege to open the NIC.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/client/netbios"
	"github.com/ObsoleteMadness/ClassicStack/client/trace"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "csnetview:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		iface     = flag.String("iface", "", "interface to browse on (pcap device or TUN/TAP device name; required)")
		ifaceType = flag.String("ifacetype", "pcap", "interface type: pcap | tap")
		timeout   = flag.Duration("timeout", 4*time.Second, "how long to listen per carrier after soliciting")
		verbose   = flag.Bool("v", false, "verbose wire trace to stderr")
	)
	flag.Usage = usage
	flag.Parse()
	trace.SetVerbose(*verbose)

	if *iface == "" {
		flag.Usage()
		return fmt.Errorf("-iface is required (a pcap or TUN/TAP device name)")
	}

	// Build the raw-Ethernet opener for the chosen interface type (pcap or the
	// libpcap-free TUN/TAP), the same way the SMB file client selects its transport.
	opener, err := netbios.OpenerFor(*ifaceType, *iface, [6]byte{})
	if err != nil {
		return err
	}

	fmt.Printf("soliciting browser announcements on %s (%s per carrier) ...\n", *iface, *timeout)
	// The SDK opens each carrier, broadcasts an AnnouncementRequest, listens, and returns
	// the hosts grouped by the protocol they were heard on. A per-carrier open failure is
	// reported per protocol (e.g. no IPX on the segment) without aborting the whole sweep.
	station := netbios.DefaultStationName(opener.MAC, netbios.NameTypeWorkstation)
	hosts, errs := netbios.BrowseAll(opener, station, *timeout)
	printGrouped(hosts, errs)
	return nil
}

// printGrouped renders the discovered hosts grouped by carrier protocol, one aligned
// table per protocol, with any per-carrier error noted.
func printGrouped(hosts map[netbios.Protocol][]netbios.Host, errs map[netbios.Protocol]error) {
	total := 0
	for _, p := range netbios.Protocols {
		fmt.Printf("\n== %s ==\n", p)
		if err := errs[p]; err != nil {
			fmt.Printf("  (carrier unavailable: %v)\n", err)
			continue
		}
		list := hosts[p]
		if len(list) == 0 {
			fmt.Println("  no hosts discovered")
			continue
		}
		fmt.Printf("  %-16s %-21s %-7s %-7s %s\n", "HOST", "ADDRESS", "OS", "APPVER", "ROLE / COMMENT")
		for _, h := range list {
			rc := h.Role
			if h.Comment != "" {
				rc = h.Role + " — " + h.Comment
			}
			fmt.Printf("  %-16s %-21s %-7s %-7s %s\n",
				h.Name, h.Address, dash(h.OSVersion), dash(h.AppVersion), rc)
		}
		total += len(list)
	}
	fmt.Printf("\n%d host(s) discovered across %d carrier(s)\n", total, len(netbios.Protocols))
}

// dash renders an empty or zero version as "-".
func dash(v string) string {
	if v == "" || v == "0.0" {
		return "-"
	}
	return v
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: csnetview -iface <dev> [flags]")
	fmt.Fprintln(os.Stderr, "  actively enumerates NetBIOS hosts over each carrier (nbf, nbipx) and groups by protocol.")
	fmt.Fprintln(os.Stderr, "  ifacetype: pcap (libpcap/Npcap NIC) | tap (Linux TUN/TAP)")
	flag.PrintDefaults()
}
