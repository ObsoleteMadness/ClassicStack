package netbeui

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
	nbf "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbeui"
)

// fakeFrameLink is an in-test link.FrameLink (queued reads, captured writes).
type fakeFrameLink struct {
	mu     sync.Mutex
	inbox  [][]byte
	sent   [][]byte
	closed bool
}

func (f *fakeFrameLink) push(frame []byte) {
	f.mu.Lock()
	f.inbox = append(f.inbox, frame)
	f.mu.Unlock()
}

func (f *fakeFrameLink) Read() (link.Frame, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil, link.ErrClosed
	}
	if len(f.inbox) > 0 {
		frame := f.inbox[0]
		f.inbox = f.inbox[1:]
		return frame, nil
	}
	return nil, link.ErrTimeout
}

func (f *fakeFrameLink) Write(frame link.Frame) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return link.ErrClosed
	}
	cp := make([]byte, len(frame))
	copy(cp, frame)
	f.sent = append(f.sent, cp)
	return nil
}

func (f *fakeFrameLink) Close() error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	return nil
}

func (f *fakeFrameLink) sentCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

func enabledModel(t *testing.T) *config.Model {
	t.Helper()
	m := config.NewModel()
	m.Set(&port.Section{SKey: Name, Iface: "eth0", IsEnabled: true})
	return m
}

func newTestLogger() log.Logger {
	return log.New(Name, log.NewStderrSink(log.NewLevelVar(log.Warn)))
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for range 1000 {
		if cond() {
			return
		}
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}
}

// uiFrame wraps an NBF body in an 802.3 + LLC UI (0xF0F003) Ethernet frame.
func uiFrame(dst, src [6]byte, nbfBody []byte) []byte {
	payloadLen := 3 + len(nbfBody)
	frame := make([]byte, 0, ethHdrLen+payloadLen)
	frame = append(frame, dst[:]...)
	frame = append(frame, src[:]...)
	frame = append(frame, byte(payloadLen>>8), byte(payloadLen))
	frame = append(frame, 0xF0, 0xF0, 0x03)
	frame = append(frame, nbfBody...)
	return frame
}

func sampleNBF() *nbf.Frame {
	f := &nbf.Frame{Command: nbf.CmdAddNameQuery, RspCorrelator: 0x0002}
	copy(f.SourceName[:], "CLASSICSTACK   \x00")
	return f
}

func TestDisabledReturnsNil(t *testing.T) {
	m := config.NewModel()
	m.Set(&port.Section{SKey: Name, Iface: "eth0", IsEnabled: false})
	c, err := New(m, nil, [6]byte{}, newTestLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c != nil {
		t.Fatalf("disabled section must yield nil, got %T", c)
	}
}

func TestInboundUIFrameDelivered(t *testing.T) {
	fl := &fakeFrameLink{}
	body, _ := sampleNBF().Encode()
	dst := nbf.NetBIOSMulticastMAC
	src := [6]byte{0x00, 0x50, 0x56, 0xc0, 0x00, 0x01}
	fl.push(uiFrame(dst, src, body))

	c, _ := New(enabledModel(t), fl, [6]byte{}, newTestLogger())
	var gotSrc, gotDst [6]byte
	var gotFrame *nbf.Frame
	var mu sync.Mutex
	c.(*Port).SetDeliveryCallback(func(s, d [6]byte, f *nbf.Frame) {
		mu.Lock()
		gotSrc, gotDst, gotFrame = s, d, f
		mu.Unlock()
	})
	c.Start(context.Background())
	defer c.Stop(context.Background())

	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return gotFrame != nil })
	mu.Lock()
	defer mu.Unlock()
	if gotFrame == nil {
		t.Fatal("no frame delivered")
	}
	if gotFrame.Command != nbf.CmdAddNameQuery {
		t.Errorf("command = %#x, want AddNameQuery", gotFrame.Command)
	}
	if gotSrc != src || gotDst != dst {
		t.Errorf("MACs = src % x dst % x, want src % x dst % x", gotSrc, gotDst, src, dst)
	}
}

func TestNonNetBIOSFrameSkipped(t *testing.T) {
	fl := &fakeFrameLink{}
	// LLC with the wrong DSAP (0xAA, i.e. SNAP, not NetBIOS 0xF0): must be skipped.
	dst := [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	src := [6]byte{1, 2, 3, 4, 5, 6}
	frame := make([]byte, 0)
	frame = append(frame, dst[:]...)
	frame = append(frame, src[:]...)
	frame = append(frame, 0x00, 0x10)
	frame = append(frame, 0xAA, 0xAA, 0x03) // SNAP, not NetBIOS
	frame = append(frame, make([]byte, 13)...)
	fl.push(frame)

	c, _ := New(enabledModel(t), fl, [6]byte{}, newTestLogger())
	var n int
	var mu sync.Mutex
	c.(*Port).SetDeliveryCallback(func([6]byte, [6]byte, *nbf.Frame) { mu.Lock(); n++; mu.Unlock() })
	c.Start(context.Background())
	defer c.Stop(context.Background())

	waitFor(t, func() bool { return c.(component.Statful).Stats().Counters["frames_rx"] >= 1 })
	mu.Lock()
	defer mu.Unlock()
	if n != 0 {
		t.Fatalf("delivered %d frames, want 0 (non-NetBIOS LLC skipped)", n)
	}
}

func TestSendUIEncapsulation(t *testing.T) {
	fl := &fakeFrameLink{}
	src := [6]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x01}
	c, _ := New(enabledModel(t), fl, src, newTestLogger())
	c.Start(context.Background())
	defer c.Stop(context.Background())

	if err := c.(*Port).SendBroadcast(sampleNBF()); err != nil {
		t.Fatalf("SendBroadcast: %v", err)
	}
	if fl.sentCount() != 1 {
		t.Fatalf("sent %d frames, want 1", fl.sentCount())
	}
	fl.mu.Lock()
	frame := fl.sent[0]
	fl.mu.Unlock()
	if [6]byte(frame[0:6]) != nbf.NetBIOSMulticastMAC {
		t.Errorf("dst MAC = % x, want NetBIOS multicast", frame[0:6])
	}
	if frame[14] != 0xF0 || frame[15] != 0xF0 || frame[16] != 0x03 {
		t.Errorf("LLC header = % x, want f0 f0 03", frame[14:17])
	}
}

// llcUFrame builds a 3-byte-LLC U-frame (SABME/DISC) with the given control byte.
func llcUFrame(dst, src [6]byte, ctrl byte) []byte {
	frame := make([]byte, ethHdrLen+3)
	copy(frame[0:6], dst[:])
	copy(frame[6:12], src[:])
	frame[12], frame[13] = 0x00, 0x03
	frame[14], frame[15], frame[16] = 0xF0, 0xF0, ctrl
	return frame
}

// llcIFrame builds a 4-byte-LLC I-frame carrying nbfBody with the given N(S)/N(R)
// and P-bit, as a WfW/DOS client sends session data.
func llcIFrame(dst, src [6]byte, nS, nR byte, poll bool, nbfBody []byte) []byte {
	payloadLen := 4 + len(nbfBody)
	frame := make([]byte, ethHdrLen+payloadLen)
	copy(frame[0:6], dst[:])
	copy(frame[6:12], src[:])
	frame[12], frame[13] = byte(payloadLen>>8), byte(payloadLen)
	frame[14], frame[15] = 0xF0, 0xF0
	frame[16] = nS << 1
	frame[17] = nR << 1
	if poll {
		frame[17] |= 0x01
	}
	copy(frame[18:], nbfBody)
	return frame
}

// TestSABMEAnsweredWithUA reproduces the netbeui.pcap blocker: a DOS/WfW client
// sends SABME to open an LLC2 session after NAME_RECOGNIZED and the port must
// answer with UA (control 0x73, SSAP 0xF1). Previously the port dropped SABME
// and the client's SMB session never came up.
func TestSABMEAnsweredWithUA(t *testing.T) {
	fl := &fakeFrameLink{}
	ourMAC := [6]byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE}
	client := [6]byte{0x00, 0x00, 0xD8, 0x2C, 0x14, 0xFC}
	fl.push(llcUFrame(ourMAC, client, 0x7F)) // SABME, P=1

	c, _ := New(enabledModel(t), fl, ourMAC, newTestLogger())
	c.Start(context.Background())
	defer c.Stop(context.Background())

	waitFor(t, func() bool { return fl.sentCount() >= 1 })
	fl.mu.Lock()
	defer fl.mu.Unlock()
	if len(fl.sent) == 0 {
		t.Fatal("no UA sent in response to SABME")
	}
	ua := fl.sent[0]
	if [6]byte(ua[0:6]) != client {
		t.Errorf("UA dst = % x, want client % x", ua[0:6], client)
	}
	if ua[14] != 0xF0 || ua[15] != 0xF1 || ua[16] != 0x73 {
		t.Errorf("UA LLC = % x, want f0 f1 73 (DSAP/SSAP-resp/UA-F)", ua[14:17])
	}
}

// TestSABMEToForeignMACIgnored: SABME not addressed to us produces no reply.
func TestSABMEToForeignMACIgnored(t *testing.T) {
	fl := &fakeFrameLink{}
	ourMAC := [6]byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE}
	other := [6]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66}
	client := [6]byte{0x00, 0x00, 0xD8, 0x2C, 0x14, 0xFC}
	fl.push(llcUFrame(other, client, 0x7F))

	c, _ := New(enabledModel(t), fl, ourMAC, newTestLogger())
	c.Start(context.Background())
	defer c.Stop(context.Background())

	waitFor(t, func() bool { return c.(component.Statful).Stats().Counters["frames_rx"] >= 1 })
	if fl.sentCount() != 0 {
		t.Fatalf("sent %d frames for foreign-MAC SABME, want 0", fl.sentCount())
	}
}

// TestInboundIFrameDeliveredAndAcked: after a connection is up (SABME→UA), a
// session-command I-frame with the P-bit set is decoded, delivered, and RR-acked.
func TestInboundIFrameDeliveredAndAcked(t *testing.T) {
	fl := &fakeFrameLink{}
	ourMAC := [6]byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE}
	client := [6]byte{0x00, 0x00, 0xD8, 0x2C, 0x14, 0xFC}

	sess := &nbf.Frame{Command: nbf.CmdSessionInitialize, DestNumber: 1, SourceNumber: 0x15}
	body, _ := sess.Encode()

	fl.push(llcUFrame(ourMAC, client, 0x7F))             // SABME
	fl.push(llcIFrame(ourMAC, client, 0, 0, true, body)) // I-frame N(S)=0, P=1

	c, _ := New(enabledModel(t), fl, ourMAC, newTestLogger())
	var got *nbf.Frame
	var mu sync.Mutex
	c.(*Port).SetDeliveryCallback(func(_, _ [6]byte, f *nbf.Frame) {
		mu.Lock()
		got = f
		mu.Unlock()
	})
	c.Start(context.Background())
	defer c.Stop(context.Background())

	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return got != nil })
	mu.Lock()
	gotCmd := uint8(0)
	if got != nil {
		gotCmd = got.Command
	}
	mu.Unlock()
	if gotCmd != nbf.CmdSessionInitialize {
		t.Fatalf("delivered command = %#x, want SESSION_INITIALIZE", gotCmd)
	}

	// Expect UA (for SABME) then RR (for the polled I-frame, N(R)=1).
	waitFor(t, func() bool { return fl.sentCount() >= 2 })
	fl.mu.Lock()
	defer fl.mu.Unlock()
	rr := fl.sent[len(fl.sent)-1]
	if rr[14] != 0xF0 || rr[15] != 0xF1 || rr[16] != 0x01 {
		t.Errorf("RR LLC = % x, want f0 f1 01 (RR S-frame)", rr[14:17])
	}
	if rr[17] != (1<<1)|0x01 { // N(R)=1, F=1
		t.Errorf("RR ctrl1 = %#x, want %#x (N(R)=1, F=1)", rr[17], (1<<1)|0x01)
	}
}

// TestSessionCommandSentAsIFrame: once a connection exists, Send of a session
// command uses I-framing (4-byte LLC, command SSAP) rather than UI.
func TestSessionCommandSentAsIFrame(t *testing.T) {
	fl := &fakeFrameLink{}
	ourMAC := [6]byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE}
	client := [6]byte{0x00, 0x00, 0xD8, 0x2C, 0x14, 0xFC}
	fl.push(llcUFrame(ourMAC, client, 0x7F)) // SABME → establishes conn, sends UA

	c, _ := New(enabledModel(t), fl, ourMAC, newTestLogger())
	c.Start(context.Background())
	defer c.Stop(context.Background())
	waitFor(t, func() bool { return fl.sentCount() >= 1 }) // UA sent, conn exists

	confirm := &nbf.Frame{Command: nbf.CmdSessionConfirm, DestNumber: 0x15, SourceNumber: 1}
	if err := c.(*Port).Send(client, confirm); err != nil {
		t.Fatalf("Send: %v", err)
	}
	waitFor(t, func() bool { return fl.sentCount() >= 2 })
	fl.mu.Lock()
	defer fl.mu.Unlock()
	iframe := fl.sent[len(fl.sent)-1]
	if iframe[15] != 0xF0 { // SSAP command
		t.Errorf("I-frame SSAP = %#x, want 0xF0 (command)", iframe[15])
	}
	if iframe[16]&0x01 != 0 { // I-frame: low bit of ctrl0 == 0
		t.Errorf("ctrl0 = %#x, not an I-frame", iframe[16])
	}
}

// llcSFrame builds a 4-byte-LLC supervisory frame (RR/RNR/REJ). ssap selects
// command (0xF0) or response (0xF1); pf sets the P/F bit alongside N(R).
func llcSFrame(dst, src [6]byte, ssap, ctrl0, nR byte, pf bool) []byte {
	frame := make([]byte, ethHdrLen+4)
	copy(frame[0:6], dst[:])
	copy(frame[6:12], src[:])
	frame[12], frame[13] = 0x00, 0x04
	frame[14], frame[15] = 0xF0, ssap
	frame[16] = ctrl0
	frame[17] = nR << 1
	if pf {
		frame[17] |= 0x01
	}
	return frame
}

// sentIFrames returns the captured outbound I-frames' ctrl0 bytes (N(S)<<1),
// in send order, skipping U- and S-frames.
func (f *fakeFrameLink) sentIFrames() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []byte
	for _, fr := range f.sent {
		if len(fr) >= 18 && fr[14] == 0xF0 && fr[16]&0x01 == 0 {
			out = append(out, fr[16])
		}
	}
	return out
}

// establishAndSendTwo brings up an LLC2 connection (SABME→UA) and has the port
// send two session-command I-frames (N(S)=0 and 1), returning the port.
func establishAndSendTwo(t *testing.T, fl *fakeFrameLink, ourMAC, client [6]byte) *Port {
	t.Helper()
	fl.push(llcUFrame(ourMAC, client, 0x7F)) // SABME
	c, _ := New(enabledModel(t), fl, ourMAC, newTestLogger())
	c.Start(context.Background())
	t.Cleanup(func() { c.Stop(context.Background()) })
	waitFor(t, func() bool { return fl.sentCount() >= 1 }) // UA — conn exists

	p := c.(*Port)
	ack := &nbf.Frame{Command: nbf.CmdDataAck, DestNumber: 0x15, SourceNumber: 1}
	dol := &nbf.Frame{Command: nbf.CmdDataOnlyLast, DestNumber: 0x15, SourceNumber: 1, Payload: []byte("\xffSMBresp")}
	if err := p.Send(client, ack); err != nil {
		t.Fatalf("Send ack: %v", err)
	}
	if err := p.Send(client, dol); err != nil {
		t.Fatalf("Send dol: %v", err)
	}
	return p
}

// TestCheckpointPollRetransmitsUnacked reproduces the NT 3.51 netbeui.pcap
// failure: the client's NIC dropped our second back-to-back I-frame (N(S)=1),
// so the client checkpoint-polled with RR P N(R)=1 — and the port only echoed
// RR, never retransmitting, so the SMB session hung until the client gave up.
// The poll must now trigger retransmission of the outstanding I-frame.
func TestCheckpointPollRetransmitsUnacked(t *testing.T) {
	fl := &fakeFrameLink{}
	ourMAC := [6]byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE}
	client := [6]byte{0x00, 0x00, 0xD8, 0x50, 0xAE, 0xD3}
	establishAndSendTwo(t, fl, ourMAC, client)

	// Client's recovery checkpoint: RR command, P=1, N(R)=1 (got 0, missed 1).
	before := fl.sentCount()
	fl.push(llcSFrame(ourMAC, client, 0xF0, 0x01, 1, true))
	waitFor(t, func() bool { return fl.sentCount() >= before+2 })

	fl.mu.Lock()
	tail := fl.sent[before:]
	fl.mu.Unlock()
	var gotRR, gotRetransmit bool
	for _, fr := range tail {
		if fr[15] == 0xF1 && fr[16] == 0x01 { // RR response to the poll
			gotRR = true
		}
		if fr[16]&0x01 == 0 && fr[16]>>1 == 1 { // I-frame N(S)=1 again
			gotRetransmit = true
		}
		if fr[16]&0x01 == 0 && fr[16]>>1 == 0 {
			t.Error("retransmitted acknowledged I-frame N(S)=0")
		}
	}
	if !gotRR {
		t.Error("no RR F=1 response to the checkpoint poll")
	}
	if !gotRetransmit {
		t.Error("checkpoint poll with N(R)=1 did not retransmit I-frame N(S)=1")
	}
}

// TestAckedIFramesNotRetransmitted: once the peer's N(R) has acknowledged
// everything, a later checkpoint poll yields only an RR response.
func TestAckedIFramesNotRetransmitted(t *testing.T) {
	fl := &fakeFrameLink{}
	ourMAC := [6]byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE}
	client := [6]byte{0x00, 0x00, 0xD8, 0x50, 0xAE, 0xD3}
	establishAndSendTwo(t, fl, ourMAC, client)

	// Delayed ack of both frames (RR response, F=0, N(R)=2)…
	fl.push(llcSFrame(ourMAC, client, 0xF1, 0x01, 2, false))
	// …then a checkpoint poll at the acked level.
	fl.push(llcSFrame(ourMAC, client, 0xF0, 0x01, 2, true))

	waitFor(t, func() bool {
		fl.mu.Lock()
		defer fl.mu.Unlock()
		for _, fr := range fl.sent {
			if fr[15] == 0xF1 && fr[16] == 0x01 { // RR response went out
				return true
			}
		}
		return false
	})
	if got := fl.sentIFrames(); len(got) != 2 {
		t.Fatalf("I-frames on the wire = %d (ctrl0 % x), want 2 (no retransmits after full ack)", len(got), got)
	}
}

// TestREJRetransmitsFromNR: a REJ S-frame is an immediate retransmit request.
func TestREJRetransmitsFromNR(t *testing.T) {
	fl := &fakeFrameLink{}
	ourMAC := [6]byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE}
	client := [6]byte{0x00, 0x00, 0xD8, 0x50, 0xAE, 0xD3}
	establishAndSendTwo(t, fl, ourMAC, client)

	fl.push(llcSFrame(ourMAC, client, 0xF1, 0x09, 0, false)) // REJ N(R)=0: resend both
	waitFor(t, func() bool { return len(fl.sentIFrames()) >= 4 })
	got := fl.sentIFrames()
	if len(got) < 4 || got[len(got)-2]>>1 != 0 || got[len(got)-1]>>1 != 1 {
		t.Fatalf("I-frame ctrl0 sequence % x, want retransmission of N(S)=0 then N(S)=1", got)
	}
}

// TestT1PollsAndRecovers: with no acknowledgment at all, the T1 reply timer
// must checkpoint-poll (RR command, P=1), and the peer's RR F=1 response
// reporting a stale N(R) must trigger retransmission.
func TestT1PollsAndRecovers(t *testing.T) {
	saved := llcT1
	llcT1 = 25 * time.Millisecond
	defer func() { llcT1 = saved }()

	fl := &fakeFrameLink{}
	ourMAC := [6]byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE}
	client := [6]byte{0x00, 0x00, 0xD8, 0x50, 0xAE, 0xD3}
	establishAndSendTwo(t, fl, ourMAC, client)

	// T1 must fire and poll: RR command (SSAP 0xF0), P=1.
	waitFor(t, func() bool {
		fl.mu.Lock()
		defer fl.mu.Unlock()
		for _, fr := range fl.sent {
			if len(fr) >= 18 && fr[15] == 0xF0 && fr[16] == 0x01 && fr[17]&0x01 != 0 {
				return true
			}
		}
		return false
	})

	// Peer answers the poll: RR response F=1, N(R)=0 — it missed everything.
	fl.push(llcSFrame(ourMAC, client, 0xF1, 0x01, 0, true))
	waitFor(t, func() bool { return len(fl.sentIFrames()) >= 4 })
	if got := fl.sentIFrames(); len(got) < 4 {
		t.Fatalf("I-frames on the wire = %d (ctrl0 % x), want both retransmitted after RR F", len(got), got)
	}
}

// TestT1GivesUpAfterN2: llcN2 unanswered polls drop the dead connection.
func TestT1GivesUpAfterN2(t *testing.T) {
	saved := llcT1
	llcT1 = 5 * time.Millisecond
	defer func() { llcT1 = saved }()

	fl := &fakeFrameLink{}
	ourMAC := [6]byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE}
	client := [6]byte{0x00, 0x00, 0xD8, 0x50, 0xAE, 0xD3}
	p := establishAndSendTwo(t, fl, ourMAC, client)

	waitFor(t, func() bool { return p.lookupConn(client) == nil })
	if p.lookupConn(client) != nil {
		t.Fatal("connection not dropped after N2 unanswered T1 polls")
	}
}

func TestStopStartRestartable(t *testing.T) {
	c, _ := New(enabledModel(t), &fakeFrameLink{}, [6]byte{}, newTestLogger())
	ctx := context.Background()
	for i := range 2 {
		if err := c.Start(ctx); err != nil {
			t.Fatalf("Start #%d: %v", i, err)
		}
		if err := c.Stop(ctx); err != nil {
			t.Fatalf("Stop #%d: %v", i, err)
		}
	}
}

func TestReconfigureIfaceChangeNeedsRestart(t *testing.T) {
	c, _ := New(enabledModel(t), &fakeFrameLink{}, [6]byte{}, newTestLogger())
	cfg := c.(component.Configurable)
	if err := cfg.ApplyConfig(&port.Section{SKey: Name, Iface: "eth0", IsEnabled: false}); err != nil {
		t.Errorf("same-iface reconfigure should apply live, got %v", err)
	}
	if err := cfg.ApplyConfig(&port.Section{SKey: Name, Iface: "wlan0", IsEnabled: true}); !errors.Is(err, component.ErrNeedsRestart) {
		t.Errorf("iface change err = %v, want ErrNeedsRestart", err)
	}
}
