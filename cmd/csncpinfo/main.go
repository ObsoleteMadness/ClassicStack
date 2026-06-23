// Command csncpinfo is a standalone NetWare file-server discovery probe over raw
// Ethernet — the ClassicStack equivalent of NetWare's SLIST. It broadcasts a SAP
// "Get Nearest Server" / "General Service" query (IPX socket 0x0452) for the File
// Server type and prints every server that answers: its name and IPX address
// (network/node/socket). It proves the SAP codec round-trips on the wire and aids
// diagnosing why a NETx/VLM client cannot see the server.
//
// Like csipxping it drives the SAME core codecs the server uses — core/protocol/ipx
// (the datagram) and core/protocol/ncp (the SAP query/response) — over the pcap NIC
// link, because IPX rides Ethernet. The IPX/Ethernet encapsulation (Ethernet II,
// type 0x8137) matches core/port/ipx.
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
	ncpproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ncp"
)

const (
	etherTypeIPX = 0x8137
	ethHdrLen    = 14
)

var broadcastMAC = [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "csncpinfo:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		iface   = flag.String("iface", "", "network interface to send on (pcap device name; required)")
		network = flag.String("net", "00000000", "IPX network number, 8 hex digits (0 = local segment)")
		timeout = flag.Duration("timeout", 2*time.Second, "how long to collect SAP responses")
		nearest = flag.Bool("nearest", false, "send a Get-Nearest-Server query instead of a general query")
	)
	flag.Parse()

	if *iface == "" {
		return fmt.Errorf("an -iface is required (a pcap device name)")
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
	if f, ok := fl.(link.FilterableLink); ok {
		_ = f.SetFilter("ipx or (ether proto 0x8137)")
	}

	op := ncpproto.SAPGeneralQuery
	if *nearest {
		op = ncpproto.SAPNearestQuery
	}
	if err := sendQuery(fl, srcMAC, net4, op); err != nil {
		return fmt.Errorf("send SAP query: %w", err)
	}
	fmt.Printf("SLIST on %s — waiting %s for file servers…\n", *iface, *timeout)

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

// sendQuery broadcasts a SAP query for the File Server type.
func sendQuery(fl link.FrameLink, srcMAC, net4 [6]byte, op uint16) error {
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
	frame := make([]byte, 0, ethHdrLen+len(ipxBytes))
	frame = append(frame, broadcastMAC[:]...)
	frame = append(frame, srcMAC[:]...)
	frame = append(frame, byte(etherTypeIPX>>8), byte(etherTypeIPX&0xFF))
	frame = append(frame, ipxBytes...)
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

func stripIPX(frame link.Frame) ([]byte, bool) {
	if len(frame) < ethHdrLen {
		return nil, false
	}
	etherType := uint16(frame[12])<<8 | uint16(frame[13])
	switch {
	case etherType == etherTypeIPX:
		return frame[ethHdrLen:], true
	case etherType <= 0x05DC:
		if len(frame) < ethHdrLen+3 {
			return nil, false
		}
		body := frame[ethHdrLen:]
		if body[0] == 0xFF && body[1] == 0xFF {
			return body, true
		}
		if body[0] == 0xE0 && body[1] == 0xE0 && body[2] == 0x03 {
			return body[3:], true
		}
	}
	return nil, false
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

func interfaceMAC(name string) ([6]byte, error) {
	var mac [6]byte
	ifi, err := net.InterfaceByName(name)
	if err != nil {
		return mac, fmt.Errorf("interface %q: %w", name, err)
	}
	if len(ifi.HardwareAddr) != 6 {
		return mac, fmt.Errorf("interface %q has no 6-byte hardware address", name)
	}
	copy(mac[:], ifi.HardwareAddr)
	return mac, nil
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
