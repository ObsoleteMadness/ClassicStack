// Command csnetsend sends a NetBIOS Messenger ("net send" / WinPopup) pop-up message to
// a named station over a raw NIC — the ClassicStack equivalent of DOS/Windows `net send`.
//
// It is a THIN consumer of the client SDK's connectionless-datagram carrier
// (client/netbios): it parses flags, builds a netbios.Conn over the chosen carrier, and
// calls Conn.SendMessage. All the wire work — the single-block Messenger frame, the
// \MAILSLOT\MESSNGR SMB_COM_TRANSACTION envelope, and the NBF / NB-IPX datagram framing —
// lives in the SDK, so this file is an example of how a third-party client transmits a
// message, not a re-implementation of the protocol.
//
// The recipient is given as "<name>,<protocol>" (e.g. "SERVER,nbf"), the same
// name-plus-carrier target form the SDK's ParseTarget accepts: the protocol half selects
// the datagram carrier (nbf = NetBEUI over 802.2 LLC, nbipx = NetBIOS-over-IPX / NWLink),
// mirroring the SMB file client's -transport carriers. It needs the 'pcap' build tag
// (libpcap/Npcap) and privilege to open the NIC.
//
// Delivery is connectionless and unacknowledged: a successful send means the datagram was
// transmitted, not that the recipient popped it up (the Messenger datagram has no reply).
package main

import (
	"flag"
	"fmt"
	"net"
	"os"

	"github.com/ObsoleteMadness/ClassicStack/client/netbios"
	"github.com/ObsoleteMadness/ClassicStack/client/trace"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "csnetsend:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		iface     = flag.String("iface", "", "interface to send from (pcap device or TUN/TAP device name; required)")
		ifaceType = flag.String("ifacetype", "pcap", "interface type: pcap | tap")
		to        = flag.String("to", "", "recipient as \"<name>,<protocol>\" (protocol: nbf | nbipx; required)")
		from      = flag.String("from", "CLASSICSTACK", "sender name (the From field)")
		text      = flag.String("text", "", "message text (required)")
		macFlag   = flag.String("mac", "", "source MAC for our virtual station (default: random locally-administered)")
		verbose   = flag.Bool("v", false, "verbose wire trace to stderr")
	)
	flag.Usage = usage
	flag.Parse()
	trace.SetVerbose(*verbose)

	if *iface == "" || *to == "" || *text == "" {
		flag.Usage()
		return fmt.Errorf("-iface, -to and -text are required")
	}

	// Parse "<name>,<protocol>" into a Messenger-addressed target (name-type <03>).
	target, err := netbios.ParseTarget(*to, netbios.MessengerNameType)
	if err != nil {
		return err
	}

	// Parse an optional pinned virtual-station MAC (else the SDK synthesises a random
	// locally-administered one, so the client never borrows the host NIC's identity).
	var mac [6]byte
	if *macFlag != "" {
		var err error
		if mac, err = parseMAC(*macFlag); err != nil {
			return err
		}
	}

	// Build the raw-Ethernet opener for the chosen interface type (pcap or the
	// libpcap-free TUN/TAP), the same way the SMB file client selects its transport.
	opener, err := netbios.OpenerFor(*ifaceType, *iface, mac)
	if err != nil {
		return err
	}

	// Our own station name (the datagram Source), derived from the station MAC.
	station := netbios.DefaultStationName(opener.MAC, netbios.NameTypeWorkstation)
	conn, err := netbios.Open(opener, target.Protocol, station)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := conn.SendMessage(target.Name, netbios.Message{From: *from, To: target.Name.String(), Text: *text}); err != nil {
		return fmt.Errorf("send: %w", err)
	}
	fmt.Printf("sent %q to %s over %s\n", *text, target.Name.String(), target.Protocol)
	return nil
}

// parseMAC parses a colon/hyphen-separated MAC into a 6-byte array.
func parseMAC(s string) ([6]byte, error) {
	hw, err := net.ParseMAC(s)
	if err != nil || len(hw) != 6 {
		return [6]byte{}, fmt.Errorf("invalid -mac %q (want aa:bb:cc:dd:ee:ff)", s)
	}
	var mac [6]byte
	copy(mac[:], hw)
	return mac, nil
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: csnetsend -iface <dev> -to <name>,<protocol> -text <msg> [flags]")
	fmt.Fprintln(os.Stderr, "  sends a NetBIOS Messenger (\"net send\") pop-up over a raw interface.")
	fmt.Fprintln(os.Stderr, "  ifacetype: pcap (libpcap/Npcap NIC) | tap (Linux TUN/TAP)")
	fmt.Fprintln(os.Stderr, "  protocol: nbf (NetBEUI) | nbipx (NetBIOS-over-IPX)")
	flag.PrintDefaults()
}
