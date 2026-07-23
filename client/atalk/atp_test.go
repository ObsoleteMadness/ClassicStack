package atalk

import (
	"sync"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/atp"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
)

// atp_test.go regression-tests the ATP requester against the behaviours a real classic
// Mac exhibits (captures/vmac-to-vmac.pcapng, captures/ltoudp vmac1): a single-packet
// response with EOM CLEAR must still complete, and a multi-packet EOM response must
// reassemble in order. The transport is a scripted in-memory DatagramLink that answers a
// TReq with a canned TResp set.

// fakeLink is an in-memory DatagramLink: WriteDatagram invokes the installed responder
// (which may enqueue reply datagrams), and ReadDatagram returns them in order.
type fakeLink struct {
	mu       sync.Mutex
	inbox    []ddp.Datagram
	respond  func(req ddp.Datagram) []ddp.Datagram
	signal   chan struct{}
	closed   bool
	closedCh chan struct{}
}

func newFakeLink(respond func(ddp.Datagram) []ddp.Datagram) *fakeLink {
	return &fakeLink{respond: respond, signal: make(chan struct{}, 64), closedCh: make(chan struct{})}
}

func (l *fakeLink) WriteDatagram(d ddp.Datagram) error {
	replies := l.respond(d)
	l.mu.Lock()
	l.inbox = append(l.inbox, replies...)
	l.mu.Unlock()
	for range replies {
		select {
		case l.signal <- struct{}{}:
		default:
		}
	}
	return nil
}

func (l *fakeLink) ReadDatagram() (ddp.Datagram, error) {
	for {
		l.mu.Lock()
		if len(l.inbox) > 0 {
			d := l.inbox[0]
			l.inbox = l.inbox[1:]
			l.mu.Unlock()
			return d, nil
		}
		l.mu.Unlock()
		select {
		case <-l.signal:
		case <-l.closedCh:
			return ddp.Datagram{}, errClosedTest
		case <-time.After(50 * time.Millisecond):
			// Return a timeout so the endpoint read loop can poll for Close.
			return ddp.Datagram{}, errTimeoutTest
		}
	}
}

func (l *fakeLink) Close() error {
	l.mu.Lock()
	if !l.closed {
		l.closed = true
		close(l.closedCh)
	}
	l.mu.Unlock()
	return nil
}

// errClosedTest / errTimeoutTest mimic the link sentinels the endpoint read loop reacts
// to (a timeout is re-polled; anything else is terminal). We map the timeout to
// link.ErrTimeout via the endpoint by returning the package sentinel.
var (
	errClosedTest  = errStr("closed")
	errTimeoutTest = link.ErrTimeout
)

type errStr string

func (e errStr) Error() string { return string(e) }

// tRespDatagram builds a TResp datagram for transID/seq with the given eom flag and
// userData, addressed to dstSocket (the requester's bound reply socket).
func tRespDatagram(dstSocket uint8, transID uint16, seq uint8, eom bool, userData uint32, payload []byte) ddp.Datagram {
	control := uint8(atp.TRESP)
	if eom {
		control |= atp.EOM
	}
	h := atp.Header{Control: control, Bitmap: seq, TransID: transID, UserData: userData}
	frame := h.Encode(nil)
	frame = append(frame, payload...)
	return ddp.Datagram{
		DestSocket: dstSocket,
		SrcSocket:  200,
		DDPType:    atp.DDPType,
		Data:       frame,
	}
}

// TestRequestCompletesWithoutEOM is the regression for the connect-hang: a real System
// 7.x ASP responder answers a single-packet OpenSession/Command reply with the EOM bit
// CLEAR. The requester asked for one packet (maxResp=1); receiving seq 0 must complete
// the transaction even though EOM is not set — otherwise every session hung.
func TestRequestCompletesWithoutEOM(t *testing.T) {
	var reqTransID uint16
	var reqSocket uint8
	link := newFakeLink(func(req ddp.Datagram) []ddp.Datagram {
		h, err := atp.Decode(req.Data)
		if err != nil || h.FuncCode() != atp.FuncTReq {
			return nil
		}
		reqTransID = h.TransID
		reqSocket = req.SrcSocket
		// Single-packet reply, EOM CLEAR (the real-Mac behaviour), data in UserData.
		return []ddp.Datagram{tRespDatagram(reqSocket, h.TransID, 0, false, 0xfb2a0000, nil)}
	})
	ep := NewEndpoint(link, Addr{Network: 0, Node: 10})
	defer ep.Close()

	a := NewATP(ep)
	resp, err := a.Request(Addr{Network: 1, Node: 11, Socket: 252}, 0x04810100, nil, false, 1)
	if err != nil {
		t.Fatalf("Request should complete on a full bitmap without EOM, got: %v", err)
	}
	if resp.UserData != 0xfb2a0000 {
		t.Errorf("UserData = %#x, want 0xfb2a0000", resp.UserData)
	}
	if reqTransID == 0 || reqSocket == 0 {
		t.Errorf("responder never saw the TReq (transID=%d socket=%d)", reqTransID, reqSocket)
	}
}

// TestRequestReassemblesEOM checks a multi-packet response (the second packet carrying
// EOM) reassembles in sequence order.
func TestRequestReassemblesEOM(t *testing.T) {
	link := newFakeLink(func(req ddp.Datagram) []ddp.Datagram {
		h, err := atp.Decode(req.Data)
		if err != nil || h.FuncCode() != atp.FuncTReq {
			return nil
		}
		s := req.SrcSocket
		return []ddp.Datagram{
			tRespDatagram(s, h.TransID, 0, false, 0, []byte("AAAA")),
			tRespDatagram(s, h.TransID, 1, true, 0, []byte("BBBB")), // EOM on the last
		}
	})
	ep := NewEndpoint(link, Addr{Network: 0, Node: 10})
	defer ep.Close()

	a := NewATP(ep)
	resp, err := a.Request(Addr{Network: 1, Node: 11, Socket: 251}, 0, nil, false, 8)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if string(resp.Data) != "AAAABBBB" {
		t.Errorf("reassembled data = %q, want AAAABBBB", resp.Data)
	}
}
