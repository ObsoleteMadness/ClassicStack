package ncp

import (
	"crypto/rand"
	"fmt"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
)

// ipx.go is the NCP-over-IPX CLIENT transport: the client mirror of the server's
// core/service/ncp IPX listener. NCP rides IPX socket 0x0451 (type 17, NCP) — one IPX
// datagram carries one whole NCP request or reply, connectionless. It reuses the shared
// IPX datagram codec (core/protocol/ipx) and the same Ethernet II encapsulation the
// server's core/port/ipx port speaks, over a raw pcap FrameLink.
//
// Connection model: the client sends CreateConnection to the IPX broadcast node
// (all-ones → broadcast MAC on Ethernet), learns the server's real node from the first
// reply, and addresses every later request to it. NCP correlates a reply to its request
// by the (sequence, connection-number) pair in the reply header, so the read loop
// matches an inbound reply against the request in flight before delivering it — a late
// or duplicated datagram cannot satisfy the wrong Send. The session above serialises
// Sends, so at most one request is in flight.

// ncpSocket is the IPX socket the NCP file service listens on (0x0451), matching the
// server's core/protocol/ncp.NCPSocket.
var ncpSocket = [2]byte{0x04, 0x51}

// ipxNCPType is the IPX packet type NCP rides (17 = NCP), matching the server transport.
const ipxNCPType uint8 = 0x11

// etherTypeIPX is the Ethernet II EtherType for IPX (0x8137).
const etherTypeIPX = 0x8137

// ethHdrLen is the Ethernet II header length (dst6 + src6 + type2).
const ethHdrLen = 14

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

	mu         sync.Mutex
	serverNode [6]byte // learned from the first reply (broadcast → real node)
	serverNet  [4]byte
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

// DialIPX builds an NCP-over-IPX transport over the pcap FrameLink fl. srcMAC is this
// virtual station's hardware address (the IPX source node): pass RandomMAC() for a
// synthetic station (the default) or a user-specified MAC to pin it. The first request
// is broadcast and the server node is learned from its reply. The caller has opened fl
// with an "ipx" BPF filter.
func DialIPX(fl link.FrameLink, srcMAC [6]byte) Transport {
	t := &ipxTransport{
		fl:     fl,
		srcMAC: srcMAC,
		respCh: make(chan []byte, 4),
		stop:   make(chan struct{}),
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
	dstNode := broadcastNode
	dstNet := t.srcNet
	if t.haveServer {
		dstNode = t.serverNode
		dstNet = t.serverNet
	}
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
	if err := t.writeDatagram(d, dstNode); err != nil {
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

// writeDatagram encapsulates an IPX datagram in an Ethernet II frame to dstMAC and
// writes it to the link. On Ethernet the destination MAC is the IPX destination node.
func (t *ipxTransport) writeDatagram(d *ipxproto.Datagram, dstMAC [6]byte) error {
	ipxBytes, err := d.Encode(nil)
	if err != nil {
		return err
	}
	frame := make([]byte, 0, ethHdrLen+len(ipxBytes))
	frame = append(frame, dstMAC[:]...)
	frame = append(frame, t.srcMAC[:]...)
	frame = append(frame, byte(etherTypeIPX>>8), byte(etherTypeIPX&0xFF))
	frame = append(frame, ipxBytes...)
	return t.fl.Write(frame)
}

// readLoop reads frames, strips the Ethernet/IPX encapsulation, and delivers NCP reply
// datagrams addressed to our node+socket to the pending Send, matched by (sequence,
// connection). It learns the server node from the first reply.
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
		payload, ok := stripIPXEncapsulation(frame)
		if !ok {
			continue
		}
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
		// Correlate against the request in flight. The CreateConnection reply carries
		// the newly-assigned connection number (the request was sent with conn 0), so a
		// reply whose sequence matches while we still have conn 0 outstanding is accepted
		// regardless of its connection number; every later exchange matches both.
		match := t.waiting && respSeq == t.waitSeq &&
			(respConn == t.waitConn || t.waitConn == 0)
		if !match {
			t.mu.Unlock()
			continue
		}
		if !t.haveServer {
			t.serverNode = d.SrcNode
			t.serverNet = d.SrcNet
			t.haveServer = true
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

// stripIPXEncapsulation returns the IPX datagram bytes from an Ethernet frame,
// accepting Ethernet II (0x8137), raw 802.3 (0xFFFF magic), and 802.2 LLC
// (DSAP=SSAP=0xE0) — the same three framings core/port/ipx accepts, so the client reads
// whatever encapsulation the server sends. The bool is false when the frame is not a
// recognised IPX encapsulation.
func stripIPXEncapsulation(frame []byte) ([]byte, bool) {
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
			return body, true // raw 802.3 IPX (no checksum → 0xFFFF magic)
		}
		if body[0] == 0xE0 && body[1] == 0xE0 && body[2] == 0x03 {
			return body[3:], true // 802.2 LLC UI (DSAP=SSAP=0xE0, control=0x03)
		}
	}
	return nil, false
}

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
