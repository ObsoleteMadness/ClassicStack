// Package atlink is the shared AppleTalk-transport opener for the client utilities
// (csecho, csnbp, csgetzones). It picks one of the three AppleTalk segment transports
// behind a single -transport flag and returns a ready link.DatagramLink that speaks
// DDP, so each client holds one transport-agnostic loop instead of hardwiring LToUDP.
//
//   - ltoudp   (default): LToUDP multicast (239.192.76.84:1954) over -iface, LLAP framing.
//   - tashtalk:           a TashTalk serial adapter on -device at -baud, LLAP framing.
//   - pcap:               an EtherTalk NIC via libpcap on -iface, Ethernet/SNAP framing.
//     Requires the 'pcap' build tag; without it Open returns an error.
//
// All three converge on a link.DatagramLink, the same type each client already drove
// after framing, so the clients change only at the open site. This lives under
// cmd/internal (the compose/cmd edge) because it imports the adapter ring (ltoudp,
// tashtalk, serial, pcap) which core and compose/runtime must not.
package atlink

import (
	"flag"
	"fmt"
	"net"

	"github.com/ObsoleteMadness/ClassicStack/adapter/link/framing"
	"github.com/ObsoleteMadness/ClassicStack/adapter/link/ltoudp"
	"github.com/ObsoleteMadness/ClassicStack/adapter/link/pcap"
	"github.com/ObsoleteMadness/ClassicStack/adapter/link/tashtalk"
	"github.com/ObsoleteMadness/ClassicStack/adapter/serial"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
)

// Transport names accepted by the -transport flag.
const (
	TransportLToUDP   = "ltoudp"
	TransportTashTalk = "tashtalk"
	TransportPcap     = "pcap"
)

// Options holds the resolved transport selection, bound to flags by Flags. A client
// registers the flags, parses, then calls Open.
type Options struct {
	Transport string // ltoudp | tashtalk | pcap
	Iface     string // ltoudp/pcap: interface (IPv4 address for ltoudp, device name for pcap)
	Device    string // tashtalk: serial device path (COM3, /dev/ttyUSB0)
	Baud      uint   // tashtalk: line speed (0 → adapter default)
}

// Flags registers the transport-selection flags on fs and returns the Options the
// parsed values land in. Call after defining the client's own flags and before
// fs.Parse. The default transport is LToUDP, so a client run with no transport flag
// behaves exactly as before this package existed.
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

// Open builds the selected transport and frames it as a DDP DatagramLink. network and
// srcNode are this client's asserted AppleTalk address (no node-claim handshake — a
// probe client asserts one), used by the LocalTalk framers; the EtherTalk framer
// broadcasts (no AARP) and ignores them on the wire. The caller closes the returned
// link.
func (o *Options) Open(network uint16, srcNode uint8) (link.DatagramLink, error) {
	switch o.Transport {
	case TransportLToUDP, "":
		return openLToUDP(o.Iface, network, srcNode)
	case TransportTashTalk:
		return openTashTalk(o.Device, o.Baud, network, srcNode)
	case TransportPcap:
		return openPcap(o.Iface, network, srcNode)
	default:
		return nil, fmt.Errorf("unknown -transport %q (want %s, %s, or %s)",
			o.Transport, TransportLToUDP, TransportTashTalk, TransportPcap)
	}
}

// openLToUDP opens the LToUDP multicast segment with LLAP framing.
func openLToUDP(iface string, network uint16, srcNode uint8) (link.DatagramLink, error) {
	fl, err := ltoudp.Open(ltoudp.DefaultConfig(iface))
	if err != nil {
		return nil, fmt.Errorf("open LToUDP: %w", err)
	}
	return frameLocalTalk(fl, network, srcNode)
}

// openTashTalk opens a TashTalk serial adapter with LLAP framing.
func openTashTalk(device string, baud uint, network uint16, srcNode uint8) (link.DatagramLink, error) {
	if device == "" {
		return nil, fmt.Errorf("tashtalk transport needs -device (a serial port path)")
	}
	s, err := serial.Open(serial.Config{Device: device, Baud: baud})
	if err != nil {
		return nil, fmt.Errorf("open serial %s: %w", device, err)
	}
	fl, err := tashtalk.NewStream(s)
	if err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("frame TashTalk: %w", err)
	}
	return frameLocalTalk(fl, network, srcNode)
}

// openPcap opens an EtherTalk NIC via libpcap with Ethernet/SNAP DDP framing. The
// source MAC is resolved from the interface; the framer broadcasts (no AARP), which
// is what a probe client needs.
func openPcap(iface string, network uint16, srcNode uint8) (link.DatagramLink, error) {
	_ = network
	_ = srcNode
	if iface == "" {
		return nil, fmt.Errorf("pcap transport needs -iface (a NIC device name)")
	}
	fl, err := pcap.Open(pcap.DefaultEtherTalkConfig(iface))
	if err != nil {
		return nil, fmt.Errorf("open pcap %s: %w", iface, err)
	}
	framer := &framing.EtherTalk{SrcMAC: interfaceMAC(iface)}
	dl, err := framer.Framing(fl)
	if err != nil {
		_ = fl.Close()
		return nil, fmt.Errorf("frame EtherTalk: %w", err)
	}
	return dl, nil
}

// frameLocalTalk wraps a FrameLink with the LLAP framer asserting our static address.
func frameLocalTalk(fl link.FrameLink, network uint16, srcNode uint8) (link.DatagramLink, error) {
	framer := &framing.LocalTalk{Addr: framing.NewStaticAddr(network, srcNode)}
	dl, err := framer.Framing(fl)
	if err != nil {
		_ = fl.Close()
		return nil, fmt.Errorf("frame LocalTalk: %w", err)
	}
	return dl, nil
}

// interfaceMAC resolves the named interface's hardware address, or nil if it cannot be
// resolved (the EtherTalk framer then stamps a zero source MAC, which still reaches a
// broadcast responder — the reply comes back to the AppleTalk broadcast MAC).
func interfaceMAC(name string) []byte {
	if ifi, err := net.InterfaceByName(name); err == nil && len(ifi.HardwareAddr) == 6 {
		return append([]byte(nil), ifi.HardwareAddr...)
	}
	return nil
}
