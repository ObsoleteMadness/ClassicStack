// Command csncpinfo is a standalone NetWare file-server discovery probe over raw
// Ethernet — the ClassicStack equivalent of NetWare's SLIST. It broadcasts a SAP
// "Get Nearest Server" / "General Service" query (IPX socket 0x0452) for the File
// Server type and prints every server that answers: its name and IPX address
// (network/node/socket). It proves the SAP codec round-trips on the wire and aids
// diagnosing why a NETx/VLM client cannot see the server.
//
// Like csipxping it drives the SAME core codecs the server uses — core/protocol/ipx
// (the datagram) and core/protocol/ncp (the SAP query/response) — over the pcap NIC
// link, because IPX rides Ethernet. The IPX/Ethernet encapsulation is done through
// core/port/ipx.FrameType, so -frametype selects Ethernet II (default, MacIPX), raw
// 802.3, or 802.2 LLC — a real NetWare server bound on raw-802.3 / 802.2 ignores an
// Ethernet II query, so matching its frame type is what makes SLIST see it.
//
// Requires the 'pcap' build tag (libpcap/Npcap) and the privilege to open the NIC.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/adapter/link/pcap"
	clientlink "github.com/ObsoleteMadness/ClassicStack/client/link"
	"github.com/ObsoleteMadness/ClassicStack/cmd/internal/buildinfo"
	"github.com/ObsoleteMadness/ClassicStack/cmd/internal/csconnect"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	ipxport "github.com/ObsoleteMadness/ClassicStack/core/port/ipx"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
	ncpproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ncp"
)

var broadcastMAC = [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

// Build metadata injected at link time via -ldflags
// -X main.BuildVersion=... -X main.BuildCommit=... -X main.BuildDate=...
var (
	BuildVersion = "0.0.0-dev"
	BuildCommit  = "unknown"
	BuildDate    = "unknown"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "csncpinfo:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		iface     = flag.String("iface", "", "network interface to send on (pcap device name; omit to auto-detect the primary NIC)")
		network   = flag.String("net", "00000000", "IPX network number, 8 hex digits (0 = local segment)")
		timeout   = flag.Duration("timeout", 2*time.Second, "how long to collect SAP responses")
		nearest   = flag.Bool("nearest", false, "send a Get-Nearest-Server query instead of a general query")
		frameType = flag.String("frametype", "", "IPX Ethernet encapsulation: ethernet_ii | 802.3 | 802.2 (default ethernet_ii)")
		macFlag   = flag.String("mac", "", "source MAC for our virtual station (default: random locally-administered)")
		listIf    = flag.Bool("list-ifaces", false, "list the capturable pcap NICs (the names -iface accepts) and exit")
		version   = flag.Bool("version", false, "print version information and exit")
	)
	flag.Parse()

	if *version {
		buildinfo.Print(os.Stdout, "csncpinfo", BuildVersion, BuildCommit, BuildDate)
		return nil
	}

	if *listIf {
		clientlink.PrintInterfaces(os.Stdout)
		return nil
	}

	// The SAP query's Ethernet encapsulation. Real NetWare servers bound on raw-802.3 or
	// 802.2 LLC ignore an Ethernet II query, so -frametype selects the framing through the
	// SAME logic the server port uses (core/port/ipx.FrameType) rather than hardcoding
	// Ethernet II — see the frame-type-must-match-server errata. Default is Ethernet II
	// (MacIPX). Responses are decoded regardless of framing via ipx.Strip.
	ft, err := ipxport.ParseFrameType(*frameType)
	if err != nil {
		return err
	}

	// Auto-detect the host's primary (default-route) NIC when -iface is omitted, so
	// "Easy mode" works on a single-NIC box. SAP rides IPX over raw Ethernet, so the
	// pcap kind is what needs a NIC device; ResolveIface fills it and announces the
	// choice, or leaves it blank (the open below then reports the missing device).
	ifaceName := csconnect.ResolveIface(clientlink.KindPcap, *iface)
	if ifaceName == "" {
		return fmt.Errorf("an -iface is required (a pcap device name; list them with -list-ifaces)")
	}
	net4, err := parseNetwork(*network)
	if err != nil {
		return err
	}
	// The SAP query is sent from a synthetic locally-administered station MAC (or the
	// pinned -mac) rather than the host NIC's own address — matching the rest of the client
	// ring (a probe must not borrow the host's identity), and avoiding a Windows-only
	// lookup that cannot resolve a pcap "\Device\NPF_{GUID}" device name. Replies are
	// matched by SAP source socket (see the read loop), not by destination MAC, so a
	// synthetic source is fine.
	srcMAC, err := csconnect.StationMAC(*macFlag)
	if err != nil {
		return err
	}

	fl, err := pcap.Open(pcap.DefaultEtherTalkConfig(ifaceName))
	if err != nil {
		return fmt.Errorf("open %s: %w", ifaceName, err)
	}
	defer fl.Close()
	if f, ok := fl.(link.FilterableLink); ok {
		_ = f.SetFilter("ipx or (ether proto 0x8137)")
	}

	op := ncpproto.SAPGeneralQuery
	if *nearest {
		op = ncpproto.SAPNearestQuery
	}
	if err := sendQuery(fl, srcMAC, net4, op, ft); err != nil {
		return fmt.Errorf("send SAP query: %w", err)
	}
	fmt.Printf("SLIST on %s (%s) — waiting %s for file servers…\n", ifaceName, ft, *timeout)

	seen := map[string]bool{}
	deadline := time.Now().Add(*timeout)
	for time.Now().Before(deadline) {
		frame, err := fl.Read()
		if err != nil {
			if err == link.ErrTimeout {
				continue
			}
			break
		}
		payload, ok := stripIPX(frame)
		if !ok {
			continue
		}
		d, err := ipxproto.Decode(payload)
		if err != nil || d.SrcSock != ncpproto.SAPSocket {
			continue
		}
		for _, e := range parseEntries(d.Payload) {
			if e.Type != ncpproto.SAPServerTypeFileServer {
				continue
			}
			key := macString(e.Node)
			if seen[key] {
				continue
			}
			seen[key] = true
			fmt.Printf("  %-48s net %s  node %s  socket %02x%02x\n",
				e.Name, netString(e.Network), macString(e.Node), e.Socket[0], e.Socket[1])
		}
	}

	fmt.Printf("\n%d file server(s) found\n", len(seen))
	if len(seen) == 0 {
		os.Exit(1)
	}
	return nil
}

// sendQuery broadcasts a SAP query for the File Server type in the chosen frame type.
func sendQuery(fl link.FrameLink, srcMAC, net4 [6]byte, op uint16, ft ipxport.FrameType) error {
	// A SAP query is the operation (2 BE) + the service type (2 BE).
	payload := []byte{byte(op >> 8), byte(op), byte(ncpproto.SAPServerTypeFileServer >> 8), byte(ncpproto.SAPServerTypeFileServer)}
	d := &ipxproto.Datagram{
		Type:    0x04, // PEP
		DstNode: broadcastMAC,
		DstSock: ncpproto.SAPSocket,
		SrcNode: srcMAC,
		SrcSock: ncpproto.SAPSocket,
		Payload: payload,
	}
	copy(d.DstNet[:], net4[:4])
	copy(d.SrcNet[:], net4[:4])

	ipxBytes, err := d.Encode(nil)
	if err != nil {
		return err
	}
	// Encapsulate through the SAME logic the server port uses, so a -frametype of 802.3 /
	// 802.2 reaches a real NetWare server bound on that framing (not just Ethernet II).
	frame := ft.Encapsulate(broadcastMAC, srcMAC, ipxBytes)
	return fl.Write(frame)
}

// parseEntries decodes the SAP service entries from a response payload (operation +
// 64-byte entries). A query (no entries) yields none.
func parseEntries(payload []byte) []ncpproto.SAPEntry {
	if len(payload) < 2 {
		return nil
	}
	body := payload[2:] // skip the operation word
	var out []ncpproto.SAPEntry
	for len(body) >= ncpproto.SAPEntryLen {
		rec := body[:ncpproto.SAPEntryLen]
		var e ncpproto.SAPEntry
		e.Type = uint16(rec[0])<<8 | uint16(rec[1])
		e.Name = string(trimNUL(rec[2:50]))
		copy(e.Network[:], rec[50:54])
		copy(e.Node[:], rec[54:60])
		copy(e.Socket[:], rec[60:62])
		e.Hops = uint16(rec[62])<<8 | uint16(rec[63])
		out = append(out, e)
		body = body[ncpproto.SAPEntryLen:]
	}
	return out
}

// stripIPX returns the IPX datagram bytes carried in an Ethernet frame, accepting all
// three IPX framings (Ethernet II 0x8137, raw 802.3 0xFFFF magic, 802.2 LLC DSAP=SSAP=0xE0)
// via core/port/ipx.Strip so a reply arrives regardless of the server's frame type.
func stripIPX(frame link.Frame) ([]byte, bool) {
	payload, _, ok := ipxport.Strip(frame)
	return payload, ok
}

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

func macString(n [6]byte) string {
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", n[0], n[1], n[2], n[3], n[4], n[5])
}

func netString(n [4]byte) string {
	return fmt.Sprintf("%02x%02x%02x%02x", n[0], n[1], n[2], n[3])
}

func trimNUL(b []byte) []byte {
	for len(b) > 0 && b[len(b)-1] == 0 {
		b = b[:len(b)-1]
	}
	return b
}
