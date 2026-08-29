// Command cspap is a minimal Printer Access Protocol (PAP) client: it enumerates the
// PAP-registered printer shares on a zone and reports each one's status string — the
// same information the Classic Mac Chooser shows next to a LaserWriter icon.
//
// It stands on the client SDK's AppleTalk endpoint (client/atalk): an NBP lookup finds
// each printer's session listening socket (SLS), then a single PAP SendStatus/Status ATP
// transaction (atalk.ATP.PAPStatus) reads its status string. This is intentionally the
// smallest useful PAP interaction — csecho, csnbp, and csgetzones already cover
// AEP/NBP/ZIP, so this tool only adds what those can't: talking PAP itself, without going
// as far as PAPOpen/data-transfer/Tickle/PAPClose (there is no service on this end to
// receive print data, so opening a connection would have nothing to do with it).
//
// PAP (Inside AppleTalk, 2nd ed., ch. 10) rides ATP (DDP type 3): a workstation NBP-looks-
// up the server's complete name to get its SLS address, then sends SendStatus (PAP
// function 8, no ATP data) to it; the server answers with Status (function 9) carrying a
// Pascal-format status string in the ATP data, without involving the print job code at
// all — the same call the Chooser polls to draw a printer's status line.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/client/atalk"
	"github.com/ObsoleteMadness/ClassicStack/cmd/internal/atlink"
	"github.com/ObsoleteMadness/ClassicStack/cmd/internal/diagflags"
)

// Build metadata injected at link time via -ldflags
// -X main.BuildVersion=... -X main.BuildCommit=... -X main.BuildDate=...
var (
	BuildVersion = "0.0.0-dev"
	BuildCommit  = "unknown"
	BuildDate    = "unknown"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "cspap:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		typ     = flag.String("type", atalk.PAPPrinterType, "NBP type to browse (LaserWriter is the conventional PAP type most Choosers and spoolers register under; pass = to browse every NBP type in the zone, e.g. when you don't know what a printer registered as — every match will then get a status query, including non-PAP entries, which will just time out)")
		zone    = flag.String("zone", "", "AppleTalk zone to search (default: this zone)")
		timeout = flag.Duration("timeout", 2*time.Second, "how long to collect NBP replies, and the per-printer PAP status timeout")
		status  = flag.Bool("status", true, "query each printer's PAP status (SendStatus) after finding it; -status=false only enumerates names/addresses")
	)
	src := diagflags.RegisterLLAPSource(flag.CommandLine)
	common := diagflags.RegisterCommon(flag.CommandLine)
	at := atlink.Flags(flag.CommandLine)
	flag.Usage = usage
	flag.Parse()
	common.ApplyVerbose()

	if common.HandleVersion(os.Stdout, "cspap", BuildVersion, BuildCommit, BuildDate) {
		return nil
	}

	if at.ListIface {
		atlink.PrintInterfaces(os.Stdout)
		return nil
	}

	if err := src.Validate(); err != nil {
		return err
	}
	network, srcNode := src.Network, src.SrcNode

	pattern := "" // object wildcard: every printer name
	if flag.NArg() > 0 {
		pattern = flag.Arg(0)
	}

	// Open the selected AppleTalk transport (LToUDP by default; -transport tashtalk or
	// pcap selects the others) and wrap it in the client SDK's DDP endpoint.
	dl, node, err := at.Open(uint16(*network), uint8(*srcNode))
	if err != nil {
		return err
	}
	ep := atalk.NewEndpoint(dl, atalk.Addr{Network: uint16(*network), Node: node})
	defer func() { _ = ep.Close() }()

	fmt.Printf("looking up =:%s@%s ...\n", orWildcard(*typ, "="), orWildcard(*zone, "*"))
	ents, err := ep.LookupTimeout(pattern, normWildcard(*typ), *zone, *timeout)
	if err != nil {
		return fmt.Errorf("NBP lookup: %w", err)
	}
	if len(ents) == 0 {
		fmt.Println("no printer shares found")
		return nil
	}

	atp := atalk.NewATP(ep)
	for _, e := range ents {
		fmt.Printf("  %s:%s@%s\t%d.%d:%d", e.Object, e.Type, e.Zone, e.Addr.Network, e.Addr.Node, e.Addr.Socket)
		if !*status {
			fmt.Println()
			continue
		}
		s, err := atp.PAPStatus(e.Addr, *timeout)
		if err != nil {
			fmt.Printf("\t(status: %v)\n", err)
			continue
		}
		fmt.Printf("\t%q\n", s)
	}
	return nil
}

// normWildcard maps an explicit wildcard token ('=') to the empty string Lookup
// interprets as "wildcard this field".
func normWildcard(s string) string {
	if s == "=" {
		return ""
	}
	return s
}

// orWildcard renders an empty (wildcarded) field as its wildcard token for display.
func orWildcard(s, wildcard string) string {
	if s == "" {
		return wildcard
	}
	return s
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: cspap [flags] [object]")
	fmt.Fprintln(os.Stderr, "  enumerates PAP printer shares (NBP -type, default LaserWriter) and reports each")
	fmt.Fprintln(os.Stderr, "  one's PAP status string. Omitted object wildcards (every name of that type).")
	flag.PrintDefaults()
}
