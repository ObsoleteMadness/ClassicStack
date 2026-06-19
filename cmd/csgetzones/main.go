// Command csgetzones queries the AppleTalk zone list — the ClassicStack equivalent of
// netatalk's getzones. It asks a router for the network's active zones and prints
// them, one per line.
//
// Like csecho (aecho) and csnbp (nbplkup), this is a T1 "protocol-reuse proof": it
// drives the SAME core constants the server's ZIP responder uses — core/service/zip —
// over the adapter/link framers/links (cmd/internal/atlink picks the segment
// transport). No server, no router; just the wire stack composed into a client. The
// transport defaults to LToUDP, with -transport tashtalk or pcap selecting the others.
//
// ZIP GetZoneList (Inside Macintosh: Networking, ch. 8) is an ATP-carried request:
// DDP type 3 (ATP) to socket 6 (ZIP). The client sends an ATP TReq whose user bytes
// hold the GetZoneList function and a 1-relative start index; the router answers with
// a TResp page of length-prefixed zone names plus a "last flag". csgetzones walks the
// pages (re-requesting from the next index) until the last flag is set. The -local flag
// switches to GetLocalZones (only zones on the requester's own network), and -my asks
// GetMyZone (the single zone of the responding router).
package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/cmd/internal/atlink"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/core/service/zip"
)

// broadcastNode is the DDP node id every node on the segment receives; with no known
// router address, csgetzones broadcasts the request and answers come from any router.
const broadcastNode = 0xFF

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "csgetzones:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		network = flag.Uint("net", 0, "AppleTalk network number (0 = local segment)")
		srcNode = flag.Uint("src", 0x01, "our LocalTalk source node (1..254)")
		dstNode = flag.Uint("dst", broadcastNode, "router node to query (0xFF = broadcast to any router)")
		timeout = flag.Duration("timeout", 2*time.Second, "per-request reply timeout")
		local   = flag.Bool("local", false, "GetLocalZones: only zones on our own network")
		myZone  = flag.Bool("my", false, "GetMyZone: just the responding router's own zone")
	)
	at := atlink.Flags(flag.CommandLine)
	flag.Parse()

	if *srcNode < 1 || *srcNode > 254 {
		return fmt.Errorf("src node %d out of range (1..254)", *srcNode)
	}

	fn := byte(zip.ATPGetZoneList)
	switch {
	case *myZone:
		fn = zip.ATPGetMyZone
	case *local:
		fn = zip.ATPGetLocalZoneList
	}

	// Open the selected AppleTalk transport (LToUDP by default; -transport tashtalk or
	// pcap selects the others), framed as a DDP DatagramLink.
	dl, err := at.Open(uint16(*network), uint8(*srcNode))
	if err != nil {
		return err
	}
	defer dl.Close()

	total := 0
	startIndex := 1 // ZIP indexes the zone list 1-relative
	for {
		tid := uint16(rand.Intn(0x10000))
		req := zoneListRequest(tid, fn, startIndex, uint16(*network), uint8(*srcNode), uint8(*dstNode))
		if err := dl.WriteDatagram(req); err != nil {
			return fmt.Errorf("send ZIP request: %w", err)
		}

		zones, last, ok := awaitResponse(dl, tid, uint8(*srcNode), *timeout)
		if !ok {
			if total == 0 {
				fmt.Printf("no reply within %s\n", *timeout)
			}
			return nil
		}
		for _, z := range zones {
			fmt.Println(string(z))
		}
		total += len(zones)
		// GetMyZone returns a single zone with no paging; otherwise page until the
		// router signals it has exhausted the list (or returns an empty page).
		if fn == zip.ATPGetMyZone || last || len(zones) == 0 {
			return nil
		}
		startIndex += len(zones)
	}
}

// zoneListRequest builds the ATP TReq datagram carrying a ZIP zone-list function. The
// 8-byte ATP user-bytes block is: control (TReq), bitmap (1 response segment), the two
// transaction-id bytes, the ZIP function, a reserved zero, and the 1-relative start
// index — the exact layout the ZIP responder decodes.
func zoneListRequest(tid uint16, fn byte, startIndex int, network uint16, srcNode, dstNode uint8) ddp.Datagram {
	data := []byte{
		zip.ATPFuncTReq, 0x01,
		byte(tid >> 8), byte(tid),
		fn, 0x00,
		byte(startIndex >> 8), byte(startIndex),
	}
	return ddp.Datagram{
		DestNetwork: network,
		SrcNetwork:  network,
		DestNode:    dstNode,
		SrcNode:     srcNode,
		DestSocket:  zip.SAS,
		SrcSocket:   zip.SAS,
		DDPType:     zip.ATPDDPType,
		Data:        data,
	}
}

// awaitResponse reads datagrams until the matching ATP TResp arrives or the timeout
// elapses. It returns the page's zone names, the router's last-flag, and whether a
// response was seen at all. The TResp layout (handleGetZoneList): control|EOM, 0, tid
// hi/lo, lastFlag, 0, numZones hi/lo, then length-prefixed zone names.
func awaitResponse(dl link.DatagramLink, tid uint16, ourNode uint8, timeout time.Duration) (zones [][]byte, last, ok bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		dg, err := dl.ReadDatagram()
		if err != nil {
			if err == link.ErrTimeout {
				continue
			}
			return nil, false, false
		}
		if dg.DDPType != zip.ATPDDPType || dg.DestSocket != zip.SAS {
			continue
		}
		if dg.DestNode != ourNode && dg.DestNode != broadcastNode {
			continue
		}
		d := dg.Data
		if len(d) < 8 || d[0]&zip.ATPFuncTResp == 0 {
			continue // not a transaction response
		}
		if uint16(d[2])<<8|uint16(d[3]) != tid {
			continue // a response to some other transaction
		}
		last = d[4] != 0
		zones = parseZones(d[8:])
		return zones, last, true
	}
	return nil, false, false
}

// parseZones decodes the length-prefixed zone-name list in a GetZoneList response page.
func parseZones(b []byte) [][]byte {
	var zones [][]byte
	for len(b) >= 1 {
		l := int(b[0])
		if len(b) < 1+l {
			break
		}
		if l > 0 {
			zones = append(zones, b[1:1+l])
		}
		b = b[1+l:]
	}
	return zones
}
