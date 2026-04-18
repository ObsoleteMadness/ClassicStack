// Package atp transaction engine.
//
// This file implements the requester (TCB) and responder (RqCB / RspCB)
// state machines for AppleTalk Transaction Protocol per Inside AppleTalk
// (2nd ed.), Chapter 9.
//
// The engine is decoupled from DDP and the router: callers feed inbound
// packets in via HandleInbound and supply a Sender for outbound traffic.
// A pluggable Clock makes the retry/release timers deterministic for tests.
package atp

import (
	"context"
	"errors"
	"fmt"
	"math/bits"
	"sync"
	"time"

	"github.com/pgodw/omnitalk/go/netlog"
)

// ----- Address / Sender / Clock -------------------------------------------

// Address is a fully-qualified AppleTalk socket address.
type Address struct {
	Net    uint16
	Node   uint8
	Socket uint8
}

// Sender abstracts DDP send for testability. The engine never opens DDP
// sockets directly; the host service wires Sender to its router.
//
// hint is an opaque per-call value: for responder-side sends (TResp during
// initial dispatch or XO duplicate replay) it carries whatever HandleInbound
// was passed, so the host service can reach back into the original inbound
// datagram (e.g. to call router.Reply). For requester-side sends (TReq, TRel)
// hint is nil and the host service must rely on src/dst alone.
type Sender interface {
	Send(src, dst Address, payload []byte, hint any) error
}

// SenderFunc adapts a function to Sender.
type SenderFunc func(src, dst Address, payload []byte, hint any) error

func (f SenderFunc) Send(src, dst Address, payload []byte, hint any) error {
	return f(src, dst, payload, hint)
}

// Timer is a stoppable one-shot timer. It mirrors the relevant subset of
// time.Timer so test clocks can implement it.
type Timer interface {
	Stop() bool
}

// Clock is the time source used by the engine.
type Clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) Timer
}

// RealClock uses the standard library time package.
type RealClock struct{}

func (RealClock) Now() time.Time                            { return time.Now() }
func (RealClock) AfterFunc(d time.Duration, f func()) Timer { return realTimer{time.AfterFunc(d, f)} }

type realTimer struct{ t *time.Timer }

func (r realTimer) Stop() bool { return r.t.Stop() }

// ----- Public types -------------------------------------------------------

// InfiniteRetries selects the spec's "retransmit until a response is
// obtained" mode for SendRequest.
const InfiniteRetries = -1

// Request describes an outbound transaction.
type Request struct {
	Src          Address // local address to use as the source on the wire
	Dst          Address
	UserBytes    uint32
	Data         []byte
	NumBuffers   int // number of TResp packets the caller has reserved (1..8)
	XO           bool
	TRelTO       TRelTimeout
	RetryTimeout time.Duration
	MaxRetries   int // -1 = infinite
}

// Response is the assembled result of a successful transaction.
type Response struct {
	Buffers   [][]byte // index = sequence number; nil if not received (only possible after EOM)
	UserBytes [MaxResponsePackets]uint32
	Count     int // number of packets actually delivered
}

// IncomingRequest is what the responder handler receives.
type IncomingRequest struct {
	Src       Address
	Local     Address // the destination address the requester sent the TReq to
	TID       uint16
	UserBytes uint32
	Data      []byte
	Bitmap    uint8
	XO        bool
	TRelTO    TRelTimeout
}

// ResponseMessage is what the responder handler returns.
type ResponseMessage struct {
	// Buffers is the response message split into 1..MaxResponsePackets
	// pieces, each ≤ MaxATPData bytes. The engine assigns sequence numbers
	// 0..len(Buffers)-1 and sets EOM on the last packet.
	Buffers [][]byte
	// UserBytes parallel to Buffers; missing entries are zero.
	UserBytes []uint32
}

// Replier delivers the response to a transaction. The handler must call it
// exactly once, either synchronously (before returning) or asynchronously
// from another goroutine. For XO transactions the engine caches the response
// in the RspCB so duplicate TReqs are answered from the cache.
type Replier func(ResponseMessage)

// RequestHandler is invoked by the engine for each new (non-duplicate) TReq.
// Asynchronous handlers (e.g. ASP's two-phase Write) capture reply and
// invoke it later from a different goroutine.
type RequestHandler func(req IncomingRequest, reply Replier)

// ----- Errors -------------------------------------------------------------

var (
	ErrInvalidNumBuffers = errors.New("atp: NumBuffers must be 1..8")
	ErrDataTooLarge      = errors.New("atp: ATP data exceeds 578 bytes")
	ErrTimeout           = errors.New("atp: transaction retries exhausted")
	ErrCancelled         = errors.New("atp: transaction cancelled")
	ErrTooManyResponse   = errors.New("atp: response message exceeds 8 packets")
)

// ----- Endpoint -----------------------------------------------------------

// Endpoint owns the per-local-socket TCB and RspCB tables for an ATP user.
type Endpoint struct {
	local   Address
	sender  Sender
	clock   Clock
	sleep   func(time.Duration)
	handler RequestHandler

	// admissibleSrc, when non-nil, restricts incoming TReqs by source address.
	// Zero fields within match anything (per spec "Opening a responding socket").
	admissibleSrc *Address

	mu      sync.Mutex
	tcbs    map[uint16]*tcb // keyed by TID — Endpoint is per local socket already
	rspcbs  map[rspKey]*rspcb
	lastTID uint16
	pacer   map[Address]*responsePacer
}

type responsePacer struct {
	interPacketDelay time.Duration
}

const (
	adaptivePacerMaxDelay      = 16 * time.Millisecond
	adaptivePacerLossStep      = 1 * time.Millisecond
	adaptivePacerLossBurstStep = 2 * time.Millisecond
	adaptivePacerRecoveryStep  = 250 * time.Microsecond
)

type rspKey struct {
	src Address
	tid uint16
}

// Option configures an Endpoint.
type Option func(*Endpoint)

// WithClock injects a custom clock (used by tests).
func WithClock(c Clock) Option { return func(e *Endpoint) { e.clock = c } }

// WithAdmissibleSource restricts inbound TReqs to a particular source.
// Fields set to zero match any value.
func WithAdmissibleSource(a Address) Option {
	return func(e *Endpoint) { e.admissibleSrc = &a }
}

// WithSleep injects a custom sleep function used by responder pacing.
// Intended for tests.
func WithSleep(f func(time.Duration)) Option { return func(e *Endpoint) { e.sleep = f } }

// NewEndpoint creates an ATP engine bound to local and using sender for output.
func NewEndpoint(local Address, sender Sender, opts ...Option) *Endpoint {
	e := &Endpoint{
		local:  local,
		sender: sender,
		clock:  RealClock{},
		sleep:  time.Sleep,
		tcbs:   make(map[uint16]*tcb),
		rspcbs: make(map[rspKey]*rspcb),
		pacer:  make(map[Address]*responsePacer),
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

// Listen registers a request handler. Pass nil to stop accepting requests.
func (e *Endpoint) Listen(h RequestHandler) {
	e.mu.Lock()
	e.handler = h
	e.mu.Unlock()
}

// ----- TCB (requester) ----------------------------------------------------

type tcb struct {
	src          Address // local source addr to use on TReq/TRel sends
	dst          Address
	tid          uint16
	xo           bool
	trelTO       TRelTimeout
	bitmap       uint8 // bits still outstanding
	expected     int   // number of buffers requested
	resp         Response
	header       []byte // cached request packet (header + data)
	retryTimeout time.Duration
	retriesLeft  int // -1 = infinite
	timer        Timer
	done         chan struct{}
	err          error
	once         sync.Once
}

// Pending is the handle returned to callers of SendRequest.
type Pending struct {
	e   *Endpoint
	tcb *tcb
}

// Wait blocks until the transaction completes or ctx is cancelled.
func (p *Pending) Wait(ctx context.Context) (Response, error) {
	if p == nil || p.tcb == nil {
		return Response{}, errors.New("atp: nil Pending")
	}
	select {
	case <-p.tcb.done:
		return p.tcb.resp, p.tcb.err
	case <-ctx.Done():
		return Response{}, ctx.Err()
	}
}

// Cancel releases the TCB without delivering a result. Implements the spec's
// optional "Releasing a TCB" call.
func (p *Pending) Cancel() {
	if p == nil || p.tcb == nil {
		return
	}
	p.e.cancelTCB(p.tcb, ErrCancelled)
}

// SendRequest issues a new transaction and returns a Pending handle.
func (e *Endpoint) SendRequest(req Request) (*Pending, error) {
	if req.NumBuffers < 1 || req.NumBuffers > MaxResponsePackets {
		return nil, ErrInvalidNumBuffers
	}
	if len(req.Data) > MaxATPData {
		return nil, ErrDataTooLarge
	}
	if req.RetryTimeout <= 0 {
		req.RetryTimeout = 2 * time.Second
	}
	if req.MaxRetries == 0 {
		req.MaxRetries = 8
	}

	t := &tcb{
		src:          req.Src,
		dst:          req.Dst,
		xo:           req.XO,
		trelTO:       req.TRelTO,
		expected:     req.NumBuffers,
		bitmap:       fullBitmap(req.NumBuffers),
		resp:         Response{Buffers: make([][]byte, req.NumBuffers)},
		retryTimeout: req.RetryTimeout,
		retriesLeft:  req.MaxRetries,
		done:         make(chan struct{}),
	}

	e.mu.Lock()
	t.tid = e.allocTIDLocked()
	e.tcbs[t.tid] = t
	e.mu.Unlock()

	// Build (and cache) the request packet for retransmissions.
	t.header = e.buildTReq(t, req.UserBytes, req.Data)

	// Initial send + arm timer.
	_ = e.sender.Send(t.src, t.dst, t.header, nil)
	e.armRetryTimerLocked(t)

	return &Pending{e: e, tcb: t}, nil
}

func (e *Endpoint) buildTReq(t *tcb, userBytes uint32, data []byte) []byte {
	ctrl := uint8(TREQ)
	if t.xo {
		ctrl |= XO
		ctrl |= uint8(t.trelTO) & 0x07
	}
	h := ATPHeader{Control: ctrl, Bitmap: t.bitmap, TransID: t.tid, UserData: userBytes}
	out := make([]byte, ATPHeaderSize+len(data))
	copy(out, h.Marshal())
	copy(out[ATPHeaderSize:], data)
	return out
}

// allocTIDLocked implements the spec's TID generation algorithm: scan live
// TCBs on this Endpoint to ensure uniqueness, advancing past any in-use TIDs.
func (e *Endpoint) allocTIDLocked() uint16 {
	start := e.lastTID
	tid := start
	for i := 0; i < 0x10000; i++ {
		tid = (tid + 1) & 0xFFFF
		if _, inUse := e.tcbs[tid]; !inUse {
			e.lastTID = tid
			return tid
		}
	}
	// All in use — return whatever we landed on; caller will likely fail.
	e.lastTID = tid
	return tid
}

// SetLastTID is exposed for tests that need to drive TID wraparound.
func (e *Endpoint) SetLastTID(v uint16) {
	e.mu.Lock()
	e.lastTID = v
	e.mu.Unlock()
}

func (e *Endpoint) armRetryTimerLocked(t *tcb) {
	t.timer = e.clock.AfterFunc(t.retryTimeout, func() { e.onRetry(t) })
}

func (e *Endpoint) onRetry(t *tcb) {
	e.mu.Lock()
	if _, ok := e.tcbs[t.tid]; !ok {
		e.mu.Unlock()
		return
	}
	if t.retriesLeft == 0 {
		e.mu.Unlock()
		e.cancelTCB(t, ErrTimeout)
		return
	}
	if t.retriesLeft > 0 {
		t.retriesLeft--
	}
	// Re-emit with current bitmap.
	e.refreshBitmapInHeader(t)
	pkt := append([]byte(nil), t.header...)
	e.armRetryTimerLocked(t)
	src := t.src
	dst := t.dst
	retriesLeft := t.retriesLeft
	bitmap := t.bitmap
	e.mu.Unlock()

	netlog.Debug("[ATP] retry TID=%d dst=%s bitmap=0x%02x retriesLeft=%d",
		t.tid, dst, bitmap, retriesLeft)
	_ = e.sender.Send(src, dst, pkt, nil)
}

func (e *Endpoint) refreshBitmapInHeader(t *tcb) {
	if len(t.header) >= 2 {
		t.header[1] = t.bitmap
	}
}

func (e *Endpoint) cancelTCB(t *tcb, err error) {
	e.mu.Lock()
	if _, ok := e.tcbs[t.tid]; !ok {
		e.mu.Unlock()
		return
	}
	delete(e.tcbs, t.tid)
	if t.timer != nil {
		t.timer.Stop()
	}
	e.mu.Unlock()
	t.once.Do(func() {
		t.err = err
		close(t.done)
	})
}

// ----- RspCB (responder) --------------------------------------------------

type rspcb struct {
	src        Address
	tid        uint16
	cached     []ResponsePacket // sequence-indexed cache for retransmission
	releaseTO  time.Duration
	releaseTmr Timer
	gotResp    bool
}

// ResponsePacket is the wire form of one cached TResp (header + data).
type ResponsePacket struct {
	Header []byte // 8-byte ATP header
	Data   []byte
}

// ----- HandleInbound ------------------------------------------------------

// HandleInbound feeds a raw ATP packet into the engine.
//
// src is the source address from the underlying DDP datagram (the requester
// for inbound TReq, the responder for inbound TResp).
//
// local is the destination address from the inbound datagram — i.e. the
// address the peer used to reach us. This is used as the source on outbound
// TResps so we reply from the same address the requester sent to.
//
// hint is opaque context that the engine threads through to Sender.Send for
// any outbound packets generated as a direct result of this inbound packet
// (initial TResp dispatch and XO duplicate replays). Host services can use
// it to retain a pointer to the original datagram + rxPort so the Sender
// implementation can call e.g. router.Reply.
func (e *Endpoint) HandleInbound(packet []byte, src, local Address, hint any) {
	var h ATPHeader
	if err := h.Unmarshal(packet); err != nil {
		return
	}
	var data []byte
	if len(packet) > ATPHeaderSize {
		data = packet[ATPHeaderSize:]
	}
	switch h.FuncCode() {
	case FuncTReq:
		e.handleTReq(h, data, src, local, hint)
	case FuncTResp:
		e.handleTResp(h, data, src)
	case FuncTRel:
		e.handleTRel(h, src)
	}
}

func (e *Endpoint) handleTResp(h ATPHeader, data []byte, src Address) {
	e.mu.Lock()
	t, ok := e.tcbs[h.TransID]
	if !ok || t.dst != src {
		e.mu.Unlock()
		netlog.Debug("[ATP] TResp tid=%d from %s: no matching TCB (dropped)", h.TransID, src)
		return
	}
	seq := h.Bitmap // sequence number for TResp
	if int(seq) >= MaxResponsePackets || int(seq) >= t.expected {
		e.mu.Unlock()
		return
	}
	bit := uint8(1) << seq
	expected := t.bitmap&bit != 0
	if expected {
		t.bitmap &^= bit
		buf := append([]byte(nil), data...)
		t.resp.Buffers[seq] = buf
		t.resp.UserBytes[seq] = h.UserData
		t.resp.Count++
	}
	if h.EOM() {
		// Clear all higher bits.
		for s := int(seq) + 1; s < MaxResponsePackets; s++ {
			t.bitmap &^= 1 << s
		}
	}
	sts := h.STS()
	complete := t.bitmap == 0

	if complete {
		// Stop timer, drop TCB, optionally TRel.
		if t.timer != nil {
			t.timer.Stop()
		}
		delete(e.tcbs, t.tid)
		xo := t.xo
		tsrc := t.src
		dst := t.dst
		tid := t.tid
		e.mu.Unlock()

		if xo {
			e.sendTRel(tsrc, dst, tid)
		}
		t.once.Do(func() { close(t.done) })
		return
	}

	if sts {
		// Immediately retransmit TReq with current bitmap and reset retry timer.
		e.refreshBitmapInHeader(t)
		pkt := append([]byte(nil), t.header...)
		if t.timer != nil {
			t.timer.Stop()
		}
		e.armRetryTimerLocked(t)
		tsrc := t.src
		dst := t.dst
		e.mu.Unlock()
		_ = e.sender.Send(tsrc, dst, pkt, nil)
		return
	}
	e.mu.Unlock()
}

func (e *Endpoint) sendTRel(src, dst Address, tid uint16) {
	h := ATPHeader{Control: TREL, TransID: tid}
	pkt := h.Marshal()
	_ = e.sender.Send(src, dst, pkt, nil)
}

// ----- Responder ----------------------------------------------------------

func (e *Endpoint) handleTReq(h ATPHeader, data []byte, src, local Address, hint any) {
	if !e.admissible(src) {
		return
	}

	xo := h.XO()

	if xo {
		// Duplicate? — replay cached response per the new bitmap, using the
		// *new* inbound's local/hint so the route is current.
		e.mu.Lock()
		if r, ok := e.rspcbs[rspKey{src: src, tid: h.TransID}]; ok && r.gotResp {
			missing := bits.OnesCount8(h.Bitmap)
			e.increaseResponderPacingLocked(src, missing)
			delay := e.currentResponderPacingLocked(src)
			netlog.Debug("[ATP] XO dup from %s tid=%d bitmap=0x%02x: client missing %d packet(s), replaying from cache",
				src, h.TransID, h.Bitmap, missing)
			if delay > 0 {
				netlog.Debug("[ATP] responder pacing dst=%s delay=%s", src, delay)
			}
			cached := r.cached
			// Restart release timer.
			if r.releaseTmr != nil {
				r.releaseTmr.Stop()
			}
			r.releaseTmr = e.clock.AfterFunc(r.releaseTO, func() { e.expireRspCB(r) })
			e.mu.Unlock()
			e.replayCachedFiltered(local, src, h.Bitmap, cached, hint)
			return
		}
		e.mu.Unlock()
	}

	e.mu.Lock()
	handler := e.handler
	e.mu.Unlock()
	if handler == nil {
		return
	}

	in := IncomingRequest{
		Src:       src,
		Local:     local,
		TID:       h.TransID,
		UserBytes: h.UserData,
		Data:      append([]byte(nil), data...),
		Bitmap:    h.Bitmap,
		XO:        xo,
		TRelTO:    h.GetTRelTimeout(),
	}

	var rcb *rspcb
	if xo {
		// Insert RspCB *before* invoking the handler so that a duplicate
		// arriving while the handler is still running is dropped (per spec).
		e.mu.Lock()
		e.relaxResponderPacingLocked(src)
		key := rspKey{src: src, tid: h.TransID}
		if _, exists := e.rspcbs[key]; exists {
			// Handler already running for this transaction; drop dup.
			netlog.Debug("[ATP] XO dup from %s tid=%d: handler running, dropped", src, h.TransID)
			e.mu.Unlock()
			return
		}
		rcb = &rspcb{src: src, tid: h.TransID, releaseTO: in.TRelTO.Duration()}
		rcb.releaseTmr = e.clock.AfterFunc(rcb.releaseTO, func() { e.expireRspCB(rcb) })
		e.rspcbs[key] = rcb
		e.mu.Unlock()
	}

	// Build a Replier closure. Once invoked, it formats the response, caches
	// it in the RspCB (XO only) and emits packets respecting the *original*
	// inbound bitmap. For async handlers, src/local/hint are captured here.
	var replied sync.Once
	tid := h.TransID
	bitmap := h.Bitmap
	reply := func(resp ResponseMessage) {
		replied.Do(func() {
			if len(resp.Buffers) > MaxResponsePackets {
				return
			}
			for _, b := range resp.Buffers {
				if len(b) > MaxATPData {
					return
				}
			}
			cached := buildResponsePackets(tid, resp)
			if xo && rcb != nil {
				e.mu.Lock()
				rcb.cached = cached
				rcb.gotResp = true
				if rcb.releaseTmr != nil {
					rcb.releaseTmr.Stop()
				}
				rcb.releaseTmr = e.clock.AfterFunc(rcb.releaseTO, func() { e.expireRspCB(rcb) })
				e.mu.Unlock()
			}
			e.replayCachedFiltered(local, src, bitmap, cached, hint)
		})
	}
	handler(in, reply)
}

// admissible reports whether src matches the admissible-source filter.
func (e *Endpoint) admissible(src Address) bool {
	if e.admissibleSrc == nil {
		return true
	}
	a := *e.admissibleSrc
	if a.Net != 0 && a.Net != src.Net {
		return false
	}
	if a.Node != 0 && a.Node != src.Node {
		return false
	}
	if a.Socket != 0 && a.Socket != src.Socket {
		return false
	}
	return true
}

// buildResponsePackets formats a ResponseMessage into wire-ready packets,
// assigning sequence numbers and setting EOM on the last packet.
func buildResponsePackets(tid uint16, resp ResponseMessage) []ResponsePacket {
	out := make([]ResponsePacket, len(resp.Buffers))
	last := len(resp.Buffers) - 1
	for i, data := range resp.Buffers {
		ctrl := uint8(TRESP)
		if i == last {
			ctrl |= EOM
		}
		var ub uint32
		if i < len(resp.UserBytes) {
			ub = resp.UserBytes[i]
		}
		h := ATPHeader{Control: ctrl, Bitmap: uint8(i), TransID: tid, UserData: ub}
		out[i] = ResponsePacket{
			Header: h.Marshal(),
			Data:   append([]byte(nil), data...),
		}
	}
	return out
}

// replayCachedFiltered emits cached TResp packets whose sequence number bit
// is set in bitmap. src is the address to emit *from* (the local address the
// requester sent its TReq to); dst is the requester. hint is forwarded.
func (e *Endpoint) replayCachedFiltered(src, dst Address, bitmap uint8, cached []ResponsePacket, hint any) {
	delay := e.currentResponderPacing(dst)
	first := true
	for i, p := range cached {
		if bitmap&(1<<uint(i)) == 0 {
			continue
		}
		if !first && delay > 0 {
			e.sleep(delay)
		}
		first = false
		pkt := make([]byte, len(p.Header)+len(p.Data))
		copy(pkt, p.Header)
		copy(pkt[len(p.Header):], p.Data)
		_ = e.sender.Send(src, dst, pkt, hint)
	}
}

func (e *Endpoint) currentResponderPacing(dst Address) time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.currentResponderPacingLocked(dst)
}

func (e *Endpoint) currentResponderPacingLocked(dst Address) time.Duration {
	if p, ok := e.pacer[dst]; ok {
		return p.interPacketDelay
	}
	return 0
}

func (e *Endpoint) ensureResponderPacerLocked(dst Address) *responsePacer {
	if p, ok := e.pacer[dst]; ok {
		return p
	}
	p := &responsePacer{}
	e.pacer[dst] = p
	return p
}

func (e *Endpoint) increaseResponderPacingLocked(dst Address, missing int) {
	if missing <= 0 {
		return
	}
	p := e.ensureResponderPacerLocked(dst)
	step := time.Duration(missing) * adaptivePacerLossStep
	if missing >= 3 {
		step += adaptivePacerLossBurstStep
	}
	p.interPacketDelay += step
	if p.interPacketDelay > adaptivePacerMaxDelay {
		p.interPacketDelay = adaptivePacerMaxDelay
	}
}

func (e *Endpoint) relaxResponderPacingLocked(dst Address) {
	p := e.ensureResponderPacerLocked(dst)
	if p.interPacketDelay <= adaptivePacerRecoveryStep {
		p.interPacketDelay = 0
		return
	}
	p.interPacketDelay -= adaptivePacerRecoveryStep
}

func (e *Endpoint) handleTRel(h ATPHeader, src Address) {
	e.mu.Lock()
	key := rspKey{src: src, tid: h.TransID}
	r, ok := e.rspcbs[key]
	if !ok {
		e.mu.Unlock()
		return
	}
	delete(e.rspcbs, key)
	if r.releaseTmr != nil {
		r.releaseTmr.Stop()
	}
	e.mu.Unlock()
}

func (e *Endpoint) expireRspCB(r *rspcb) {
	e.mu.Lock()
	defer e.mu.Unlock()
	key := rspKey{src: r.src, tid: r.tid}
	if cur, ok := e.rspcbs[key]; ok && cur == r {
		delete(e.rspcbs, key)
	}
}

// ----- helpers ------------------------------------------------------------

func fullBitmap(n int) uint8 {
	if n >= MaxResponsePackets {
		return 0xFF
	}
	return (1 << uint(n)) - 1
}

// String helpers used in error/log messages and tests.
func (a Address) String() string {
	return fmt.Sprintf("%d.%d:%d", a.Net, a.Node, a.Socket)
}
