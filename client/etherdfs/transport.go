// Package etherdfs is the EtherDFS ("The Ethernet DOS File System", by Mateusz Viste)
// file client: it drives one DOS-redirector-style circuit to an EtherDFS server over
// raw Ethernet (EtherType 0xEDF5, no IP/IPX/DDP) through the client-direction codec
// (core/protocol/etherdfs) and presents a mounted drive as an fs.FileSystem — the same
// interface an AFP/SMB/NCP/local share exposes, so client/xfer and cmd/csfs drive a
// remote DOS drive identically. EtherDFS has no native resource fork, so client.Connect
// wraps this base with the AppleDouble fork backend, which reads/writes the server's own
// "._NAME" sidecars as ordinary 8.3 files.
//
// EtherDFS has no login or session handshake: the client learns the server's MAC by
// broadcasting an AL_DISKSPACE probe for the target drive (the reference client's
// discovery, sendquery()/updatermac) and then unicasts every request to it, correlating
// replies by the request sequence byte. A request names its drive by the frame's drive
// field and its path in DOS wire form.
//
// Ring: CLIENT.
package etherdfs

import (
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/etherdfs"
)

// ethHdrLen is the Ethernet II header length (dst6 + src6 + type2). EtherDFS frames
// carry the EtherType in that header and the EtherDFS header/body after it, but the
// codec (proto.Frame.Encode / proto.ParseFrame) operates on the WHOLE Ethernet frame
// (it reads/writes the MACs and EtherType itself), so this transport hands complete
// frames to the link.
const ethHdrLen = 14

// broadcastMAC is the Ethernet broadcast address the discovery probe targets.
var broadcastMAC = [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

// requestTimeout bounds how long a Send waits for the matching reply before giving up
// (a lost frame over the connectionless segment). A bounded wait avoids a hang.
const requestTimeout = 5 * time.Second

// maxPayload is the largest reply body one EtherDFS frame carries back. EtherDFS rides
// one Ethernet frame per request with no reassembly, so a READ reply must fit the MTU:
// 1500-byte payload − 60-byte EtherDFS header leaves ~1440. 1024 is a conservative cap
// (the reference client reads in 1024-byte chunks) that keeps the whole frame under the
// MTU; the session bounds READ/WRITE sizes by it.
const maxPayload = 1024

// Transport is one EtherDFS circuit as the client sees it: send a whole request frame
// (drive + opcode + body) and get the reply frame's AX status and body back, blocking
// until it arrives. The framing (Ethernet encapsulation, the learned server MAC, the
// sequence correlation) lives in the implementation; the session only sees a
// request→(status, body) exchange.
type Transport interface {
	// Send transmits one request (drive, opcode, body) and returns the reply's AX
	// status word and body. The client serialises requests per circuit (one in flight).
	Send(drive, opcode uint8, body []byte) (status uint16, reply []byte, err error)
	// MaxPayload is the largest reply body one frame can carry back (one Ethernet
	// frame, no reassembly), used to bound READ/WRITE sizes.
	MaxPayload() int
	// ServerMAC returns the learned server hardware address (zero until discovery), for
	// diagnostics.
	ServerMAC() [6]byte
	Close() error
}

// frameTransport is the raw-Ethernet EtherDFS client transport. It owns the pcap
// FrameLink, runs a read loop matching inbound reply frames to the pending Send by
// sequence, and applies the learned server MAC to each outbound request.
type frameTransport struct {
	fl     link.FrameLink
	srcMAC [6]byte

	mu         sync.Mutex
	serverMAC  [6]byte
	haveServer bool
	seq        uint8 // request sequence, bumped per Send and echoed by the reply

	waiting bool
	waitSeq uint8

	respCh chan proto.Frame
	stop   chan struct{}
	closed bool
}

// RandomMAC generates a locally-administered, unicast MAC for the client's virtual
// station. The client is a distinct station on the segment the pcap device bridges, NOT
// the host itself, so it presents its own address rather than borrow the host NIC's MAC
// (which would collide). The first octet has the locally-administered bit set and the
// group bit clear; the rest are random.
func RandomMAC() [6]byte {
	var mac [6]byte
	_, _ = rand.Read(mac[:])
	mac[0] = (mac[0] | 0x02) &^ 0x01 // locally-administered, unicast
	return mac
}

// DialFrame builds an EtherDFS transport over the pcap FrameLink fl. srcMAC is this
// virtual station's hardware address: pass RandomMAC() for a synthetic station (the
// default) or a user-specified MAC to pin it. The caller has opened fl with the
// "ether proto 0xedf5" BPF filter. The server MAC is learned from the first reply (the
// first request is broadcast).
func DialFrame(fl link.FrameLink, srcMAC [6]byte) Transport {
	t := &frameTransport{
		fl:     fl,
		srcMAC: srcMAC,
		respCh: make(chan proto.Frame, 4),
		stop:   make(chan struct{}),
	}
	go t.readLoop()
	return t
}

// Send transmits one EtherDFS request and returns the reply's status and body. The
// destination is the learned server MAC (broadcast on the first, pre-discovery request).
func (t *frameTransport) Send(drive, opcode uint8, body []byte) (uint16, []byte, error) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return 0, nil, ErrTransportClosed
	}
	dst := broadcastMAC
	if t.haveServer {
		dst = t.serverMAC
	}
	t.seq++
	seq := t.seq
	// Drain any stale reply from a prior timed-out Send, then register this sequence.
	for {
		select {
		case <-t.respCh:
			continue
		default:
		}
		break
	}
	t.waiting = true
	t.waitSeq = seq
	t.mu.Unlock()
	defer func() {
		t.mu.Lock()
		t.waiting = false
		t.mu.Unlock()
	}()

	req := proto.Frame{
		DstMAC:   dst,
		SrcMAC:   t.srcMAC,
		Sequence: seq,
		Drive:    drive,
		Opcode:   opcode,
		Payload:  body,
	}
	if err := t.fl.Write(req.Encode(nil)); err != nil {
		return 0, nil, err
	}

	select {
	case f := <-t.respCh:
		return f.Status, f.Payload, nil
	case <-time.After(requestTimeout):
		return 0, nil, fmt.Errorf("etherdfs: no reply within %s", requestTimeout)
	case <-t.stop:
		return 0, nil, ErrTransportClosed
	}
}

// MaxPayload is the datagram-safe reply-body cap (one Ethernet frame, no reassembly).
func (t *frameTransport) MaxPayload() int { return maxPayload }

// ServerMAC returns the learned server address (zero until the first reply).
func (t *frameTransport) ServerMAC() [6]byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.serverMAC
}

// readLoop reads frames, decodes EtherDFS reply frames addressed to our station, and
// delivers the one matching the pending Send's sequence. It learns the server MAC from
// the first matching reply.
func (t *frameTransport) readLoop() {
	for {
		frame, err := t.fl.Read()
		if err != nil {
			if errors.Is(err, link.ErrTimeout) {
				select {
				case <-t.stop:
					return
				default:
					continue
				}
			}
			return // terminal (ErrClosed or other)
		}
		f, perr := proto.ParseFrame(frame)
		if perr != nil {
			continue
		}
		// Accept only frames addressed to our station (or broadcast) whose source is not
		// ourselves (ignore our own echoed requests on a hub/loopback).
		if f.DstMAC != t.srcMAC && f.DstMAC != broadcastMAC {
			continue
		}
		if f.SrcMAC == t.srcMAC {
			continue
		}

		t.mu.Lock()
		if !t.waiting || f.Sequence != t.waitSeq {
			t.mu.Unlock()
			continue
		}
		if !t.haveServer {
			t.serverMAC = f.SrcMAC
			t.haveServer = true
			edfstracef("learned server MAC %s from first reply", macTraceEDFS(f.SrcMAC))
		}
		t.mu.Unlock()

		// Mark the parsed frame as a reply so f.Status/f.Payload are the meaningful
		// fields (ParseFrame fills Drive/Opcode from the same offset; for a reply those
		// bytes are the AX status, which we recompute here from the raw frame).
		select {
		case t.respCh <- replyView(f, frame):
		case <-t.stop:
			return
		default:
		}
	}
}

// replyView reinterprets a parsed frame as a reply: ParseFrame reads header offset
// 58-59 as Drive+Opcode, but on a reply those two bytes are the little-endian AX status
// word. This reads that word from the raw frame and returns a Frame with Status set and
// IsReply true, so the session reads f.Status/f.Payload.
func replyView(f proto.Frame, raw []byte) proto.Frame {
	f.IsReply = true
	// AX status is at Ethernet-frame offset 58-59 (little-endian): the same offset
	// ParseFrame read Drive/Opcode from. Recover it from the raw frame.
	if len(raw) >= 60 {
		f.Status = uint16(raw[58]) | uint16(raw[59])<<8
	}
	return f
}

// Close stops the read loop and closes the link.
func (t *frameTransport) Close() error {
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

// ErrTransportClosed is returned when a Send races a Close.
var ErrTransportClosed = errors.New("etherdfs: transport closed")
