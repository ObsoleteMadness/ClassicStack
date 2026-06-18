// Command csipxping is a standalone IPX reachability probe over raw Ethernet — the
// ClassicStack equivalent of Novell's IPXPING. It sends an IPX Diagnostic request
// (socket 0x0456) to a target node (or the broadcast address) and reports the
// round-trip time of each Diagnostic Response, the IPX analogue of csecho's AEP ping.
//
// Like csecho/csnbp/csgetzones it drives the SAME core codecs the server uses —
// core/protocol/ipx (the datagram) and core/protocol/ipx/diag (the diagnostic
// request/response) — but over the pcap NIC link rather than LToUDP, because IPX
// rides Ethernet, not multicast UDP. The IPX/Ethernet encapsulation (Ethernet II,
// type 0x8137) is small enough to frame inline here; it matches core/port/ipx.
//
// IPX node IDs on Ethernet are the 6-byte MAC, so the target is given as a MAC (or
// "broadcast"); our own source node is the chosen interface's MAC. A reachable host
// running an IPX Diagnostic Responder (ClassicStack does — see core/service/ipxdiag,
// and so do real NetWare nodes) answers; csipxping prints the responder's address and
// the RTT.
//
// Requires the 'pcap' build tag (libpcap/Npcap) and the privilege to open the NIC.
package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/adapter/link/pcap"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx/diag"
)

// etherTypeIPX is the Ethernet II type for IPX; ethHdrLen is the Ethernet II header
// length (dst MAC + src MAC + type). These match core/port/ipx's encapsulation.
const (
	etherTypeIPX = 0x8137
	ethHdrLen    = 14
)

// broadcastMAC is the all-ones Ethernet/IPX broadcast address.
var broadcastMAC = [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "csipxping:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		iface   = flag.String("iface", "", "network interface to send on (pcap device name; required)")
		target  = flag.String("dst", "broadcast", "target node as a MAC address (aa:bb:cc:dd:ee:ff) or \"broadcast\"")
		network = flag.String("net", "00000000", "IPX network number, 8 hex digits (0 = local segment)")
		count   = flag.Int("count", 3, "number of diagnostic requests to send")
		timeout = flag.Duration("timeout", 2*time.Second, "per-request reply timeout")
		wait    = flag.Duration("interval", 500*time.Millisecond, "delay between requests")
	)
	flag.Parse()

	if *iface == "" {
		return fmt.Errorf("an -iface is required (a pcap device name; list them with the server's diagnostics)")
	}
	dstNode, broadcast, err := parseTarget(*target)
	if err != nil {
		return err
	}
	net4, err := parseNetwork(*network)
	if err != nil {
		return err
	}
	srcMAC, err := interfaceMAC(*iface)
	if err != nil {
		return err
	}

	fl, err := pcap.Open(pcap.DefaultEtherTalkConfig(*iface))
	if err != nil {
		return fmt.Errorf("open %s: %w", *iface, err)
	}
	defer fl.Close()

	// Narrow the capture to IPX frames if the link supports a kernel filter; harmless
	// (best-effort) otherwise — the read loop filters again by socket/type anyway.
	if f, ok := fl.(link.FilterableLink); ok {
		_ = f.SetFilter("ipx or (ether proto 0x8137)")
	}

	fmt.Printf("IPXPING %s on %s\n", *target, *iface)
	replies := 0
	for i := range *count {
		sent := time.Now()
		if err := sendRequest(fl, srcMAC, dstNode, net4, broadcast); err != nil {
			return fmt.Errorf("send request: %w", err)
		}
		from, ok := awaitReply(fl, srcMAC, *timeout)
		if ok {
			replies++
			fmt.Printf("reply #%d from %s  net %s  time=%s\n",
				i+1, macString(from.SrcNode), netString(from.SrcNet), time.Since(sent).Round(time.Microsecond))
		} else {
			fmt.Printf("request #%d: no reply within %s\n", i+1, *timeout)
		}
		if i+1 < *count {
			time.Sleep(*wait)
		}
	}

	fmt.Printf("\n--- %s IPX diagnostic statistics ---\n", *target)
	loss := 100
	if *count > 0 {
		loss = (*count - replies) * 100 / *count
	}
	fmt.Printf("%d requests sent, %d replies, %d%% loss\n", *count, replies, loss)
	if replies == 0 {
		os.Exit(1)
	}
	return nil
}

// sendRequest builds and writes one IPX Diagnostic request as an Ethernet II frame.
// A directed ping carries an empty exclusion list; a broadcast ping excludes our own
// node so we do not answer ourselves.
func sendRequest(fl link.FrameLink, srcMAC, dstNode, net4 [6]byte, broadcast bool) error {
	var req diag.Request
	if broadcast {
		req.Exclusions = [][6]byte{srcMAC}
	}
	body, err := req.Marshal()
	if err != nil {
		return err
	}
	d := &ipxproto.Datagram{
		Type:    0x04, // PEP, matching NBIPX / direct-SMB and the responder
		DstNode: dstNode,
		DstSock: diag.Socket,
		SrcNode: srcMAC,
		SrcSock: diag.Socket,
		Payload: body,
	}
	copy(d.DstNet[:], net4[:4])
	copy(d.SrcNet[:], net4[:4])

	ipxBytes, err := d.Encode(nil)
	if err != nil {
		return err
	}
	dstMAC := dstNode
	if broadcast {
		dstMAC = broadcastMAC
	}
	frame := make([]byte, 0, ethHdrLen+len(ipxBytes))
	frame = append(frame, dstMAC[:]...)
	frame = append(frame, srcMAC[:]...)
	frame = append(frame, byte(etherTypeIPX>>8), byte(etherTypeIPX&0xFF))
	frame = append(frame, ipxBytes...)
	return fl.Write(frame)
}

// awaitReply reads frames until a Diagnostic Response addressed to our diagnostic
// socket arrives or the timeout elapses, returning the responder's IPX datagram.
func awaitReply(fl link.FrameLink, srcMAC [6]byte, timeout time.Duration) (*ipxproto.Datagram, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		frame, err := fl.Read()
		if err != nil {
			if err == link.ErrTimeout {
				continue
			}
			return nil, false
		}
		payload, ok := stripIPX(frame)
		if !ok {
			continue
		}
		d, err := ipxproto.Decode(payload)
		if err != nil {
			continue
		}
		if d.DstSock != diag.Socket || d.SrcSock != diag.Socket {
			continue // not diagnostic traffic
		}
		if d.SrcNode == srcMAC {
			continue // our own broadcast echoed back
		}
		if _, err := diag.UnmarshalResponse(d.Payload); err != nil {
			continue // a malformed or non-response frame on the socket
		}
		return d, true
	}
	return nil, false
}

// stripIPX returns the IPX datagram bytes from an Ethernet frame, accepting the three
// legacy framings core/port/ipx accepts (Ethernet II 0x8137, raw 802.3 0xFFFF magic,
// and 802.2 LLC DSAP=SSAP=0xE0).
func stripIPX(frame link.Frame) ([]byte, bool) {
	if len(frame) < ethHdrLen {
		return nil, false
	}
	etherType := uint16(frame[12])<<8 | uint16(frame[13])
	switch {
	case etherType == etherTypeIPX:
		return frame[ethHdrLen:], true
	case etherType <= 0x05DC: // 802.3 length-typed
		if len(frame) < ethHdrLen+3 {
			return nil, false
		}
		body := frame[ethHdrLen:]
		if body[0] == 0xFF && body[1] == 0xFF {
			return body, true // raw 802.3 IPX
		}
		if body[0] == 0xE0 && body[1] == 0xE0 && body[2] == 0x03 {
			return body[3:], true // 802.2 LLC UI
		}
	}
	return nil, false
}

// parseTarget parses the -dst flag into a node MAC and whether it is the broadcast.
func parseTarget(s string) (node [6]byte, broadcast bool, err error) {
	if s == "broadcast" || s == "ff:ff:ff:ff:ff:ff" {
		return broadcastMAC, true, nil
	}
	hw, err := net.ParseMAC(s)
	if err != nil || len(hw) != 6 {
		return node, false, fmt.Errorf("invalid target MAC %q (want aa:bb:cc:dd:ee:ff or \"broadcast\")", s)
	}
	copy(node[:], hw)
	return node, node == broadcastMAC, nil
}

// parseNetwork parses an 8-hex-digit IPX network number into the first 4 bytes of a
// padded array (the upper two stay zero so a [6]byte fits both net + node call sites).
func parseNetwork(s string) ([6]byte, error) {
	var out [6]byte
	if len(s) != 8 {
		return out, fmt.Errorf("network %q must be 8 hex digits", s)
	}
	for i := range 4 {
		var b byte
		if _, err := fmt.Sscanf(s[2*i:2*i+2], "%02x", &b); err != nil {
			return out, fmt.Errorf("network %q is not hex", s)
		}
		out[i] = b
	}
	return out, nil
}

// interfaceMAC resolves the hardware address of the named interface. pcap device
// names and OS interface names usually coincide; when they do not (e.g. Windows NPF
// device GUIDs) the caller can still ping but the source node will be zero — so we
// fail loudly rather than send a zero-node probe.
func interfaceMAC(name string) ([6]byte, error) {
	var mac [6]byte
	ifi, err := net.InterfaceByName(name)
	if err != nil {
		return mac, fmt.Errorf("interface %q: %w (on Windows pass the OS interface name, not the NPF device id)", name, err)
	}
	if len(ifi.HardwareAddr) != 6 {
		return mac, fmt.Errorf("interface %q has no 6-byte hardware address", name)
	}
	copy(mac[:], ifi.HardwareAddr)
	return mac, nil
}

// macString formats a 6-byte node as a colon-separated MAC.
func macString(n [6]byte) string {
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", n[0], n[1], n[2], n[3], n[4], n[5])
}

// netString formats the 4-byte IPX network number as 8 hex digits.
func netString(n [4]byte) string {
	return fmt.Sprintf("%02x%02x%02x%02x", n[0], n[1], n[2], n[3])
}
