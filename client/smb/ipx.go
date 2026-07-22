package smb

import (
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
)

// ipx.go is the SMB direct-hosted-over-IPX CLIENT transport: the client mirror of the
// server's core/service/smb.DirectIPX. SMB rides straight on IPX (socket 0x0550, type 4
// PEP) with NO NetBIOS name/session layer — one IPX datagram carries one whole SMB
// message, connectionless ([MS-CIFS] §2.2.1.6.4). It reuses the shared IPX datagram
// codec (core/protocol/ipx) and speaks the same Ethernet II encapsulation the server's
// core/port/ipx port uses, over a raw pcap FrameLink.
//
// Connection model: the client sends NEGOTIATE to the IPX broadcast node (all-ones →
// broadcast MAC on Ethernet, core/router/ipx), learns the server's real node from the
// first response, and addresses every later message to it. The server assigns a
// Connection ID (CID) on NEGOTIATE and stamps it into the SMB header SecurityFeatures
// field (bytes 18-19); the client echoes it on subsequent messages, which the server's
// stampConnectionless honours. This transport tracks the learned server node + CID and
// applies them transparently, so the session layer above sees a plain request→response
// Transport.

// directSMBSocket is the IPX socket direct-hosted SMB listens on (0x0550), matching the
// server's smb.DirectSMBSocket.
var directSMBSocket = [2]byte{0x05, 0x50}

// ipxPEPType is the IPX packet type direct-hosted SMB rides (4 = Packet Exchange
// Protocol), matching the server transport and NBIPX session traffic.
const ipxPEPType uint8 = 0x04

// etherTypeIPX is the Ethernet II EtherType for IPX (0x8137) — the framing MacIPX and
// this client's server default speak (core/port/ipx).
const etherTypeIPX = 0x8137

// ethHdrLen is the Ethernet II header length (dst6 + src6 + type2).
const ethHdrLen = 14

// broadcastNode is the IPX broadcast node (all-ones); on Ethernet the IPX node IS the
// MAC, so a broadcast node yields a broadcast destination MAC (core/router/ipx).
var broadcastNode = [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

// SMB header offsets the connectionless framing reads/writes ([MS-CIFS] §2.2.3.1).
const (
	smbCommandOffset = 4  // Command (UCHAR)
	smbFlagsOffset   = 9  // Flags: bit 7 (0x80) = reply
	smbCIDOffset     = 18 // SecurityFeatures: Connection ID (USHORT LE)
	smbMIDOffset     = 30 // MID: multiplex ID (USHORT LE)
	smbReplyFlag     = 0x80
)

// ipxRequestTimeout bounds how long the client waits for a response datagram before
// giving up on one Send (a lost datagram over the connectionless transport). The
// session layer retries at a higher level if needed; a bounded wait avoids a hang.
const ipxRequestTimeout = 5 * time.Second

// ipxMaxResponse is the largest SMB response this transport can carry back in one IPX
// datagram over Ethernet. Direct-hosted SMB over IPX is connectionless with NO
// reassembly (one datagram = one whole SMB message, [MS-CIFS] §2.2.1.6.4), so a reply
// must fit a single Ethernet frame: 1500-byte payload − 30-byte IPX header leaves ~1470
// for the SMB message. 1400 is a conservative cap that keeps the whole frame under the
// MTU with headroom for the Ethernet/LLC encapsulation, so the server never packs a
// TRANS2/READ reply too big to transmit (the classic DOS/WfW redirectors cap likewise).
// The session bounds TRANS2 MaxDataCount and READ/WRITE sizes by this so a directory
// listing pages through FIND_NEXT2 instead of overflowing one datagram.
const ipxMaxResponse = 1400

// ipxTransport is the direct-hosted-SMB-over-IPX client transport. It owns the pcap
// FrameLink, runs a read loop demultiplexing inbound IPX/SMB datagrams to the pending
// Send, and applies the learned server node + CID to each outbound message.
type ipxTransport struct {
	fl     link.FrameLink
	srcMAC [6]byte
	srcNet [4]byte // client IPX network (0 = unknown; the server replies to our node regardless)

	mu         sync.Mutex
	serverNode [6]byte // learned from the first response (broadcast → real node)
	serverNet  [4]byte
	haveServer bool
	cid        uint16 // server-assigned Connection ID, echoed on later messages

	// Pending-request correlation. This connectionless transport carries no per-request
	// demux of its own, so the read loop must match an inbound response to the request
	// currently in flight before delivering it — otherwise a reordered or duplicated
	// datagram (e.g. a NEGOTIATE reply arriving while SESSION_SETUP is outstanding)
	// satisfies the wrong Send. The session layer above serialises Sends, so at most one
	// request is in flight; waitCmd/waitMID name it (waiting is true while a Send waits).
	waiting bool
	waitCmd uint8
	waitMID uint16

	respCh chan []byte
	stop   chan struct{}
	closed bool
}

// RandomMAC generates a locally-administered, unicast MAC address for the client's
// virtual IPX station. The client is a distinct station ON the segment the pcap device
// bridges, NOT the host itself, so it must present its own node address rather than
// borrow the host NIC's MAC — otherwise it collides with the host's own networking
// identity and two client instances on one NIC clash. The first octet has the
// locally-administered bit set (bit 1) and the group bit clear (bit 0), the IEEE
// convention for a synthetic unicast address; the remaining 5 octets are random.
func RandomMAC() [6]byte {
	var mac [6]byte
	_, _ = rand.Read(mac[:])
	mac[0] = (mac[0] | 0x02) &^ 0x01 // locally-administered, unicast
	return mac
}

// DialIPX builds a direct-hosted-SMB-over-IPX transport over the pcap FrameLink fl.
// srcMAC is this virtual station's hardware address (the IPX source node): pass
// RandomMAC() for a synthetic station (the default) or a user-specified MAC to pin the
// address. The first request is broadcast and the server node is learned from its reply.
// The caller has opened fl with an "ipx" BPF filter.
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

// Send transmits one SMB message as an IPX datagram and returns the matching response.
// The destination is the learned server node (broadcast on the first, pre-NEGOTIATE
// message); the CID the server assigned is stamped into the request header so the
// server correlates the circuit.
func (t *ipxTransport) Send(req []byte) ([]byte, error) {
	t.mu.Lock()
	dstNode := broadcastNode
	dstNet := t.srcNet
	if t.haveServer {
		dstNode = t.serverNode
		dstNet = t.serverNet
	}
	cid := t.cid
	closed := t.closed
	t.mu.Unlock()
	if closed {
		return nil, ErrTransportClosed
	}

	// Stamp the CID the server assigned (0 before NEGOTIATE completes) into the request
	// SMB header SecurityFeatures field, so the server maps it to the right circuit.
	msg := append([]byte(nil), req...)
	if cid != 0 && len(msg) >= smbCIDOffset+2 {
		msg[smbCIDOffset] = byte(cid)
		msg[smbCIDOffset+1] = byte(cid >> 8)
	}

	// Register this request as the one in flight so the read loop delivers only its
	// matching response (same command byte and MID). Drain any stale response left in
	// respCh from a prior timed-out Send first, so we never return a bygone reply.
	if len(msg) < 32 {
		return nil, fmt.Errorf("smb/ipx: request shorter than an SMB header")
	}
	reqCmd := msg[smbCommandOffset]
	reqMID := uint16(msg[smbMIDOffset]) | uint16(msg[smbMIDOffset+1])<<8
	t.mu.Lock()
	for {
		select {
		case <-t.respCh:
			continue // discard a stale queued response from a previous Send
		default:
		}
		break
	}
	t.waiting = true
	t.waitCmd = reqCmd
	t.waitMID = reqMID
	t.mu.Unlock()
	defer func() {
		t.mu.Lock()
		t.waiting = false
		t.mu.Unlock()
	}()

	d := &ipxproto.Datagram{
		Type:    ipxPEPType,
		DstNet:  dstNet,
		DstNode: dstNode,
		DstSock: directSMBSocket,
		SrcNet:  t.srcNet,
		SrcNode: t.srcMAC,
		SrcSock: directSMBSocket,
		Payload: msg,
	}
	if err := t.writeDatagram(d, dstNode); err != nil {
		return nil, err
	}

	select {
	case resp := <-t.respCh:
		return resp, nil
	case <-time.After(ipxRequestTimeout):
		return nil, fmt.Errorf("smb/ipx: no response within %s", ipxRequestTimeout)
	case <-t.stop:
		return nil, ErrTransportClosed
	}
}

// MaxResponse is the datagram-safe reply cap (one IPX datagram over Ethernet, no
// reassembly), used by the session to bound TRANS2 MaxDataCount and READ/WRITE sizes.
func (t *ipxTransport) MaxResponse() int { return ipxMaxResponse }

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

// readLoop reads frames, strips the Ethernet/IPX encapsulation, and delivers SMB
// RESPONSE datagrams addressed to our node+socket to the pending Send. It learns the
// server node + CID from the first response.
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
		if err != nil || d.Type != ipxPEPType {
			continue
		}
		msg := d.Payload
		if len(msg) < 32 || string(msg[:4]) != "\xffSMB" {
			continue
		}
		// Only SMB RESPONSES (reply bit set) on our socket are ours; ignore requests
		// (another client's) and our own echoes.
		if len(msg) <= smbFlagsOffset || msg[smbFlagsOffset]&smbReplyFlag == 0 {
			continue
		}
		if d.DstSock != directSMBSocket || d.DstNode != t.srcMAC {
			continue
		}

		respCmd := msg[smbCommandOffset]
		respMID := uint16(msg[smbMIDOffset]) | uint16(msg[smbMIDOffset+1])<<8

		t.mu.Lock()
		// Correlate against the request in flight: a response whose command byte and MID
		// do not match the pending Send is a reordered/duplicated datagram (e.g. a late
		// NEGOTIATE reply arriving during SESSION_SETUP) and must not satisfy it.
		if !t.waiting || respCmd != t.waitCmd || respMID != t.waitMID {
			t.mu.Unlock()
			continue
		}
		if !t.haveServer {
			t.serverNode = d.SrcNode
			t.serverNet = d.SrcNet
			t.haveServer = true
		}
		// Learn/refresh the CID the server stamped, so later requests echo it.
		if len(msg) >= smbCIDOffset+2 {
			if c := uint16(msg[smbCIDOffset]) | uint16(msg[smbCIDOffset+1])<<8; c != 0 && c != 0xFFFF {
				t.cid = c
			}
		}
		t.mu.Unlock()

		select {
		case t.respCh <- append([]byte(nil), msg...):
		case <-t.stop:
			return
		default:
			// No pending Send (a duplicate/late response): drop it.
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

// errIPXNoMAC is returned when the client cannot resolve its own NIC MAC (needed as the
// IPX source node).
var errIPXNoMAC = errors.New("smb/ipx: cannot resolve source MAC for the pcap device")
