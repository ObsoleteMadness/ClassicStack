// Command csnbp is a standalone AppleTalk Name Binding Protocol (NBP) lookup client —
// the ClassicStack equivalent of netatalk's nbplkup. It resolves an NBP entity name
// (object:type@zone) to the network addresses registered under it, acting as an
// nslookup for Classic Mac networks.
//
// It stands on the client SDK's AppleTalk endpoint (client/atalk): it opens a transport
// via client/link, wraps it in an atalk.Endpoint, and calls Endpoint.Lookup — the same
// NBP requester the AFP client uses to discover a server — so the lookup, the reply
// collection, and the -v wire trace are shared with every other client tool rather than
// hand-rolled here. The transport defaults to LToUDP, with -transport tashtalk or pcap
// selecting the others.
//
// NBP (Inside AppleTalk, 2nd ed., ch. 7): DDP type 2 on socket 2. Lookup emits a
// Broadcast Request (BrRq) carrying the name pattern and the endpoint's OWN reply
// address; every node holding a matching name returns a Lookup Reply (LkUp-Rply) tuple.
// csnbp prints one line per match. The name pattern may use '=' to wildcard the object or
// type field and '*' for "this zone".
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/client/atalk"
	"github.com/ObsoleteMadness/ClassicStack/client/trace"
	"github.com/ObsoleteMadness/ClassicStack/cmd/internal/atlink"
	"github.com/ObsoleteMadness/ClassicStack/cmd/internal/buildinfo"
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
		fmt.Fprintln(os.Stderr, "csnbp:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		network = flag.Uint("net", 0, "AppleTalk network number we claim as our source (0 = the AppleTalk \"startup range\" placeholder — a strict peer, e.g. a real Mac or an accurate emulator, may legitimately ignore requests from a node still asserting network 0; pass the segment's real network number, e.g. -net 1, if a peer that answers a real client doesn't answer this probe)")
		srcNode = flag.Uint("src", 0x01, "our LocalTalk source node (1..254)")
		timeout = flag.Duration("timeout", 2*time.Second, "how long to collect replies")
		verbose = flag.Bool("v", false, "verbose wire trace to stderr")
		version = flag.Bool("version", false, "print version information and exit")
	)
	at := atlink.Flags(flag.CommandLine)
	flag.Usage = usage
	flag.Parse()
	trace.SetVerbose(*verbose)

	if *version {
		buildinfo.Print(os.Stdout, "csnbp", BuildVersion, BuildCommit, BuildDate)
		return nil
	}

	if at.ListIface {
		atlink.PrintInterfaces(os.Stdout)
		return nil
	}

	if *srcNode < 1 || *srcNode > 254 {
		return fmt.Errorf("src node %d out of range (1..254)", *srcNode)
	}

	pattern := "=:=@*" // default: every name in this zone (like nbplkup with no args)
	if flag.NArg() > 0 {
		pattern = flag.Arg(0)
	}
	obj, typ, zone := parseEntity(pattern)

	// Open the selected AppleTalk transport (LToUDP by default; -transport tashtalk or
	// pcap selects the others) and wrap it in the client SDK's DDP endpoint, asserting our
	// claimed network/node (a probe client may assert one without a node-claim handshake).
	dl, err := at.Open(uint16(*network), uint8(*srcNode))
	if err != nil {
		return err
	}
	ep := atalk.NewEndpoint(dl, atalk.Addr{Network: uint16(*network), Node: uint8(*srcNode)})
	defer func() { _ = ep.Close() }()

	fmt.Printf("looking up %s:%s@%s ...\n", orWildcard(obj, "="), orWildcard(typ, "="), orWildcard(zone, "*"))
	ents, err := ep.LookupTimeout(obj, typ, zone, *timeout)
	if err != nil {
		return fmt.Errorf("NBP lookup: %w", err)
	}
	for _, e := range ents {
		fmt.Printf("  %s:%s@%s\t%d.%d:%d\n",
			e.Object, e.Type, e.Zone, e.Addr.Network, e.Addr.Node, e.Addr.Socket)
	}
	if len(ents) == 0 {
		fmt.Println("no replies")
	}
	return nil
}

// parseEntity splits an NBP entity name "object:type@zone" into its three fields.
// Omitted fields become empty strings, which atalk.Endpoint.Lookup treats as the
// wildcard ('=' for object/type, '*' for zone), matching nbplkup's defaults.
func parseEntity(s string) (obj, typ, zone string) {
	rest := s
	if at := strings.LastIndex(rest, "@"); at >= 0 {
		zone = rest[at+1:]
		rest = rest[:at]
	}
	if colon := strings.Index(rest, ":"); colon >= 0 {
		typ = rest[colon+1:]
		rest = rest[:colon]
	}
	obj = rest
	// A bare '=' / '*' is already the wildcard; normalise it to empty so Lookup wildcards.
	return normWildcard(obj), normWildcard(typ), normWildcard(zone)
}

// normWildcard maps an explicit wildcard token ('=' or '*') to the empty string Lookup
// interprets as "wildcard this field".
func normWildcard(s string) string {
	if s == "=" || s == "*" {
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
	fmt.Fprintln(os.Stderr, "usage: csnbp [flags] [object:type@zone]")
	fmt.Fprintln(os.Stderr, "  resolves an NBP name to its registered addresses (omitted fields wildcard:")
	fmt.Fprintln(os.Stderr, "  '=' object/type, '*' zone). Default pattern: =:=@*")
	flag.PrintDefaults()
}
