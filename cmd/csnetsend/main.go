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
	"os"

	clientlink "github.com/ObsoleteMadness/ClassicStack/client/link"
	"github.com/ObsoleteMadness/ClassicStack/client/netbios"
	"github.com/ObsoleteMadness/ClassicStack/client/trace"
	"github.com/ObsoleteMadness/ClassicStack/cmd/internal/buildinfo"
	"github.com/ObsoleteMadness/ClassicStack/cmd/internal/csconnect"
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
		fmt.Fprintln(os.Stderr, "csnetsend:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		iface     = flag.String("iface", "", "interface to send from (pcap device or TUN/TAP device name; omit to auto-detect the primary NIC)")
		ifaceType = flag.String("ifacetype", "pcap", "interface type: pcap | tap")
		to        = flag.String("to", "", "recipient as \"<name>,<protocol>\" (protocol: nbf | nbipx; required)")
		from      = flag.String("from", "CLASSICSTACK", "sender name (the From field)")
		text      = flag.String("text", "", "message text (required)")
		macFlag   = flag.String("mac", "", "source MAC for our virtual station (default: random locally-administered)")
		verbose   = flag.Bool("v", false, "verbose wire trace to stderr")
		listIf    = flag.Bool("list-ifaces", false, "list the capturable pcap NICs (the names -iface accepts) and exit")
		version   = flag.Bool("version", false, "print version information and exit")
	)
	flag.Usage = usage
	flag.Parse()
	trace.SetVerbose(*verbose)

	if *version {
		buildinfo.Print(os.Stdout, "csnetsend", BuildVersion, BuildCommit, BuildDate)
		return nil
	}

	if *listIf {
		clientlink.PrintInterfaces(os.Stdout)
		return nil
	}

	// Auto-detect the host's primary (default-route) NIC when -iface is omitted, so
	// "Easy mode" works on a single-NIC box — the same detection the file client and
	// csncpinfo use. The Messenger datagram rides a raw-Ethernet carrier (pcap/tap), so
	// ResolveIface fills a blank device name and announces the choice; ltoudp-style
	// non-NIC kinds do not apply here.
	ifaceName := csconnect.ResolveIface(*ifaceType, *iface)

	if ifaceName == "" || *to == "" || *text == "" {
		flag.Usage()
		return fmt.Errorf("-iface, -to and -text are required")
	}

	// Parse "<name>,<protocol>" into a Messenger-addressed target (name-type <03>).
	target, err := netbios.ParseTarget(*to, netbios.MessengerNameType)
	if err != nil {
		return err
	}

	// Parse an optional pinned virtual-station MAC (else netbios.OpenerFor synthesises a
	// random locally-administered one from the zero value, so the client never borrows the
	// host NIC's identity). Uses the shared csconnect parser the whole tool ring shares.
	var mac [6]byte
	if *macFlag != "" {
		var err error
		if mac, err = csconnect.ParseMAC(*macFlag); err != nil {
			return err
		}
	}

	// Build the raw-Ethernet opener for the chosen interface type (pcap or the
	// libpcap-free TUN/TAP), the same way the SMB file client selects its transport.
	opener, err := netbios.OpenerFor(*ifaceType, ifaceName, mac)
	if err != nil {
		return err
	}

	// Our own station name (the datagram Source), derived from the station MAC.
	station := netbios.DefaultStationName(opener.MAC, netbios.NameTypeWorkstation)
	conn, err := netbios.Open(opener, target.Protocol, station)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	if err := conn.SendMessage(target.Name, netbios.Message{From: *from, To: target.Name.String(), Text: *text}); err != nil {
		return fmt.Errorf("send: %w", err)
	}
	fmt.Printf("sent %q to %s over %s\n", *text, target.Name.String(), target.Protocol)
	return nil
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: csnetsend -iface <dev> -to <name>,<protocol> -text <msg> [flags]")
	fmt.Fprintln(os.Stderr, "  sends a NetBIOS Messenger (\"net send\") pop-up over a raw interface.")
	fmt.Fprintln(os.Stderr, "  ifacetype: pcap (libpcap/Npcap NIC) | tap (Linux TUN/TAP)")
	fmt.Fprintln(os.Stderr, "  protocol: nbf (NetBEUI) | nbipx (NetBIOS-over-IPX)")
	flag.PrintDefaults()
}
