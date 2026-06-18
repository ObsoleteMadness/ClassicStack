// Command csecho is a standalone AppleTalk Echo Protocol (AEP) client over LToUDP.
// It is a T1 "protocol-reuse proof": a tiny client binary that drives the SAME core
// codecs and adapters the server uses — core/protocol/ddp (the datagram), the
// adapter/link/framing LLAP framer, and the adapter/link/ltoudp multicast link — to
// send an AEP echo request and print the reply. No server code, no router; just the
// wire stack, proving the protocol layers compose into a client as cleanly as a
// server.
//
// AEP (Inside Macintosh: Networking, ch. 3): DDP type 4 on socket 4. An echo
// REQUEST carries command byte 1; the responder reflects it as a REPLY with command
// byte 2 and the same payload. csecho sends a request to a destination node and
// waits for the matching reply on the shared LToUDP segment (multicast UDP
// 239.192.76.84:1954), the simplest real ClassicStack transport (no pcap/NIC).
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/adapter/link/framing"
	"github.com/ObsoleteMadness/ClassicStack/adapter/link/ltoudp"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/core/service/aep"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "csecho:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		iface   = flag.String("iface", "", "local IPv4 interface address to send on (default: every multicast-capable interface)")
		network = flag.Uint("net", 0, "AppleTalk network number (0 = local segment)")
		srcNode = flag.Uint("src", 0x01, "our LocalTalk source node (1..254)")
		dstNode = flag.Uint("dst", 0xFF, "destination node (0xFF = broadcast to every node)")
		count   = flag.Int("count", 1, "number of echo requests to send")
		timeout = flag.Duration("timeout", 2*time.Second, "per-request reply timeout")
		payload = flag.String("data", "ClassicStack csecho", "echo payload string")
	)
	flag.Parse()

	if *srcNode < 1 || *srcNode > 254 {
		return fmt.Errorf("src node %d out of range (1..254)", *srcNode)
	}

	// Open the SAME LToUDP link + LLAP framer the LocalTalk port uses, so we exchange
	// real DDP datagrams on the segment. A static Addr supplies our claimed
	// network/node (no node-claim handshake — we assert one, as a probe client may).
	fl, err := ltoudp.Open(ltoudp.DefaultConfig(*iface))
	if err != nil {
		return fmt.Errorf("open LToUDP: %w", err)
	}
	defer fl.Close()

	framer := &framing.LocalTalk{Addr: framing.NewStaticAddr(uint16(*network), uint8(*srcNode))}
	dl, err := framer.Framing(fl)
	if err != nil {
		return fmt.Errorf("frame LToUDP: %w", err)
	}

	for i := 0; i < *count; i++ {
		req := echoRequest(uint16(*network), uint8(*srcNode), uint8(*dstNode), []byte(*payload))
		if err := dl.WriteDatagram(req); err != nil {
			return fmt.Errorf("send echo request: %w", err)
		}
		fmt.Printf("sent AEP request #%d to node 0x%02X (%d bytes payload)\n", i+1, uint8(*dstNode), len(*payload))

		reply, ok := awaitReply(dl, uint8(*srcNode), *timeout)
		if !ok {
			fmt.Printf("  no reply within %s\n", *timeout)
			continue
		}
		fmt.Printf("  reply from node 0x%02X: %q\n", reply.SrcNode, string(reply.Data[1:]))
	}
	return nil
}

// echoRequest builds the AEP echo-request datagram: DDP type 4 to socket 4, command
// byte 1 (CmdRequest) followed by the payload. Source and destination network are
// equal so the LLAP framer uses the intra-segment short header.
func echoRequest(network uint16, srcNode, dstNode uint8, payload []byte) ddp.Datagram {
	data := append([]byte{aep.CmdRequest}, payload...)
	return ddp.Datagram{
		DestNetwork: network,
		SrcNetwork:  network,
		DestNode:    dstNode,
		SrcNode:     srcNode,
		DestSocket:  aep.Socket,
		SrcSocket:   aep.Socket,
		DDPType:     aep.DDPType,
		Data:        data,
	}
}

// awaitReply reads datagrams until an AEP reply addressed to us arrives or the
// timeout elapses. It filters for DDP type 4 / socket 4 / command byte 2 (CmdReply)
// destined for our node, ignoring our own echoed request and unrelated traffic.
func awaitReply(dl link.DatagramLink, ourNode uint8, timeout time.Duration) (ddp.Datagram, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		dg, err := dl.ReadDatagram()
		if err != nil {
			if err == link.ErrTimeout {
				continue // the LToUDP read deadline ticked; keep waiting until our own deadline
			}
			return ddp.Datagram{}, false
		}
		if dg.DDPType != aep.DDPType || dg.DestSocket != aep.Socket {
			continue
		}
		if len(dg.Data) == 0 || dg.Data[0] != aep.CmdReply {
			continue // not a reply (skip our own request, which carries CmdRequest)
		}
		if dg.DestNode != ourNode && dg.DestNode != 0xFF {
			continue
		}
		return dg, true
	}
	return ddp.Datagram{}, false
}
