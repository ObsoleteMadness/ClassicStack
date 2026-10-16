package nat

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/service"
)

// captureRouter records the datagrams OSNAT routes toward the Mac. Only Route
// is used by the NAT; the embedded interface leaves the rest unimplemented.
type captureRouter struct {
	service.Router
	mu   sync.Mutex
	segs []tcpSeg
}

// tcpSeg is a decoded TCP segment sent toward the Mac.
type tcpSeg struct {
	seq, ack uint32
	flags    byte
	data     []byte
}

func (r *captureRouter) Route(d ddp.Datagram, _ bool) error {
	pkt := d.Data
	ihl := int(pkt[0]&0xf) * 4
	tcp := pkt[ihl:]
	off := int(tcp[12]>>4) * 4
	r.mu.Lock()
	r.segs = append(r.segs, tcpSeg{
		seq:   binary.BigEndian.Uint32(tcp[4:8]),
		ack:   binary.BigEndian.Uint32(tcp[8:12]),
		flags: tcp[13],
		data:  append([]byte(nil), tcp[off:]...),
	})
	r.mu.Unlock()
	return nil
}

// waitSeg waits for the n-th segment (0-based) routed toward the Mac.
func (r *captureRouter) waitSeg(t *testing.T, n int) tcpSeg {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		if len(r.segs) > n {
			s := r.segs[n]
			r.mu.Unlock()
			return s
		}
		r.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("segment %d never sent", n)
	return tcpSeg{}
}

func (r *captureRouter) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.segs)
}

var (
	testClientIP = [4]byte{192, 168, 100, 2}
	testServerIP = [4]byte{10, 0, 0, 1}
)

const (
	testMacISN = 1000
	testOurISN = 5000
	testWindow = 1092
)

// newTestFlow returns an OSNAT with one established flow whose host side is
// one end of a pipe; the other end is returned as the "server".
func newTestFlow(t *testing.T) (*OSNAT, *captureRouter, *tcpFwdFlow, osFlowKey, net.Conn) {
	t.Helper()
	r := &captureRouter{}
	n := &OSNAT{router: r, socket: 72, ddpType: 22, tcpFlows: make(map[osFlowKey]*tcpFwdFlow), stop: make(chan struct{})}
	local, server := net.Pipe()
	t.Cleanup(func() { _ = server.Close(); _ = local.Close() })
	flow := &tcpFwdFlow{
		conn:     local,
		clientIP: testClientIP, serverIP: testServerIP,
		clientPort: 1025, serverPort: 70,
		ourISN:    testOurISN,
		ourSeq:    testOurISN + 1,
		macSeq:    testMacISN + 1,
		macAck:    testOurISN + 1,
		macWindow: testWindow,
		mss:       osNATMaxSegment,
		rto:       20 * time.Millisecond,
		expiry:    time.Now().Add(osNATTCPTimeout),
		windowAdv: make(chan struct{}, 1),
		done:      make(chan struct{}),
	}
	key := osFlowKey{6, testClientIP, 1025, testServerIP, 70}
	n.tcpFlows[key] = flow
	return n, r, flow, key, server
}

// macSegment builds an IP packet carrying a TCP segment from the Mac.
func macSegment(seq, ack uint32, flags byte, data []byte) []byte {
	hdr := make([]byte, 20+len(data))
	binary.BigEndian.PutUint16(hdr[0:2], 1025)
	binary.BigEndian.PutUint16(hdr[2:4], 70)
	binary.BigEndian.PutUint32(hdr[4:8], seq)
	binary.BigEndian.PutUint32(hdr[8:12], ack)
	hdr[12] = 0x50
	hdr[13] = flags
	binary.BigEndian.PutUint16(hdr[14:16], testWindow)
	copy(hdr[20:], data)
	return BuildIPv4Packet(testClientIP[:], testServerIP[:], 6, hdr)
}

// readServer collects what the NAT writes to the server side.
func readServer(server net.Conn) <-chan []byte {
	out := make(chan []byte, 1)
	go func() {
		b, _ := io.ReadAll(server)
		out <- b
	}()
	return out
}

func TestHandleTCP_RetransmitFromMacWrittenOnce(t *testing.T) {
	n, r, flow, _, server := newTestFlow(t)
	got := readServer(server)

	n.Forward(macSegment(testMacISN+1, testOurISN+1, 0x18, []byte("abc")), 0, 0)
	n.Forward(macSegment(testMacISN+1, testOurISN+1, 0x18, []byte("abc")), 0, 0)    // retransmit
	n.Forward(macSegment(testMacISN+1, testOurISN+1, 0x18, []byte("abcdef")), 0, 0) // overlap
	n.Forward(macSegment(testMacISN+20, testOurISN+1, 0x18, []byte("zzz")), 0, 0)   // out of order
	for i, want := range []uint32{testMacISN + 4, testMacISN + 4, testMacISN + 7, testMacISN + 7} {
		if s := r.waitSeg(t, i); s.ack != want || s.flags != 0x10 {
			t.Errorf("ACK %d: ack %d flags %#x, want ack %d", i, s.ack, s.flags, want)
		}
	}
	_ = flow.conn.Close()
	if b := <-got; !bytes.Equal(b, []byte("abcdef")) {
		t.Errorf("server got %q, want %q", b, "abcdef")
	}
}

func TestTCP_FastRetransmitOnDuplicateACKs(t *testing.T) {
	n, r, flow, key, server := newTestFlow(t)
	flow.rto = time.Hour // only the duplicate ACKs may trigger the resend
	go n.tcpServerReadLoop(key, flow)

	if _, err := server.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	first := r.waitSeg(t, 0)
	if first.seq != testOurISN+1 || string(first.data) != "hello" {
		t.Fatalf("first segment seq %d %q", first.seq, first.data)
	}
	for range osNATTCPDupAckThreshold {
		n.Forward(macSegment(testMacISN+1, testOurISN+1, 0x10, nil), 0, 0)
	}
	resend := r.waitSeg(t, 1)
	if resend.seq != first.seq || string(resend.data) != "hello" {
		t.Errorf("resend seq %d %q, want seq %d %q", resend.seq, resend.data, first.seq, "hello")
	}
	flow.closeConn()
}

func TestTCP_RetransmitTimeoutThenACK(t *testing.T) {
	n, r, flow, key, server := newTestFlow(t)
	go n.tcpServerReadLoop(key, flow)

	if _, err := server.Write([]byte("data")); err != nil {
		t.Fatal(err)
	}
	r.waitSeg(t, 0)
	resend := r.waitSeg(t, 1) // after the 20 ms RTO
	if resend.seq != testOurISN+1 || string(resend.data) != "data" {
		t.Errorf("resend seq %d %q", resend.seq, resend.data)
	}
	n.Forward(macSegment(testMacISN+1, testOurISN+5, 0x10, nil), 0, 0)
	flow.mu.Lock()
	if len(flow.unacked) != 0 || flow.rtxTimer != nil {
		t.Errorf("after ACK: %d bytes unacked, timer %v", len(flow.unacked), flow.rtxTimer != nil)
	}
	flow.mu.Unlock()
	before := r.count()
	time.Sleep(100 * time.Millisecond)
	if r.count() != before {
		t.Errorf("resent after everything was acknowledged")
	}
	flow.closeConn()
}

func TestTCP_FINResentAndMacFINAcked(t *testing.T) {
	n, r, flow, key, server := newTestFlow(t)
	exited := make(chan struct{})
	go func() { n.tcpServerReadLoop(key, flow); close(exited) }()

	_ = server.Close() // server EOF
	fin := r.waitSeg(t, 0)
	if fin.flags != 0x11 || fin.seq != testOurISN+1 {
		t.Fatalf("FIN flags %#x seq %d", fin.flags, fin.seq)
	}
	if again := r.waitSeg(t, 1); again.flags != 0x11 || again.seq != fin.seq {
		t.Errorf("FIN resend flags %#x seq %d", again.flags, again.seq)
	}
	// The Mac acknowledges our FIN and sends its own.
	n.Forward(macSegment(testMacISN+1, testOurISN+2, 0x11, nil), 0, 0)
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("flow not closed after both FINs")
	}
	r.mu.Lock()
	last := r.segs[len(r.segs)-1]
	r.mu.Unlock()
	if last.flags != 0x10 || last.ack != testMacISN+2 {
		t.Errorf("Mac's FIN answered with flags %#x ack %d, want ACK %d", last.flags, last.ack, testMacISN+2)
	}
	n.tcpMu.Lock()
	_, still := n.tcpFlows[key]
	n.tcpMu.Unlock()
	if still {
		t.Error("flow still in the table")
	}
}

func TestTCP_RetransmittedSYNGetsSYNACK(t *testing.T) {
	n, r, _, _, _ := newTestFlow(t)
	n.Forward(macSegment(testMacISN, 0, 0x02, nil), 0, 0)
	s := r.waitSeg(t, 0)
	if s.flags != 0x12 || s.seq != testOurISN || s.ack != testMacISN+1 {
		t.Errorf("SYN-ACK flags %#x seq %d ack %d", s.flags, s.seq, s.ack)
	}
}

func TestTCP_PartialACKResendsNextSegment(t *testing.T) {
	n, r, flow, key, server := newTestFlow(t)
	go n.tcpServerReadLoop(key, flow)

	// Two full segments fill the Mac's 1092-byte window; say both are lost.
	if _, err := server.Write(bytes.Repeat([]byte("x"), 2*osNATMaxSegment)); err != nil {
		t.Fatal(err)
	}
	r.waitSeg(t, 1)
	if s := r.waitSeg(t, 2); s.seq != testOurISN+1 { // timeout resend of the first
		t.Fatalf("timeout resend seq %d", s.seq)
	}
	// The Mac acknowledges the first segment only: the second must be resent
	// at once, not after another (now 1 s) timeout.
	acked := time.Now()
	n.Forward(macSegment(testMacISN+1, testOurISN+1+osNATMaxSegment, 0x10, nil), 0, 0)
	for i := 3; ; i++ {
		s := r.waitSeg(t, i)
		if s.seq == testOurISN+1+osNATMaxSegment {
			if d := time.Since(acked); d > 500*time.Millisecond {
				t.Errorf("second segment resent %v after the partial ACK", d)
			}
			break
		}
	}
	flow.closeConn()
}
