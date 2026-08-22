// Command csecho is a standalone AppleTalk Echo Protocol (AEP) client — the AppleTalk
// analogue of ping (netatalk's aecho). It sends an echo request to a node and reports
// the round-trip of each reply.
//
// It stands on the client SDK's AppleTalk endpoint (client/atalk): it opens a transport
// via client/link, wraps it in an atalk.Endpoint, and calls Endpoint.Echo — the AEP
// requester half the server ring lacks — so the DDP send, the reply filtering, and the
// -v wire trace are shared with every other client tool rather than hand-rolled. The
// transport defaults to LToUDP, with -transport tashtalk or pcap selecting the others.
//
// AEP (Inside Macintosh: Networking, ch. 3): DDP type 4 on socket 4. A request carries
// command byte 1; the responder reflects it as a reply with command byte 2 and the same
// payload. A destination node of 0xFF broadcasts to every node on the segment.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
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
		fmt.Fprintln(os.Stderr, "csecho:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		network = flag.Uint("net", 0, "AppleTalk network number (0 = local segment)")
		srcNode = flag.Uint("src", 0x01, "our LocalTalk source node (1..254)")
		dstNode = flag.Uint("dst", 0xFF, "destination node (0xFF = broadcast to every node)")
		count   = flag.Int("count", 1, "number of echo requests to send")
		timeout = flag.Duration("timeout", 2*time.Second, "per-request reply timeout")
		payload = flag.String("data", "ClassicStack csecho", "echo payload string")
		verbose = flag.Bool("v", false, "verbose wire trace to stderr")
		version = flag.Bool("version", false, "print version information and exit")
	)
	at := atlink.Flags(flag.CommandLine)
	flag.Parse()
	trace.SetVerbose(*verbose)

	if *version {
		buildinfo.Print(os.Stdout, "csecho", BuildVersion, BuildCommit, BuildDate)
		return nil
	}

	if at.ListIface {
		atlink.PrintInterfaces(os.Stdout)
		return nil
	}

	if *srcNode < 1 || *srcNode > 254 {
		return fmt.Errorf("src node %d out of range (1..254)", *srcNode)
	}

	// Open the selected AppleTalk transport (LToUDP by default; -transport tashtalk or
	// pcap selects the others) and wrap it in the client SDK's DDP endpoint. A static Addr
	// supplies our claimed network/node (no node-claim handshake — a probe client asserts
	// one, as it may).
	dl, err := at.Open(uint16(*network), uint8(*srcNode))
	if err != nil {
		return err
	}
	ep := atalk.NewEndpoint(dl, atalk.Addr{Network: uint16(*network), Node: uint8(*srcNode)})
	defer ep.Close()

	dst := atalk.Addr{Network: uint16(*network), Node: uint8(*dstNode)}
	replies := 0
	for i := 0; i < *count; i++ {
		sent := time.Now()
		echoed, from, err := ep.Echo(dst, []byte(*payload), *timeout)
		if err != nil {
			if errors.Is(err, atalk.ErrEchoTimeout) {
				fmt.Printf("request #%d to node 0x%02X: no reply within %s\n", i+1, uint8(*dstNode), *timeout)
				continue
			}
			return fmt.Errorf("send echo request: %w", err)
		}
		replies++
		fmt.Printf("reply #%d from %d.%d: %q  time=%s\n",
			i+1, from.Network, from.Node, string(echoed), time.Since(sent).Round(time.Microsecond))
	}

	if replies == 0 {
		os.Exit(1)
	}
	return nil
}
