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
	return o
}

// Open builds the selected transport as a DDP DatagramLink, delegating to client/link.
// network and srcNode are this client's asserted AppleTalk address for the LocalTalk
// framers (the EtherTalk framer broadcasts and ignores them). The caller closes the link.
func (o *Options) Open(network uint16, srcNode uint8) (corelink.DatagramLink, error) {
	name := o.Iface
	if o.Transport == link.KindTashTalk {
		name = o.Device
	}
	opener := &link.Opener{
		Spec: link.Spec{Kind: o.Transport, Name: name, Baud: o.Baud},
		Net:  network,
		Node: srcNode,
	}
	return opener.DatagramLinkDDP()
}
