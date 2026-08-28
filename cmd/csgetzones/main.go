// Command csgetzones queries the AppleTalk zone list — the ClassicStack equivalent of
// netatalk's getzones. It asks a router for the network's active zones and prints them,
// one per line.
//
// It stands on the client SDK's AppleTalk endpoint (client/atalk): it opens a transport
// via client/link, wraps it in an atalk.Endpoint + ATP requester, and calls
// ATP.GetZoneList — the ZIP zone-list requester half the server ring lacks — so the ATP
// paging, the TResp parse, and the -v wire trace are shared with the AFP client rather
// than hand-rolled. The transport defaults to LToUDP, with -transport tashtalk or pcap
// selecting the others.
//
// ZIP GetZoneList (Inside Macintosh: Networking, ch. 8) is ATP-carried: DDP type 3 to
// socket 6. The client walks the response pages (re-requesting from the next index) until
// the router signals the last page. The -local flag switches to GetLocalZones (only zones
// on the requester's own network), and -my asks GetMyZone (the single zone of the
// responding router).
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/client/atalk"
	"github.com/ObsoleteMadness/ClassicStack/client/trace"
	"github.com/ObsoleteMadness/ClassicStack/cmd/internal/atlink"
	"github.com/ObsoleteMadness/ClassicStack/cmd/internal/buildinfo"
)

// broadcastNode is the DDP node id every node on the segment receives; with no known
// router address, csgetzones broadcasts the request and answers come from any router.
const broadcastNode = 0xFF

// Build metadata injected at link time via -ldflags
// -X main.BuildVersion=... -X main.BuildCommit=... -X main.BuildDate=...
var (
	BuildVersion = "0.0.0-dev"
	BuildCommit  = "unknown"
	BuildDate    = "unknown"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "csgetzones:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		network = flag.Uint("net", 0, "AppleTalk network number (0 = local segment)")
		srcNode = flag.Uint("src", 0x01, "our LocalTalk source node (1..254) — with -claim (the default), this is only the desired first candidate for the LLAP node-claim; the node actually used may differ if it's taken")
		dstNode = flag.Uint("dst", broadcastNode, "router node to query (0xFF = broadcast to any router)")
		timeout = flag.Duration("timeout", 2*time.Second, "per-request reply timeout")
		local   = flag.Bool("local", false, "GetLocalZones: only zones on our own network")
		myZone  = flag.Bool("my", false, "GetMyZone: just the responding router's own zone")
		verbose = flag.Bool("v", false, "verbose wire trace to stderr")
		version = flag.Bool("version", false, "print version information and exit")
	)
	at := atlink.Flags(flag.CommandLine)
	flag.Parse()
	trace.SetVerbose(*verbose)

	if *version {
		buildinfo.Print(os.Stdout, "csgetzones", BuildVersion, BuildCommit, BuildDate)
		return nil
	}

	if at.ListIface {
		atlink.PrintInterfaces(os.Stdout)
		return nil
	}

	if *srcNode < 1 || *srcNode > 254 {
		return fmt.Errorf("src node %d out of range (1..254)", *srcNode)
	}

	query := atalk.AllZones
	switch {
	case *myZone:
		query = atalk.MyZone
	case *local:
		query = atalk.LocalZones
	}

	// Open the selected AppleTalk transport (LToUDP by default; -transport tashtalk or
	// pcap selects the others) and wrap it in the client SDK's DDP endpoint + ATP requester.
	dl, node, err := at.Open(uint16(*network), uint8(*srcNode))
	if err != nil {
		return err
	}
	ep := atalk.NewEndpoint(dl, atalk.Addr{Network: uint16(*network), Node: node})
	defer func() { _ = ep.Close() }()

	dst := atalk.Addr{Network: uint16(*network), Node: uint8(*dstNode)}
	zones, err := atalk.NewATP(ep).GetZoneList(dst, query, *timeout)
	if err != nil {
		return fmt.Errorf("ZIP zone-list query: %w", err)
	}
	for _, z := range zones {
		fmt.Println(z)
	}
	if len(zones) == 0 {
		fmt.Printf("no zones returned within %s\n", *timeout)
	}
	return nil
}
