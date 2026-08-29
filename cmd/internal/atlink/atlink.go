// Package atlink is a thin compatibility shim over client/link for the AppleTalk probe
// utilities (csecho, csnbp, csgetzones): it keeps the -transport/-iface/-device/-baud
// flag surface those commands already bind, and delegates the actual transport opening
// to client/link (the promoted, generalised opener). New code should use client/link
// directly; this shim only preserves the existing probe utilities unchanged.
//
//   - ltoudp   (default): LToUDP multicast (239.192.76.84:1954) over -iface, LLAP framing.
//   - tashtalk:           a TashTalk serial adapter on -device at -baud, LLAP framing.
//   - pcap:               an EtherTalk NIC via libpcap on -iface, Ethernet/SNAP framing.
//
// All three converge on a link.DatagramLink that speaks DDP.
package atlink

import (
	"flag"
	"fmt"
	"io"
	"math/rand"

	"github.com/ObsoleteMadness/ClassicStack/client/link"
	corelink "github.com/ObsoleteMadness/ClassicStack/core/link"
)

// Transport names accepted by the -transport flag (mirrors client/link kinds).
const (
	TransportLToUDP   = link.KindLToUDP
	TransportTashTalk = link.KindTashTalk
	TransportPcap     = link.KindPcap
)

// Options holds the resolved transport selection, bound to flags by Flags.
type Options struct {
	Transport string // ltoudp | tashtalk | pcap
	Iface     string // ltoudp/pcap: interface (IPv4 address for ltoudp, device name for pcap)
	Device    string // tashtalk: serial device path (COM3, /dev/ttyUSB0)
	Baud      uint   // tashtalk: line speed (0 → adapter default)
	ListIface bool   // -list-ifaces: print capturable pcap NICs and exit (see PrintInterfaces)
	Claim     bool   // ltoudp/tashtalk: run a real LLAP ENQ/ACK node-claim (default true)
}

// Flags registers the transport-selection flags on fs and returns the Options the
// parsed values land in. The default transport is LToUDP.
func Flags(fs *flag.FlagSet) *Options {
	o := &Options{}
	fs.StringVar(&o.Transport, "transport", TransportLToUDP,
		"AppleTalk transport: ltoudp (default), tashtalk, or pcap")
	fs.StringVar(&o.Iface, "iface", "",
		"ltoudp: local IPv4 interface address (default: all multicast interfaces); pcap: NIC device name")
	fs.StringVar(&o.Device, "device", "",
		"tashtalk: serial device path (e.g. COM3 or /dev/ttyUSB0)")
	fs.UintVar(&o.Baud, "baud", 0,
		"tashtalk: serial line speed (0 → adapter default)")
	fs.BoolVar(&o.ListIface, "list-ifaces", false,
		"list the capturable pcap NICs (the names -iface accepts) and exit")
	fs.BoolVar(&o.Claim, "claim", true,
		"ltoudp/tashtalk: run a real LLAP ENQ/ACK node-claim for our address (-src is only the "+
			"desired first candidate; adds up to ~2s on a quiet segment). -claim=false asserts "+
			"-net/-src directly with no negotiation, the old behavior")
	return o
}

// PrintInterfaces writes the host's capturable pcap NICs to w — the shared -list-ifaces
// output for the AppleTalk probe utilities, delegating to client/link so the device names
// match those the file clients print (and that -iface accepts). It never returns an error;
// a listing failure is reported in-band.
func PrintInterfaces(w io.Writer) { link.PrintInterfaces(w) }

// Open builds the selected transport as a DDP DatagramLink, delegating to client/link.
// network and srcNode are this client's AppleTalk address for the LocalTalk framers
// (the EtherTalk framer broadcasts and ignores them); when o.Claim is set (the
// default) srcNode is only the desired first LLAP node-claim candidate, and the
// second return value is the node actually claimed (which may differ). With Claim
// false, or on a non-LocalTalk transport, the returned node is srcNode unchanged.
//
// srcNode == 0 means "no explicit candidate": with Claim set, Open generates one at
// random from the LocalTalk workstation range (Inside AppleTalk, 2nd ed., Appendix B —
// "IF hint > 0 THEN MyAddress := hint ELSE ... Random(127) + 1" for a non-server node),
// rather than defaulting to a fixed low node like 1 that's likely to already be taken.
// Without Claim there is no negotiation to resolve an unspecified address, so srcNode
// == 0 is an error.
//
// The caller closes the link.
func (o *Options) Open(network uint16, srcNode uint8) (corelink.DatagramLink, uint8, error) {
	if srcNode == 0 {
		if !o.Claim {
			return nil, 0, fmt.Errorf("src node 0 requires -claim (asserting an address with -claim=false needs an explicit -src 1..254)")
		}
		srcNode = uint8(1 + rand.Intn(127)) //nolint:gosec // address pick, not a security boundary
	}
	name := o.Iface
	if o.Transport == link.KindTashTalk {
		name = o.Device
	}
	opener := &link.Opener{
		Spec:      link.Spec{Kind: o.Transport, Name: name, Baud: o.Baud},
		Net:       network,
		Node:      srcNode,
		ClaimNode: o.Claim,
	}
	dl, err := opener.DatagramLinkDDP()
	if err != nil {
		return nil, 0, err
	}
	return dl, opener.Node, nil
}
