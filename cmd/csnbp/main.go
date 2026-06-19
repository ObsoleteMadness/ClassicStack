// Command csnbp is a standalone AppleTalk Name Binding Protocol (NBP) lookup client —
// the ClassicStack equivalent of netatalk's nbplkup. It resolves an NBP entity name
// (object:type@zone) to the network addresses registered under it, acting as an
// nslookup for Classic Mac networks.
//
// Like csecho (the AEP echo client / aecho equivalent), this is a T1 "protocol-reuse
// proof": it drives the SAME core codec the server uses — core/protocol/nbp — over the
// adapter/link framers/links (cmd/internal/atlink picks the segment transport). No
// server, no router; just the wire stack composed into a client. The transport
// defaults to LToUDP, with -transport tashtalk or pcap selecting the others.
//
// NBP (Inside AppleTalk, 2nd ed., ch. 7): DDP type 2 on socket 2. csnbp emits a
// Broadcast Request (BrRq) carrying the name pattern and its OWN reply address; every
// node holding a matching name returns a Lookup Reply (LkUp-Rply) tuple to that
// address. csnbp collects replies until the timeout and prints one line per match. The
// name pattern may use '=' to wildcard the object or type field and '*' for "this
// zone".
package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/cmd/internal/atlink"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/nbp"
)

// broadcastNode is the DDP node id every node on the segment receives.
const broadcastNode = 0xFF

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "csnbp:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		network = flag.Uint("net", 0, "AppleTalk network number (0 = local segment)")
		srcNode = flag.Uint("src", 0x01, "our LocalTalk source node (1..254)")
		timeout = flag.Duration("timeout", 2*time.Second, "how long to collect replies")
	)
	at := atlink.Flags(flag.CommandLine)
	flag.Usage = usage
	flag.Parse()

	if *srcNode < 1 || *srcNode > 254 {
		return fmt.Errorf("src node %d out of range (1..254)", *srcNode)
	}

	pattern := "=:=@*" // default: every name in this zone (like nbplkup with no args)
	if flag.NArg() > 0 {
		pattern = flag.Arg(0)
	}
	obj, typ, zone, err := parseEntity(pattern)
	if err != nil {
		return err
	}

	// Open the selected AppleTalk transport (LToUDP by default; -transport tashtalk or
	// pcap selects the others), asserting our claimed network/node (a probe client may
	// assert one without a node-claim handshake).
	dl, err := at.Open(uint16(*network), uint8(*srcNode))
	if err != nil {
		return err
	}
	defer dl.Close()

	nbpID := byte(rand.Intn(256))
	req := lookupRequest(nbpID, uint16(*network), uint8(*srcNode), obj, typ, zone)
	if err := dl.WriteDatagram(req); err != nil {
		return fmt.Errorf("send NBP BrRq: %w", err)
	}
	fmt.Printf("looking up %s:%s@%s ...\n", obj, typ, zone)

	matches := collectReplies(dl, nbpID, uint8(*srcNode), *timeout)
	if matches == 0 {
		fmt.Printf("no replies within %s\n", *timeout)
	}
	return nil
}

// lookupRequest builds the NBP Broadcast Request datagram: DDP type 2 to socket 2,
// carrying the name pattern and our own reply address (where matches are returned).
func lookupRequest(nbpID byte, network uint16, srcNode uint8, obj, typ, zone string) ddp.Datagram {
	data := nbp.BuildLkUp(nbp.CtrlBrRq, nbpID, network, srcNode, nbp.SASSocket,
		[]byte(obj), []byte(typ), []byte(zone))
	return ddp.Datagram{
		DestNetwork: network,
		SrcNetwork:  network,
		DestNode:    broadcastNode,
		SrcNode:     srcNode,
		DestSocket:  nbp.SASSocket,
		SrcSocket:   nbp.SASSocket,
		DDPType:     nbp.DDPType,
		Data:        data,
	}
}

// collectReplies reads datagrams until the timeout, printing one line per matching
// LkUp-Rply tuple addressed to us with our request's NBP id. Returns the match count.
func collectReplies(dl link.DatagramLink, nbpID, ourNode uint8, timeout time.Duration) int {
	deadline := time.Now().Add(timeout)
	matches := 0
	for time.Now().Before(deadline) {
		dg, err := dl.ReadDatagram()
		if err != nil {
			if err == link.ErrTimeout {
				continue // the LToUDP read deadline ticked; keep waiting until our own deadline
			}
			return matches
		}
		if dg.DDPType != nbp.DDPType || dg.DestSocket != nbp.SASSocket {
			continue
		}
		if dg.DestNode != ourNode && dg.DestNode != broadcastNode {
			continue // a reply meant for some other requester
		}
		pkt, err := nbp.ParsePacket(dg.Data)
		if err != nil || pkt.Function != nbp.CtrlLkUpRply || pkt.NBPID != nbpID {
			continue
		}
		t := pkt.Tuple
		fmt.Printf("  %s:%s@%s\t%d.%d:%d\n",
			t.Object, t.Type, t.Zone, t.Network, t.Node, t.Socket)
		matches++
	}
	return matches
}

// parseEntity splits an NBP entity name "object:type@zone" into its three fields,
// defaulting omitted fields to wildcards ('=' for object/type, '*' for zone) the way
// nbplkup does.
func parseEntity(s string) (obj, typ, zone string, err error) {
	obj, typ, zone = "=", "=", "*"
	rest := s
	if at := strings.LastIndex(rest, "@"); at >= 0 {
		if z := rest[at+1:]; z != "" {
			zone = z
		}
		rest = rest[:at]
	}
	if colon := strings.Index(rest, ":"); colon >= 0 {
		if t := rest[colon+1:]; t != "" {
			typ = t
		}
		rest = rest[:colon]
	}
	if rest != "" {
		obj = rest
	}
	if len(obj) > 0xFF || len(typ) > 0xFF || len(zone) > 0xFF {
		return "", "", "", fmt.Errorf("entity field too long (max 255 bytes each)")
	}
	return obj, typ, zone, nil
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: csnbp [flags] [object:type@zone]")
	fmt.Fprintln(os.Stderr, "  resolves an NBP name to its registered addresses (omitted fields wildcard:")
	fmt.Fprintln(os.Stderr, "  '=' object/type, '*' zone). Default pattern: =:=@*")
	flag.PrintDefaults()
}
