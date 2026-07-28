package ncp

import (
	"crypto/rand"
	"fmt"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
	ipxport "github.com/ObsoleteMadness/ClassicStack/core/port/ipx"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
)

// ipx.go is the NCP-over-IPX CLIENT transport: the client mirror of the server's
// core/service/ncp IPX listener. NCP rides IPX socket 0x0451 (type 17, NCP) — one IPX
// datagram carries one whole NCP request or reply, connectionless. It reuses the shared
// IPX datagram codec (core/protocol/ipx) and the same Ethernet II encapsulation the
// server's core/port/ipx port speaks, over a raw pcap FrameLink.
//
// Connection model: the client sends CreateConnection to the IPX broadcast node
// (all-ones → broadcast MAC on Ethernet), learns the server's real node AND network
// from the first reply, and addresses every later request to it. NCP correlates a reply
// to its request by the (sequence, connection-number) pair in the reply header, so the
// read loop matches an inbound reply against the request in flight before delivering it —
// a late or duplicated datagram cannot satisfy the wrong Send. The session above
// serialises Sends, so at most one request is in flight.
//
// Frame type: a real NetWare server is frequently bound to raw 802.3 or 802.2 LLC rather
// than Ethernet II (NetWare 3.x defaulted to raw 802.3, 4.x to 802.2), and each Ethernet
// frame type is a DISTINCT logical IPX network on the same wire — so an Ethernet-II
// request never reaches a server bound only on 802.2. The transport therefore LEARNS the
// server's frame type from the encapsulation of the first frame it receives from the
// server and frames every later request in that same type (the NETx/VLM behaviour),
// unless the caller pins one explicitly. The initial broadcast goes out in the pinned or
// default frame type; a server answering any framing is then matched.

// ncpSocket is the IPX socket the NCP file service listens on (0x0451), matching the
// server's core/protocol/ncp.NCPSocket.
var ncpSocket = [2]byte{0x04, 0x51}

// ipxNCPType is the IPX packet type NCP rides (17 = NCP), matching the server transport.
const ipxNCPType uint8 = 0x11

// broadcastNode is the IPX broadcast node (all-ones); on Ethernet the IPX node IS the
// MAC, so a broadcast node yields a broadcast destination MAC (core/router/ipx).
var broadcastNode = [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

// NCP reply-header offsets the transport reads for correlation (ncp.go ReplyHeader):
// type(2 BE) at 0, sequence at 2, conn-low at 3, task at 4, conn-high at 5.
const (
	ncpTypeOffset = 0
	ncpSeqOffset  = 2
	ncpConnLow    = 3
	ncpConnHigh   = 5
)

// ncpTypeReply is the NCP reply request-type (TypeReply 0x3333) a server→client packet
// carries; the transport only delivers packets of this type (and the create-connection
// echo, 0x1111) as replies. Matching core/protocol/ncp.TypeReply.
const (
	ncpTypeReply         uint16 = 0x3333
	ncpTypeCreateConnRep uint16 = 0x1111
	ncpTypePositiveAck   uint16 = 0x9999 // "request being processed" keep-alive; skipped
)

// ipxRequestTimeout bounds how long the client waits for a reply datagram before
// giving up on one Send. A bounded wait avoids a hang on a lost datagram; the session
// surfaces the error.
const ipxRequestTimeout = 5 * time.Second

// ipxMaxPayload is the largest NCP reply BODY this transport can carry back in one IPX
// datagram over Ethernet. NCP is connectionless with no reassembly, so a reply must fit
// one Ethernet frame: 1500-byte payload − 30-byte IPX header − 8-byte NCP reply header
// leaves ~1462. 1024 matches the server's Ethernet read/write buffer (mars_nwe
// RW_BUFFERSIZE), a conservative cap that keeps the whole frame under the MTU. The
// session bounds Read/Write sizes by this so a read reply never overflows a datagram.
const ipxMaxPayload = 1024

// ipxTransport is the NCP-over-IPX client transport. It owns the pcap FrameLink, runs a
// read loop demultiplexing inbound IPX/NCP reply datagrams to the pending Send, and
// applies the learned server node to each outbound request.
type ipxTransport struct {
	fl     link.FrameLink
	srcMAC [6]byte
	srcNet [4]byte

	// frameType is the Ethernet encapsulation used on OUTBOUND requests. It starts at
	// the pinned/default type and, unless pinned, is overwritten with the type learned
	// from the first frame received from the server (frameTypePinned guards that). This
	// lets the client reach a real NetWare server bound on raw-802.3 / 802.2 rather than
	// Ethernet II.
	frameType       ipxport.FrameType
	frameTypePinned bool

	mu         sync.Mutex
	serverNode [6]byte // IPX node of the server (from the reply's IPX header)
	serverNet  [4]byte // IPX network of the server (may be its INTERNAL net, a hop away)
	serverMAC  [6]byte // Ethernet source MAC of the reply frame — the L2 next hop (the
	// router's cable MAC when the server is on an internal net), which is where later
	// unicast requests are addressed at layer 2 even though the IPX DstNode is serverNode.
	haveServer bool

	// Pending-request correlation. This connectionless transport carries no per-request
	// demux of its own, so the read loop matches an inbound reply to the request in
	// flight by (sequence, connection) before delivering it. The session serialises
	// Sends, so at most one request is outstanding; waitSeq/waitConn name it.
	waiting  bool
	waitSeq  uint8
	waitConn uint16

	respCh chan []byte
	stop   chan struct{}
	closed bool
}

// RandomMAC generates a locally-administered, unicast MAC for the client's virtual IPX
// station. The client is a distinct station on the segment the pcap device bridges, NOT
// the host itself, so it presents its own node address rather than borrow the host
// NIC's MAC (which would collide). The first octet has the locally-administered bit set
// and the group bit clear; the rest are random.
func RandomMAC() [6]byte {
	var mac [6]byte
	_, _ = rand.Read(mac[:])
	mac[0] = (mac[0] | 0x02) &^ 0x01 // locally-administered, unicast
	return mac
}

// DialIPX builds an NCP-over-IPX transport over the pcap FrameLink fl in the default
// (learned) frame type. See DialIPXFrame to pin a frame type.
func DialIPX(fl link.FrameLink, srcMAC [6]byte) Transport {
	return DialIPXFrame(fl, srcMAC, ipxport.DefaultFrameType, false)
}

// DialIPXFrame builds an NCP-over-IPX transport over the pcap FrameLink fl. srcMAC is
// this virtual station's hardware address (the IPX source node): pass RandomMAC() for a
// synthetic station (the default) or a user-specified MAC to pin it. frameType is the
// Ethernet encapsulation used on the initial broadcast request; when pinned is false the
// transport LEARNS the server's frame type from the first frame it receives and switches
// to it, so it reaches a real NetWare server bound on raw-802.3 / 802.2 rather than
// Ethernet II. When pinned is true frameType is used for every request and never
// relearned. The first request is broadcast and the server node + network are learned
// from its reply. The caller has opened fl with an "ipx" BPF filter.
func DialIPXFrame(fl link.FrameLink, srcMAC [6]byte, frameType ipxport.FrameType, pinned bool) Transport {
	t := &ipxTransport{
		fl:              fl,
		srcMAC:          srcMAC,
		frameType:       frameType,
		frameTypePinned: pinned,
		respCh:          make(chan []byte, 4),
		stop:            make(chan struct{}),
	}
	go t.readLoop()
	return t
}

// ServerAddr is a NetWare server's resolved IPX address plus the Ethernet next hop and
// framing to reach it — what a SAP query yields (see resolve.go). Net/Node is the server's
// IPX identity (often its INTERNAL network, node 1); MAC is the L2 next hop the SAP reply
// came from (the server's cable NIC, or a router's, when the service is on an internal
// net); FrameType is the encapsulation that reply used.
type ServerAddr struct {
	Net       [4]byte
	Node      [6]byte
	MAC       [6]byte
	FrameType ipxport.FrameType
}

// DialIPXResolved builds an NCP-over-IPX transport pre-seeded with a server address
// resolved out of band (via SAP — resolve.go). Unlike the broadcast-and-learn path, the
// FIRST CreateConnection is addressed straight to the server's IPX net/node and unicast to
// the next-hop MAC in the resolved frame type, so it reaches a server whose NCP service
// lives on an internal network a router hop away (a net-0 broadcast never routes there).
// When pinned is false the frame type may still be refined from the first reply.
func DialIPXResolved(fl link.FrameLink, srcMAC [6]byte, srv ServerAddr, pinned bool) Transport {
	t := &ipxTransport{
		fl:              fl,
		srcMAC:          srcMAC,
		frameType:       srv.FrameType,
		frameTypePinned: pinned,
		serverNode:      srv.Node,
		serverNet:       srv.Net,
		serverMAC:       srv.MAC,
		haveServer:      true,
		respCh:          make(chan []byte, 4),
		stop:            make(chan struct{}),
	}
	go t.readLoop()
	return t
}

// Send transmits one NCP request as an IPX datagram and returns the matching reply. The
// destination is the learned server node (broadcast on the first, pre-attach request).
func (t *ipxTransport) Send(req []byte) ([]byte, error) {
	if len(req) < 6 {
		return nil, fmt.Errorf("ncp/ipx: request shorter than an NCP header")
	}
	reqSeq := req[ncpSeqOffset]
	reqConn := uint16(req[ncpConnLow]) | uint16(req[ncpConnHigh])<<8

	t.mu.Lock()
	// Layer-2 destination (Ethernet) and layer-3 destination (IPX header) are DISTINCT
	// once the server is learned: the IPX packet is addressed to the server's IPX node
	// (which may live on its internal network), but the frame is sent to the L2 next
	// hop — the router's cable MAC we saw the reply come from. Before the server is
	// learned both are broadcast.
	dstMAC := broadcastNode
	dstNode := broadcastNode
	dstNet := t.srcNet
	if t.haveServer {
		dstMAC = t.serverMAC
		dstNode = t.serverNode
		dstNet = t.serverNet
	}
	frameType := t.frameType
	if t.closed {
		t.mu.Unlock()
		return nil, ErrTransportClosed
	}
	// Drain any stale reply left in respCh from a prior timed-out Send, then register
	// this request as the one in flight.
	for {
		select {
		case <-t.respCh:
			continue
		default:
		}
		break
	}
	t.waiting = true
	t.waitSeq = reqSeq
	t.waitConn = reqConn
	t.mu.Unlock()
	defer func() {
		t.mu.Lock()
		t.waiting = false
		t.mu.Unlock()
	}()

	d := &ipxproto.Datagram{
		Type:    ipxNCPType,
		DstNet:  dstNet,
		DstNode: dstNode,
		DstSock: ncpSocket,
		SrcNet:  t.srcNet,
		SrcNode: t.srcMAC,
		SrcSock: ncpSocket,
		Payload: req,
	}
	if err := t.writeDatagram(d, dstMAC, frameType); err != nil {
		return nil, err
	}

	select {
	case resp := <-t.respCh:
		return resp, nil
	case <-time.After(ipxRequestTimeout):
		return nil, fmt.Errorf("ncp/ipx: no reply within %s", ipxRequestTimeout)
	case <-t.stop:
		return nil, ErrTransportClosed
	}
}

// MaxPayload is the datagram-safe reply-body cap (one IPX datagram, no reassembly),
// used by the session to bound Read/Write sizes.
func (t *ipxTransport) MaxPayload() int { return ipxMaxPayload }

// writeDatagram encapsulates an IPX datagram in an Ethernet frame of frameType to
// dstMAC and writes it to the link. The frame type is the pinned/default type or the
// one learned from the server; the encapsulation itself is done through the same
// core/port/ipx logic the server uses.
func (t *ipxTransport) writeDatagram(d *ipxproto.Datagram, dstMAC [6]byte, frameType ipxport.FrameType) error {
	ipxBytes, err := d.Encode(nil)
	if err != nil {
		return err
	}
	return t.fl.Write(frameType.Encapsulate(dstMAC, t.srcMAC, ipxBytes))
}

// readLoop reads frames, strips the Ethernet/IPX encapsulation, and delivers NCP reply
// datagrams addressed to our node+socket to the pending Send, matched by (sequence,
// connection). From the first matched reply it learns the server's IPX node+net, the L2
// next-hop MAC (the reply frame's Ethernet source), and — unless the frame type was
// pinned — the encapsulation the server speaks, so later requests reach a server bound
// on raw-802.3 / 802.2 rather than Ethernet II.
func (t *ipxTransport) readLoop() {
	for {
		frame, err := t.fl.Read()
		if err != nil {
			if err == link.ErrTimeout {
				select {
				case <-t.stop:
					return
				default:
					continue
				}
			}
			return // terminal (ErrClosed or other)
		}
		payload, frameType, ok := ipxport.Strip(frame)
		if !ok {
			continue
		}
		// The Ethernet source MAC is the L2 next hop to the server (its own NIC MAC, or a
		// router's cable MAC when the server sources replies from an internal network).
		var srcMAC [6]byte
		copy(srcMAC[:], frame[6:12])
		d, err := ipxproto.Decode(payload)
		if err != nil || d.Type != ipxNCPType {
			continue
		}
		msg := d.Payload
		if len(msg) < 8 {
			continue // shorter than an NCP reply header
		}
		if d.DstSock != ncpSocket || d.DstNode != t.srcMAC {
			continue
		}
		typ := uint16(msg[ncpTypeOffset])<<8 | uint16(msg[ncpTypeOffset+1])
		if typ == ncpTypePositiveAck {
			continue // "request being processed" keep-alive: keep waiting
		}
		if typ != ncpTypeReply && typ != ncpTypeCreateConnRep {
			continue // not a reply (another client's request, or our own echo)
		}
		respSeq := msg[ncpSeqOffset]
		respConn := uint16(msg[ncpConnLow]) | uint16(msg[ncpConnHigh])<<8

		t.mu.Lock()
		// Correlate against the request in flight. Ordinarily the reply's (sequence,
		// connection) must match the outstanding request. The CreateConnection exchange is
		// the exception: the request goes out with connection 0, and a real NetWare server
		// answers it from its internal address with the newly-assigned connection number AND
		// a sequence of its own (observed: our request seq 1, NW 4.1's reply seq 0). So while
		// we are still pre-connection (waitConn == 0) — i.e. the create exchange — accept the
		// reply on the strength of it being the only request in flight, regardless of its
		// sequence or connection number. Every later exchange matches both strictly.
		preConnection := t.waitConn == 0
		match := t.waiting && (preConnection ||
			(respSeq == t.waitSeq && respConn == t.waitConn))
		if !match {
			t.mu.Unlock()
			continue
		}
		if !t.haveServer {
			t.serverNode = d.SrcNode
			t.serverNet = d.SrcNet
			t.serverMAC = srcMAC
			t.haveServer = true
			// Learn the server's frame type from its reply unless the caller pinned one.
			if !t.frameTypePinned {
				t.frameType = frameType
			}
		}
		t.mu.Unlock()

		select {
		case t.respCh <- append([]byte(nil), msg...):
		case <-t.stop:
			return
		default:
			// No pending Send (a duplicate/late reply): drop it.
		}
	}
}

// (Frame demux is provided by core/port/ipx.Strip, which additionally reports the
// detected frame type so the transport can learn the server's encapsulation.)

// Close stops the read loop and closes the link.
func (t *ipxTransport) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	close(t.stop)
	t.mu.Unlock()
	return t.fl.Close()
}
