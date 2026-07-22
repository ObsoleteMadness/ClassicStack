// Package link is the client-side transport opener: it turns a transport selection
// (kind + name) into one of the link views a protocol client needs — a raw
// core/link.FrameLink, a DDP-framed core/link.DatagramLink, or a net.Conn for TCP.
//
// It generalises cmd/internal/atlink (which only produced a DDP DatagramLink for the
// AppleTalk probe utilities) so a single Opener serves every scheme: AFP wants a DDP
// DatagramLink, SMB/NCP over IPX or NetBEUI want a raw FrameLink, SMB/NCP over TCP
// want a net.Conn, and EtherDFS wants a raw Ethernet FrameLink. atlink stays as a thin
// shim over this package so csecho/csnbp/csgetzones keep building.
//
// Transport kinds (Spec.Kind):
//
//	pcap:<device>       a NIC via libpcap/Npcap (needs the 'pcap' build tag)
//	ltoudp:<iface>      LToUDP multicast (239.192.76.84:1954) on a local IPv4 iface
//	tashtalk:<device>   a TashTalk serial adapter at <device> (COM3, /dev/ttyUSB0)
//	tcp:<host>          a TCP dial target (host or host:port)
//	inmem               an in-memory loopback/pair (tests only)
//
// Ring: CLIENT (may import adapter/, unlike core/).
package link

import (
	"fmt"
	"math/rand"
	"net"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/adapter/link/framing"
	"github.com/ObsoleteMadness/ClassicStack/adapter/link/inmem"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
)

// Transport kind names.
const (
	KindPcap     = "pcap"
	KindLToUDP   = "ltoudp"
	KindTashTalk = "tashtalk"
	KindTCP      = "tcp"
	KindInmem    = "inmem"
)

// Spec selects one transport instance: a kind plus the name it addresses (device,
// interface, or host), and the serial line speed for TashTalk.
type Spec struct {
	Kind string // pcap | ltoudp | tashtalk | tcp | inmem
	Name string // device / interface / host, as the kind interprets it
	Baud uint   // tashtalk only; 0 → adapter default
}

// ParseSpec parses "kind:name" (or a bare kind for inmem) into a Spec. The name may
// itself contain ':' (a tcp host:port), so only the FIRST ':' delimits the kind.
func ParseSpec(s string) (Spec, error) {
	kind, name, ok := strings.Cut(s, ":")
	kind = strings.ToLower(strings.TrimSpace(kind))
	if !ok {
		// A bare kind is valid for kinds that need no name (inmem).
		return Spec{Kind: kind}, nil
	}
	return Spec{Kind: kind, Name: strings.TrimSpace(name)}, nil
}

// Opener holds a transport Spec and the AppleTalk source address the LocalTalk/
// EtherTalk framers assert when producing a DDP DatagramLink. One Opener is built per
// connection and handed to the scheme's factory through client.Options.
type Opener struct {
	Spec Spec
	// Net / Node is this client's asserted AppleTalk address for the DDP framers. A
	// probe client asserts one rather than running a node-claim handshake; the AFP
	// client may run a real LLAP/AARP claim above the FrameLink instead (see
	// client/atalk), in which case it opens a FrameLink and frames it itself.
	Net  uint16
	Node uint8
	// MAC, when non-zero, is the hardware address a raw-Ethernet client transport
	// (SMB-over-IPX, EtherDFS) presents as its virtual station's source node. The client
	// is a distinct station on the segment the pcap device bridges, not the host itself,
	// so it must NOT borrow the host NIC's MAC; a zero value tells the transport to
	// synthesise a locally-administered random MAC (the default). A CLI -mac flag pins it.
	MAC [6]byte
	// inmemPeer, when set, is the loopback peer a KindInmem opener hands back so an
	// in-process test can wire the client to a server over one frame pair.
	inmemFrame link.FrameLink
	// datagram, when set, is a pre-built DDP DatagramLink DatagramLinkDDP returns as-is
	// (bypassing framing). It is the in-process e2e seam: a test bridges this straight to
	// a running server's Inbound, so the whole AFP client stack (ATP requester, ASP
	// session, fs adapter) runs against the real service without a wire.
	datagram link.DatagramLink
}

// LLAP node-ID ranges (Inside AppleTalk, 2nd ed., §1 "LocalTalk Link Access Protocol",
// Node ID assignment). LLAP node IDs are 8-bit, partitioned so that transient client
// nodes cannot collide with persistent services:
//
//	0x00        reserved — "not allowed (unknown)"; never a deliverable address
//	0x01..0x7F  USER node IDs — workstations and clients (switched on/off frequently)
//	0x80..0xFE  SERVER node IDs — routers and persistent services (rarely restarted)
//	0xFF        broadcast
//
// The spec draws this line deliberately: "Excluding user (nonserver) node IDs from the
// server node ID range eliminates the possibility that user nodes ... will conflict with
// server nodes." A ClassicStack router/AFP server acquires from the server range and
// typically sits at 0xFE, so a client MUST take its node from the user range.
const (
	llapNodeReserved  uint8 = 0x00
	llapUserNodeMin   uint8 = 0x01
	llapUserNodeMax   uint8 = 0x7F
	llapBroadcastNode uint8 = 0xFF // for reference; a client never sources from here
)

// pickClientNode returns a random USER-range node id (0x01..0x7F) for a client that
// asserts an address without running a full LLAP ENQ/ACK acquisition. Staying in the
// user range guarantees it never collides with the server's node (server range,
// typically 0xFE); randomising within it means two concurrent clients on the same
// segment are unlikely to pick the same node. This is the "guess a candidate" step of
// the spec's acquisition algorithm without the ENQ verification burst — sufficient for a
// short-lived probe/file-client session on a simulated segment, and the seam a real
// LLAP node-claim would replace.
func pickClientNode() uint8 {
	span := int(llapUserNodeMax - llapUserNodeMin + 1) // 127 candidates
	return llapUserNodeMin + uint8(rand.Intn(span))
}

// NewOpener builds an Opener for spec with a default asserted AppleTalk address. The
// node is a random USER-range LLAP node (never 0, never the server range) so DDP replies
// are deliverable and do not collide with the server; callers that run a real node-claim
// or need a specific node set Opener.Node afterwards.
func NewOpener(spec Spec) *Opener {
	return &Opener{Spec: spec, Net: 0, Node: pickClientNode()}
}

// NewInmemOpener builds an Opener whose FrameLink is one end of an in-memory pair;
// peer is the other end, which a test hands to the server side. Both ends share the
// loopback so client↔server frames flow without hardware.
func NewInmemOpener(clientEnd link.FrameLink) *Opener {
	return &Opener{Spec: Spec{Kind: KindInmem}, inmemFrame: clientEnd}
}

// NewDatagramOpener builds an Opener that returns dl directly from DatagramLinkDDP,
// bypassing all framing. It is the in-process e2e seam: a test bridges dl to a running
// server's Inbound so the AFP client stack runs against the real service without a wire.
func NewDatagramOpener(dl link.DatagramLink) *Opener {
	return &Opener{Spec: Spec{Kind: KindInmem}, datagram: dl}
}

// FrameLink opens the raw L2 frame transport for the Opener's Spec: an EtherTalk /
// IPX / NetBEUI / EtherDFS carrier that a scheme frames itself. filter is the kernel
// BPF the pcap handle is narrowed to ("" = everything); a scheme passes its own
// (e.g. EtherDFS' EtherType, "ipx" for IPX) so it is not stuck with the EtherTalk
// preset. tcp is not a FrameLink (use Dial); ltoudp/tashtalk are DDP-only segments
// exposed via DatagramLinkDDP, not raw FrameLinks here.
func (o *Opener) FrameLink(filter string) (link.FrameLink, error) {
	switch o.Spec.Kind {
	case KindInmem:
		if o.inmemFrame != nil {
			return o.inmemFrame, nil
		}
		a, _ := inmem.Pair(16)
		return a, nil
	case KindPcap:
		return openPcapFrame(o.Spec.Name, filter)
	default:
		return nil, fmt.Errorf("link: kind %q has no raw FrameLink (use DatagramLinkDDP or Dial)", o.Spec.Kind)
	}
}

// DatagramLinkDDP opens a DDP-framed datagram transport for the Opener's Spec, over
// LToUDP, TashTalk, EtherTalk (pcap), or an in-memory pair. This is the AFP/NBP path.
func (o *Opener) DatagramLinkDDP() (link.DatagramLink, error) {
	if o.datagram != nil {
		return o.datagram, nil
	}
	switch o.Spec.Kind {
	case KindLToUDP, "":
		return openLToUDP(o.Spec.Name, o.Net, o.Node)
	case KindTashTalk:
		return openTashTalk(o.Spec.Name, o.Spec.Baud, o.Net, o.Node)
	case KindPcap:
		return openPcapDDP(o.Spec.Name, o.Net, o.Node)
	case KindInmem:
		fl, err := o.FrameLink("")
		if err != nil {
			return nil, err
		}
		return frameLocalTalk(fl, o.Net, o.Node)
	default:
		return nil, fmt.Errorf("link: kind %q cannot produce a DDP DatagramLink", o.Spec.Kind)
	}
}

// Dial opens a TCP connection to the Opener's Spec name (host or host:port),
// defaulting to defPort when the name carries no port. This is the SMB-over-TCP /
// AFP-over-DSI path (DSI is a future adapter; TCP is provided for SMB now).
func (o *Opener) Dial(defPort string) (net.Conn, error) {
	if o.Spec.Kind != KindTCP {
		return nil, fmt.Errorf("link: kind %q is not tcp", o.Spec.Kind)
	}
	host := o.Spec.Name
	if _, _, err := net.SplitHostPort(host); err != nil {
		host = net.JoinHostPort(host, defPort)
	}
	return net.Dial("tcp", host)
}

// frameLocalTalk wraps a FrameLink with the LLAP framer asserting a static address.
func frameLocalTalk(fl link.FrameLink, network uint16, srcNode uint8) (link.DatagramLink, error) {
	framer := &framing.LocalTalk{Addr: framing.NewStaticAddr(network, srcNode)}
	dl, err := framer.Framing(fl)
	if err != nil {
		_ = fl.Close()
		return nil, fmt.Errorf("frame LocalTalk: %w", err)
	}
	return dl, nil
}
