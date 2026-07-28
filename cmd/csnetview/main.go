// Command csnetview enumerates the SMB servers on a segment — a real "net view". Crucially,
// it does NOT rely on broadcast self-announcements: in a real workgroup an ordinary host
// (e.g. a Win98 File & Print station) announces ONLY to the local master browser, on a slow
// periodic timer, and does not answer a broadcast AnnouncementRequest — so a solicit-and-
// sniff sweep almost never sees it. Instead csnetview finds the master browser and asks IT
// for the authoritative list (RAP NetServerEnum2), which is where the quiet ordinary hosts
// actually live. Over each carrier (nbf = NetBEUI over 802.2 LLC, nbipx = NetBIOS-over-IPX)
// it runs three sources — solicit+sniff, find-master (__MSBROWSE__ / <workgroup><1D> +
// GetBackupList), and NetServerEnum2 over an SMB session to the master — and prints the
// merged, de-duplicated server list.
//
// It is a THIN consumer of the client SDK's browse primitive (client/browse.Enumerate),
// which owns all the wire orchestration; the same primitive backs `csclient discover smb`.
// It is deliberately passive — it never sends a browser election, so it is never electable
// as master (a browse client is ephemeral).
//
// It needs the 'pcap' build tag (libpcap/Npcap) and privilege to open the NIC.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/client/browse"
	clientlink "github.com/ObsoleteMadness/ClassicStack/client/link"
	"github.com/ObsoleteMadness/ClassicStack/client/trace"
	"github.com/ObsoleteMadness/ClassicStack/cmd/internal/csconnect"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "csnetview:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		iface     = flag.String("iface", "", "interface to browse on (pcap device or TUN/TAP device name; omit to auto-detect the primary NIC)")
		ifaceType = flag.String("ifacetype", "pcap", "interface type: pcap | tap")
		timeout   = flag.Duration("timeout", 4*time.Second, "how long to listen per carrier after soliciting")
		verbose   = flag.Bool("v", false, "verbose wire trace to stderr")
		listIf    = flag.Bool("list-ifaces", false, "list the capturable pcap NICs (the names -iface accepts) and exit")
	)
	flag.Usage = usage
	flag.Parse()
	trace.SetVerbose(*verbose)

	if *listIf {
		clientlink.PrintInterfaces(os.Stdout)
		return nil
	}

	// Auto-detect the host's primary (default-route) NIC when -iface is omitted, so
	// "Easy mode" works on a single-NIC box. Both carriers ride raw Ethernet (pcap/tap),
	// so ResolveIface fills a blank -iface with the primary NIC and announces the choice.
	ifaceName := csconnect.ResolveIface(*ifaceType, *iface)
	if ifaceName == "" {
		flag.Usage()
		return fmt.Errorf("-iface is required (a pcap or TUN/TAP device name; list them with -list-ifaces)")
	}

	fmt.Printf("enumerating SMB servers on %s (%s per carrier) ...\n", ifaceName, *timeout)
	// client/browse owns the whole sweep: over each carrier it solicits+sniffs, finds the
	// master browser (__MSBROWSE__ / <1D> + GetBackupList), and runs NetServerEnum2 against
	// the master for the authoritative list — then merges + de-duplicates. Progress lines are
	// echoed through Trace so the user sees each phase.
	servers, results := browse.Enumerate(browse.Options{
		Device: ifaceName,
		Kind:   *ifaceType,
		Window: *timeout,
		Trace:  func(line string) { fmt.Println(line) },
	})
	printServers(servers, results)
	return nil
}

// printServers renders the merged server list plus the per-carrier master-browser summary
// and any carrier-open errors.
func printServers(servers []browse.Server, results []browse.Result) {
	for _, r := range results {
		fmt.Printf("\n== %s ==\n", r.Protocol)
		if r.Err != nil {
			fmt.Printf("  (carrier unavailable: %v)\n", r.Err)
			continue
		}
		if r.MasterName == "" {
			fmt.Println("  no master browser answered")
		} else {
			fmt.Printf("  master browser: %s%s\n", r.MasterName, backups(r.BackupBrowsers))
		}
	}

	fmt.Println()
	if len(servers) == 0 {
		fmt.Println("no SMB servers found (no announcements, and no master browser answered)")
		return
	}
	fmt.Printf("%-16s %-14s %-9s %s\n", "SERVER", "CARRIERS", "SOURCE", "ROLE / COMMENT")
	for _, s := range servers {
		fmt.Printf("%-16s %-14s %-9s %s\n",
			s.Name, carriers(s.Carriers), sourceLabel(s.Source), roleComment(s))
	}
	fmt.Printf("\n%d server(s) discovered\n", len(servers))
}

// carriers joins a server's carriers ("nbf+nbipx").
func carriers(cs []browse.Protocol) string {
	parts := make([]string, 0, len(cs))
	for _, c := range cs {
		parts = append(parts, string(c))
	}
	return strings.Join(parts, "+")
}

// sourceLabel names how a server was discovered.
func sourceLabel(s browse.Source) string {
	switch s {
	case browse.SourceBrowseList:
		return "list" // authoritative — from a master's NetServerEnum2
	case browse.SourceMaster:
		return "master"
	default:
		return "announce"
	}
}

// roleComment renders the role and/or comment as one field.
func roleComment(s browse.Server) string {
	switch {
	case s.Role != "" && s.Comment != "":
		return s.Role + " — " + s.Comment
	case s.Role != "":
		return s.Role
	default:
		return s.Comment
	}
}

// backups renders a master's backup-browser list as a suffix.
func backups(bs []string) string {
	if len(bs) == 0 {
		return ""
	}
	return " (backups: " + strings.Join(bs, ", ") + ")"
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: csnetview -iface <dev> [flags]")
	fmt.Fprintln(os.Stderr, "  enumerates SMB servers via the master browser (NetServerEnum2) over each carrier (nbf, nbipx).")
	fmt.Fprintln(os.Stderr, "  ifacetype: pcap (libpcap/Npcap NIC) | tap (Linux TUN/TAP)")
	flag.PrintDefaults()
}
