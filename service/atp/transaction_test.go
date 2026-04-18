package atp

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ----- fakeClock ----------------------------------------------------------

type fakeClock struct {
	mu    sync.Mutex
	now   time.Time
	next  int
	tasks map[int]*fakeTimer
}

type fakeTimer struct {
	id       int
	deadline time.Time
	fn       func()
	clock    *fakeClock
	stopped  bool
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Unix(0, 0), tasks: make(map[int]*fakeTimer)}
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }

func (c *fakeClock) AfterFunc(d time.Duration, f func()) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next++
	t := &fakeTimer{id: c.next, deadline: c.now.Add(d), fn: f, clock: c}
	c.tasks[t.id] = t
	return t
}

func (t *fakeTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	if _, ok := t.clock.tasks[t.id]; ok {
		delete(t.clock.tasks, t.id)
		t.stopped = true
		return true
	}
	return false
}

// Advance moves time forward by d, firing all due callbacks (in deadline
// order) before returning. Callbacks added during firing whose deadline is
// still in the future are NOT fired in this call.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
	for {
		c.mu.Lock()
		var due []*fakeTimer
		for _, t := range c.tasks {
			if !t.deadline.After(c.now) {
				due = append(due, t)
			}
		}
		sort.Slice(due, func(i, j int) bool { return due[i].deadline.Before(due[j].deadline) })
		for _, t := range due {
			delete(c.tasks, t.id)
		}
		c.mu.Unlock()
		if len(due) == 0 {
			return
		}
		for _, t := range due {
			t.fn()
		}
	}
}

func (c *fakeClock) PendingCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.tasks)
}

// ----- fakeSender ---------------------------------------------------------

type sentPacket struct {
	src    Address
	dst    Address
	header ATPHeader
	data   []byte
	hint   any
}

type fakeSender struct {
	mu      sync.Mutex
	packets []sentPacket
}

func (s *fakeSender) Send(src, dst Address, payload []byte, hint any) error {
	var h ATPHeader
	if err := h.Unmarshal(payload); err != nil {
		return err
	}
	var data []byte
	if len(payload) > ATPHeaderSize {
		data = append([]byte(nil), payload[ATPHeaderSize:]...)
	}
	s.mu.Lock()
	s.packets = append(s.packets, sentPacket{src: src, dst: dst, header: h, data: data, hint: hint})
	s.mu.Unlock()
	return nil
}

func (s *fakeSender) Drain() []sentPacket {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.packets
	s.packets = nil
	return out
}

func (s *fakeSender) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.packets)
}

// ----- helpers ------------------------------------------------------------

var (
	addrRequester = Address{Net: 1, Node: 2, Socket: 100}
	addrResponder = Address{Net: 1, Node: 3, Socket: 200}
)

func mkTRespPacket(tid uint16, seq uint8, eom, sts bool, userBytes uint32, data []byte) []byte {
	ctrl := uint8(TRESP)
	if eom {
		ctrl |= EOM
	}
	if sts {
		ctrl |= STS
	}
	h := ATPHeader{Control: ctrl, Bitmap: seq, TransID: tid, UserData: userBytes}
	out := make([]byte, ATPHeaderSize+len(data))
	copy(out, h.Marshal())
	copy(out[ATPHeaderSize:], data)
	return out
}

func mkTReqPacket(tid uint16, bitmap uint8, xo bool, trelTO TRelTimeout, userBytes uint32, data []byte) []byte {
	ctrl := uint8(TREQ)
	if xo {
		ctrl |= XO
		ctrl |= uint8(trelTO) & 0x07
	}
	h := ATPHeader{Control: ctrl, Bitmap: bitmap, TransID: tid, UserData: userBytes}
	out := make([]byte, ATPHeaderSize+len(data))
	copy(out, h.Marshal())
	copy(out[ATPHeaderSize:], data)
	return out
}

func mkTRelPacket(tid uint16) []byte {
	h := ATPHeader{Control: TREL, TransID: tid}
	return h.Marshal()
}

func newReqEndpoint(t *testing.T) (*Endpoint, *fakeSender, *fakeClock) {
	t.Helper()
	clk := newFakeClock()
	snd := &fakeSender{}
	e := NewEndpoint(addrRequester, snd, WithClock(clk))
	return e, snd, clk
}

// ----- Requester tests ----------------------------------------------------

func TestRequester_HappySinglePacketALO(t *testing.T) {
	e, snd, _ := newReqEndpoint(t)
	p, err := e.SendRequest(Request{
		Dst: addrResponder, NumBuffers: 1, Data: []byte("hi"),
		RetryTimeout: time.Second, MaxRetries: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	pkts := snd.Drain()
	if len(pkts) != 1 {
		t.Fatalf("want 1 packet sent, got %d", len(pkts))
	}
	if pkts[0].header.FuncCode() != FuncTReq || pkts[0].header.Bitmap != 0x01 {
		t.Fatalf("bad TReq: %+v", pkts[0].header)
	}
	tid := pkts[0].header.TransID
	e.HandleInbound(mkTRespPacket(tid, 0, true, false, 0xAA, []byte("ok")), addrResponder, addrRequester, nil)
	resp, err := p.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if resp.Count != 1 || string(resp.Buffers[0]) != "ok" || resp.UserBytes[0] != 0xAA {
		t.Fatalf("bad resp: %+v", resp)
	}
	if snd.Len() != 0 {
		t.Fatalf("unexpected extra packets: %v", snd.Drain())
	}
}

func TestRequester_MultiPacketInOrder(t *testing.T) {
	e, snd, clk := newReqEndpoint(t)
	p, _ := e.SendRequest(Request{
		Dst: addrResponder, NumBuffers: 6, RetryTimeout: time.Second, MaxRetries: 3,
	})
	pkts := snd.Drain()
	if pkts[0].header.Bitmap != 0x3F {
		t.Fatalf("want bitmap 0x3F, got 0x%02X", pkts[0].header.Bitmap)
	}
	tid := pkts[0].header.TransID
	for i := uint8(0); i < 6; i++ {
		eom := i == 5
		e.HandleInbound(mkTRespPacket(tid, i, eom, false, uint32(i), []byte{i}), addrResponder, addrRequester, nil)
	}
	resp, err := p.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if resp.Count != 6 {
		t.Fatalf("want 6 packets, got %d", resp.Count)
	}
	clk.Advance(10 * time.Second)
	if snd.Len() != 0 {
		t.Fatalf("unexpected retries after completion: %v", snd.Drain())
	}
}

func TestRequester_RetryReplaysCorrectBitmap(t *testing.T) {
	e, snd, clk := newReqEndpoint(t)
	p, _ := e.SendRequest(Request{
		Dst: addrResponder, NumBuffers: 6, RetryTimeout: time.Second, MaxRetries: 3,
	})
	pkts := snd.Drain()
	tid := pkts[0].header.TransID
	// Deliver all but seq=2.
	for _, i := range []uint8{0, 1, 3, 4, 5} {
		e.HandleInbound(mkTRespPacket(tid, i, i == 5, false, 0, []byte{i}), addrResponder, addrRequester, nil)
	}
	clk.Advance(time.Second)
	pkts = snd.Drain()
	if len(pkts) != 1 {
		t.Fatalf("want 1 retry, got %d", len(pkts))
	}
	if pkts[0].header.Bitmap != 0x04 {
		t.Fatalf("want retry bitmap 0x04, got 0x%02X", pkts[0].header.Bitmap)
	}
	// Now deliver missing seq 2.
	e.HandleInbound(mkTRespPacket(tid, 2, false, false, 0, []byte{2}), addrResponder, addrRequester, nil)
	if _, err := p.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRequester_OutOfOrderDelivery(t *testing.T) {
	e, snd, _ := newReqEndpoint(t)
	p, _ := e.SendRequest(Request{
		Dst: addrResponder, NumBuffers: 6, RetryTimeout: time.Second, MaxRetries: 3,
	})
	tid := snd.Drain()[0].header.TransID
	for _, i := range []uint8{5, 3, 0, 1, 4, 2} {
		e.HandleInbound(mkTRespPacket(tid, i, i == 5, false, 0, []byte{i}), addrResponder, addrRequester, nil)
	}
	resp, err := p.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if len(resp.Buffers[i]) != 1 || resp.Buffers[i][0] != byte(i) {
			t.Fatalf("seq %d: %v", i, resp.Buffers[i])
		}
	}
}

func TestRequester_EOMShortResponse(t *testing.T) {
	e, snd, _ := newReqEndpoint(t)
	p, _ := e.SendRequest(Request{
		Dst: addrResponder, NumBuffers: 6, RetryTimeout: time.Second, MaxRetries: 3,
	})
	tid := snd.Drain()[0].header.TransID
	e.HandleInbound(mkTRespPacket(tid, 0, false, false, 0, []byte("a")), addrResponder, addrRequester, nil)
	e.HandleInbound(mkTRespPacket(tid, 1, false, false, 0, []byte("b")), addrResponder, addrRequester, nil)
	e.HandleInbound(mkTRespPacket(tid, 2, true, false, 0, []byte("c")), addrResponder, addrRequester, nil)
	resp, err := p.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if resp.Count != 3 {
		t.Fatalf("want 3, got %d", resp.Count)
	}
}

func TestRequester_RetryExhaustion(t *testing.T) {
	e, snd, clk := newReqEndpoint(t)
	p, _ := e.SendRequest(Request{
		Dst: addrResponder, NumBuffers: 1, RetryTimeout: time.Second, MaxRetries: 2,
	})
	for i := 0; i < 3; i++ {
		clk.Advance(time.Second)
	}
	_, err := p.Wait(context.Background())
	if err != ErrTimeout {
		t.Fatalf("want timeout, got %v", err)
	}
	// Initial + 2 retries = 3 sends.
	if got := snd.Len(); got != 3 {
		t.Fatalf("want 3 sends, got %d", got)
	}
}

func TestRequester_InfiniteRetry(t *testing.T) {
	e, snd, clk := newReqEndpoint(t)
	p, _ := e.SendRequest(Request{
		Dst: addrResponder, NumBuffers: 1, RetryTimeout: time.Second, MaxRetries: InfiniteRetries,
	})
	for i := 0; i < 100; i++ {
		clk.Advance(time.Second)
	}
	if got := snd.Len(); got != 101 {
		t.Fatalf("want 101 sends, got %d", got)
	}
	tid := snd.Drain()[0].header.TransID
	e.HandleInbound(mkTRespPacket(tid, 0, true, false, 0, nil), addrResponder, addrRequester, nil)
	if _, err := p.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRequester_XOSendsTRel(t *testing.T) {
	e, snd, _ := newReqEndpoint(t)
	p, _ := e.SendRequest(Request{
		Dst: addrResponder, NumBuffers: 1, XO: true, TRelTO: TRel30s,
		RetryTimeout: time.Second, MaxRetries: 3,
	})
	pkts := snd.Drain()
	if pkts[0].header.Control&XO == 0 {
		t.Fatal("XO bit not set on TReq")
	}
	tid := pkts[0].header.TransID
	e.HandleInbound(mkTRespPacket(tid, 0, true, false, 0, nil), addrResponder, addrRequester, nil)
	if _, err := p.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	pkts = snd.Drain()
	if len(pkts) != 1 || pkts[0].header.FuncCode() != FuncTRel || pkts[0].header.TransID != tid {
		t.Fatalf("want TRel, got %+v", pkts)
	}
}

func TestRequester_STS(t *testing.T) {
	e, snd, clk := newReqEndpoint(t)
	p, _ := e.SendRequest(Request{
		Dst: addrResponder, NumBuffers: 4, RetryTimeout: time.Second, MaxRetries: 3,
	})
	tid := snd.Drain()[0].header.TransID
	// STS-bearing partial response: should provoke immediate retransmit.
	e.HandleInbound(mkTRespPacket(tid, 0, false, true, 0, []byte("a")), addrResponder, addrRequester, nil)
	pkts := snd.Drain()
	if len(pkts) != 1 || pkts[0].header.FuncCode() != FuncTReq {
		t.Fatalf("want STS-triggered TReq, got %+v", pkts)
	}
	if pkts[0].header.Bitmap != 0x0E {
		t.Fatalf("want bitmap 0x0E, got 0x%02X", pkts[0].header.Bitmap)
	}
	// Retry timer should have been reset; advancing just under retry timeout
	// must NOT produce another TReq.
	clk.Advance(900 * time.Millisecond)
	if snd.Len() != 0 {
		t.Fatalf("retry timer not reset by STS: %v", snd.Drain())
	}
	// Now finish the transaction.
	for _, i := range []uint8{1, 2, 3} {
		e.HandleInbound(mkTRespPacket(tid, i, i == 3, false, 0, nil), addrResponder, addrRequester, nil)
	}
	if _, err := p.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRequester_TIDWraparoundSkipsLive(t *testing.T) {
	e, snd, _ := newReqEndpoint(t)
	// Park a TCB at TID 0 by setting lastTID = 0xFFFF.
	e.SetLastTID(0xFFFF)
	p1, _ := e.SendRequest(Request{
		Dst: addrResponder, NumBuffers: 1, RetryTimeout: time.Second, MaxRetries: 3,
	})
	if got := snd.Drain()[0].header.TransID; got != 0 {
		t.Fatalf("want TID 0, got %d", got)
	}
	_ = p1
	// Force the next allocation to start from 0xFFFF again — generator must
	// skip TID 0 and pick 1.
	e.SetLastTID(0xFFFF)
	p2, _ := e.SendRequest(Request{
		Dst: addrResponder, NumBuffers: 1, RetryTimeout: time.Second, MaxRetries: 3,
	})
	if got := snd.Drain()[0].header.TransID; got != 1 {
		t.Fatalf("want TID 1 (skipped 0), got %d", got)
	}
	_ = p2
}

func TestRequester_Cancel(t *testing.T) {
	e, snd, clk := newReqEndpoint(t)
	p, _ := e.SendRequest(Request{
		Dst: addrResponder, NumBuffers: 1, RetryTimeout: time.Second, MaxRetries: InfiniteRetries,
	})
	snd.Drain()
	p.Cancel()
	clk.Advance(10 * time.Second)
	if snd.Len() != 0 {
		t.Fatalf("retries continued after cancel: %v", snd.Drain())
	}
	_, err := p.Wait(context.Background())
	if err != ErrCancelled {
		t.Fatalf("want ErrCancelled, got %v", err)
	}
}

// ----- Responder tests ----------------------------------------------------

func newRespEndpoint(t *testing.T, h RequestHandler) (*Endpoint, *fakeSender, *fakeClock) {
	t.Helper()
	clk := newFakeClock()
	snd := &fakeSender{}
	e := NewEndpoint(addrResponder, snd, WithClock(clk))
	e.Listen(h)
	return e, snd, clk
}

func TestResponder_SingleRequest(t *testing.T) {
	var calls int32
	e, snd, _ := newRespEndpoint(t, func(in IncomingRequest, reply Replier) {
		atomic.AddInt32(&calls, 1)
		reply(ResponseMessage{Buffers: [][]byte{[]byte("a"), []byte("b"), []byte("c")}})
	})
	e.HandleInbound(mkTReqPacket(42, 0xFF, false, 0, 0, nil), addrRequester, addrResponder, nil)
	pkts := snd.Drain()
	if len(pkts) != 3 {
		t.Fatalf("want 3 resp pkts, got %d", len(pkts))
	}
	for i, p := range pkts {
		if p.header.Bitmap != uint8(i) {
			t.Fatalf("seq %d wrong: %d", i, p.header.Bitmap)
		}
		if (p.header.Control&EOM != 0) != (i == 2) {
			t.Fatalf("EOM wrong at %d", i)
		}
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatal("handler not called once")
	}
}

func TestResponder_BitmapHonored(t *testing.T) {
	e, snd, _ := newRespEndpoint(t, func(in IncomingRequest, reply Replier) {
		reply(ResponseMessage{Buffers: [][]byte{[]byte("0"), []byte("1"), []byte("2")}})
	})
	e.HandleInbound(mkTReqPacket(7, 0x05, false, 0, 0, nil), addrRequester, addrResponder, nil)
	pkts := snd.Drain()
	if len(pkts) != 2 {
		t.Fatalf("want 2 pkts, got %d", len(pkts))
	}
	if pkts[0].header.Bitmap != 0 || pkts[1].header.Bitmap != 2 {
		t.Fatalf("wrong seqs: %d, %d", pkts[0].header.Bitmap, pkts[1].header.Bitmap)
	}
}

func TestResponder_XODuplicateFiltering(t *testing.T) {
	var calls int32
	e, snd, _ := newRespEndpoint(t, func(in IncomingRequest, reply Replier) {
		atomic.AddInt32(&calls, 1)
		reply(ResponseMessage{Buffers: [][]byte{[]byte("once"), []byte("twice")}})
	})
	pkt := mkTReqPacket(7, 0x03, true, TRel30s, 0, nil)
	e.HandleInbound(pkt, addrRequester, addrResponder, nil)
	first := snd.Drain()
	if len(first) != 2 {
		t.Fatalf("first send: want 2, got %d", len(first))
	}
	// Duplicate.
	e.HandleInbound(pkt, addrRequester, addrResponder, nil)
	second := snd.Drain()
	if len(second) != 2 {
		t.Fatalf("dup send: want 2 from cache, got %d", len(second))
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("handler should run once, ran %d", atomic.LoadInt32(&calls))
	}
}

func TestResponder_XODuplicateNewBitmap(t *testing.T) {
	e, snd, _ := newRespEndpoint(t, func(in IncomingRequest, reply Replier) {
		reply(ResponseMessage{Buffers: [][]byte{[]byte("0"), []byte("1"), []byte("2")}})
	})
	e.HandleInbound(mkTReqPacket(9, 0x07, true, TRel30s, 0, nil), addrRequester, addrResponder, nil)
	snd.Drain()
	// Duplicate asking only for seq 1.
	e.HandleInbound(mkTReqPacket(9, 0x02, true, TRel30s, 0, nil), addrRequester, addrResponder, nil)
	pkts := snd.Drain()
	if len(pkts) != 1 || pkts[0].header.Bitmap != 1 {
		t.Fatalf("want only seq 1 from cache, got %+v", pkts)
	}
}

func TestResponder_AdaptivePacingStartsFastAndBacksOffOnLoss(t *testing.T) {
	var slept []time.Duration
	sleepFn := func(d time.Duration) {
		slept = append(slept, d)
	}

	clk := newFakeClock()
	snd := &fakeSender{}
	e := NewEndpoint(addrResponder, snd, WithClock(clk), WithSleep(sleepFn))
	e.Listen(func(in IncomingRequest, reply Replier) {
		reply(ResponseMessage{Buffers: [][]byte{[]byte("0"), []byte("1"), []byte("2")}})
	})

	// First transaction should be sent with no pacing sleep.
	e.HandleInbound(mkTReqPacket(21, 0x07, true, TRel30s, 0, nil), addrRequester, addrResponder, nil)
	_ = snd.Drain()
	if len(slept) != 0 {
		t.Fatalf("initial XO response should not sleep, got %v", slept)
	}

	// Duplicate with full bitmap indicates loss; replay should apply pacing.
	e.HandleInbound(mkTReqPacket(21, 0x07, true, TRel30s, 0, nil), addrRequester, addrResponder, nil)
	_ = snd.Drain()
	if len(slept) == 0 {
		t.Fatal("expected pacing sleeps after duplicate-loss feedback")
	}
	for _, d := range slept {
		if d <= 0 {
			t.Fatalf("expected positive pacing delay, got %v", d)
		}
	}
}

func TestResponder_TRelDropsRspCB(t *testing.T) {
	var calls int32
	e, snd, _ := newRespEndpoint(t, func(in IncomingRequest, reply Replier) {
		atomic.AddInt32(&calls, 1)
		reply(ResponseMessage{Buffers: [][]byte{nil}})
	})
	pkt := mkTReqPacket(11, 0x01, true, TRel30s, 0, nil)
	e.HandleInbound(pkt, addrRequester, addrResponder, nil)
	snd.Drain()
	e.HandleInbound(mkTRelPacket(11), addrRequester, addrResponder, nil)
	e.HandleInbound(pkt, addrRequester, addrResponder, nil)
	if calls := atomic.LoadInt32(&calls); calls != 2 {
		t.Fatalf("want handler called twice (RspCB gone), got %d", calls)
	}
}

func TestResponder_ReleaseTimerExpiry(t *testing.T) {
	var calls int32
	e, _, clk := newRespEndpoint(t, func(in IncomingRequest, reply Replier) {
		atomic.AddInt32(&calls, 1)
		reply(ResponseMessage{Buffers: [][]byte{nil}})
	})
	pkt := mkTReqPacket(13, 0x01, true, TRel30s, 0, nil)
	e.HandleInbound(pkt, addrRequester, addrResponder, nil)
	clk.Advance(35 * time.Second)
	e.HandleInbound(pkt, addrRequester, addrResponder, nil)
	if c := atomic.LoadInt32(&calls); c != 2 {
		t.Fatalf("want 2 calls after release expiry, got %d", c)
	}
}

func TestResponder_TRelTimeoutIndicatorHonored(t *testing.T) {
	var calls int32
	e, _, clk := newRespEndpoint(t, func(in IncomingRequest, reply Replier) {
		atomic.AddInt32(&calls, 1)
		reply(ResponseMessage{Buffers: [][]byte{nil}})
	})
	pkt := mkTReqPacket(15, 0x01, true, TRel2m, 0, nil)
	e.HandleInbound(pkt, addrRequester, addrResponder, nil)
	// Just under 2 minutes — RspCB still alive (handler not re-invoked).
	clk.Advance(110 * time.Second)
	e.mu.Lock()
	live := len(e.rspcbs)
	e.mu.Unlock()
	if live != 1 {
		t.Fatalf("RspCB expired too early at 110s: live=%d", live)
	}
	// Past 2 minutes — release timer fires. Advance from t=110s by 20s; the
	// release timer was set at t=0 with deadline 120s, so it fires here.
	clk.Advance(20 * time.Second)
	pkt2 := mkTReqPacket(16, 0x01, true, TRel2m, 0, nil) // different TID so it's a new tx
	_ = pkt2
	e.HandleInbound(pkt, addrRequester, addrResponder, nil)
	if c := atomic.LoadInt32(&calls); c != 2 {
		t.Fatalf("RspCB should have expired by 130s; calls=%d", c)
	}
}

func TestResponder_ALONotCached(t *testing.T) {
	var calls int32
	e, _, _ := newRespEndpoint(t, func(in IncomingRequest, reply Replier) {
		atomic.AddInt32(&calls, 1)
		reply(ResponseMessage{Buffers: [][]byte{nil}})
	})
	pkt := mkTReqPacket(17, 0x01, false, 0, 0, nil)
	e.HandleInbound(pkt, addrRequester, addrResponder, nil)
	e.HandleInbound(pkt, addrRequester, addrResponder, nil)
	if c := atomic.LoadInt32(&calls); c != 2 {
		t.Fatalf("ALO must not cache; calls=%d", c)
	}
}

func TestResponder_AdmissibleSourceFilter(t *testing.T) {
	var calls int32
	clk := newFakeClock()
	snd := &fakeSender{}
	e := NewEndpoint(addrResponder, snd,
		WithClock(clk),
		WithAdmissibleSource(Address{Net: 1, Node: 2, Socket: 0}))
	e.Listen(func(in IncomingRequest, reply Replier) {
		atomic.AddInt32(&calls, 1)
		reply(ResponseMessage{Buffers: [][]byte{nil}})
	})
	e.HandleInbound(mkTReqPacket(1, 0x01, false, 0, 0, nil), Address{Net: 1, Node: 2, Socket: 99}, addrResponder, nil)
	if calls != 1 {
		t.Fatalf("want admitted, got calls=%d", calls)
	}
	e.HandleInbound(mkTReqPacket(2, 0x01, false, 0, 0, nil), Address{Net: 1, Node: 9, Socket: 99}, addrResponder, nil)
	if calls != 1 {
		t.Fatalf("want filtered, got calls=%d", calls)
	}
}

// ----- Loopback integration ----------------------------------------------

// loopbackSender routes outbound packets from one Endpoint to another.
type loopbackSender struct {
	to *Endpoint
	// drop, if non-nil, is consulted for each packet; returning true drops it.
	drop func(p []byte) bool
}

func (l *loopbackSender) Send(src, dst Address, payload []byte, hint any) error {
	if l.drop != nil && l.drop(payload) {
		return nil
	}
	l.to.HandleInbound(payload, src, dst, nil)
	return nil
}

func TestIntegration_XOWithDroppedPacket(t *testing.T) {
	clk := newFakeClock()

	var responder, requester *Endpoint
	respSnd := &loopbackSender{}
	reqSnd := &loopbackSender{}

	responder = NewEndpoint(addrResponder, respSnd, WithClock(clk))
	requester = NewEndpoint(addrRequester, reqSnd, WithClock(clk))
	respSnd.to = requester
	reqSnd.to = responder

	responder.Listen(func(in IncomingRequest, reply Replier) {
		reply(ResponseMessage{Buffers: [][]byte{
			[]byte("aa"), []byte("bb"), []byte("cc"), []byte("dd"),
		}})
	})

	// Drop seq=2 the first time we see it.
	dropped := false
	respSnd.drop = func(p []byte) bool {
		var h ATPHeader
		if err := h.Unmarshal(p); err != nil {
			return false
		}
		if h.FuncCode() == FuncTResp && h.Bitmap == 2 && !dropped {
			dropped = true
			return true
		}
		return false
	}

	p, err := requester.SendRequest(Request{
		Src: addrRequester, Dst: addrResponder, NumBuffers: 4, XO: true, TRelTO: TRel30s,
		RetryTimeout: 500 * time.Millisecond, MaxRetries: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Trigger retry.
	clk.Advance(500 * time.Millisecond)
	resp, err := p.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if resp.Count != 4 {
		t.Fatalf("want 4 packets, got %d", resp.Count)
	}
	for i, want := range []string{"aa", "bb", "cc", "dd"} {
		if string(resp.Buffers[i]) != want {
			t.Fatalf("seq %d: %q", i, string(resp.Buffers[i]))
		}
	}
	// Responder should have no RspCBs left after TRel.
	responder.mu.Lock()
	left := len(responder.rspcbs)
	responder.mu.Unlock()
	if left != 0 {
		t.Fatalf("responder has %d RspCBs left", left)
	}
}
