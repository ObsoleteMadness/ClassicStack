package atalk

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/protocol/atp"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
)

// atp.go is the ATP REQUESTER — the workstation half of ATP the server ring lacks
// (core/service/afp/atp.go is the responder). It runs one transaction: send a TReq
// asking for up to 8 response packets, collect the TResp packets by sequence bit,
// reassemble on EOM, retry the whole request (with a shrunk bitmap re-requesting only
// the still-missing packets) on timeout, and release an exactly-once transaction with
// a TRel.

// ATP requester tuning (Inside AppleTalk: retry interval and count).
const (
	// atpRetryInterval is how long to wait for the response set before retransmitting.
	atpRetryInterval = 2 * time.Second
	// atpMaxRetries is the number of TReq retransmissions before giving up.
	atpMaxRetries = 5
	// atpBurstIdle is how long to wait after the last TResp before treating a
	// contiguous prefix as complete when EOM never arrives. System 7 often omits
	// EOM; asking for 8 slots (FPEnumerate / FPRead) would otherwise stall until
	// atpRetryInterval. Matches classicstack-web BurstIdleMs (400).
	atpBurstIdle = 400 * time.Millisecond
)

// ErrATPTimeout is returned when a transaction gets no complete response after all
// retries.
var ErrATPTimeout = errors.New("atalk: ATP transaction timed out")

// Response is the reassembled result of an ATP transaction: the 4-byte UserData from
// the response packets (all packets carry the same UserData — ASP echoes the
// function/session/seq, and the AFP result code rides here) and the concatenated data.
type Response struct {
	UserData uint32
	Data     []byte
}

// transactor allocates monotonic transaction ids for one endpoint.
type transactor struct {
	mu   sync.Mutex
	next uint16
}

func (t *transactor) id() uint16 {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.next++
	if t.next == 0 {
		t.next = 1
	}
	return t.next
}

// ATP holds the requester state bound to an Endpoint.
type ATP struct {
	ep *Endpoint
	tx transactor

	// retryInterval and maxRetries override the default retry policy when non-zero.
	// ASP leaves them zero (the aggressive atpRetryInterval/atpMaxRetries defaults suit a
	// session); a one-shot probe like csgetzones sets them via SetRetryPolicy so a -timeout
	// bounds the whole transaction instead of waiting 6×2s to fail against a dead segment.
	retryInterval time.Duration
	maxRetries    int
}

// NewATP builds an ATP requester over ep.
func NewATP(ep *Endpoint) *ATP { return &ATP{ep: ep} }

// SetRetryPolicy overrides the per-attempt wait and retry count for this requester. A
// zero interval or negative count restores the default (atpRetryInterval/atpMaxRetries).
// A probe tool uses this to honour a short -timeout; session callers (ASP) leave the
// defaults, which tolerate a lossy segment across a whole AFP session.
func (a *ATP) SetRetryPolicy(interval time.Duration, retries int) {
	a.retryInterval = interval
	a.maxRetries = retries
}

// policy returns the effective retry interval and count, falling back to the package
// defaults when this requester has not overridden them.
func (a *ATP) policy() (time.Duration, int) {
	interval, retries := a.retryInterval, a.maxRetries
	if interval <= 0 {
		interval = atpRetryInterval
	}
	if retries <= 0 {
		retries = atpMaxRetries
	}
	return interval, retries
}

// Request runs one ATP transaction to dst and returns the reassembled response.
//   - userData is the 4-byte ATP UserData (the ASP function/session/seq).
//   - reqData is the TReq payload (the ASP command block); it must fit one DDP
//     datagram (ASP command blocks are ≤ the ATP data max — the server enforces this).
//   - xo requests exactly-once delivery (an ASP Command/Write is XO; GetStatus is ALO):
//     an XO transaction is released with a TRel after the response is received.
//   - maxResp bounds how many response packets the requester will accept (1..8); pass
//     atp.MaxResponsePackets for a full ASP quantum.
//
// It retries the whole transaction atpMaxRetries times on timeout, re-requesting only
// the packets still missing so a single dropped packet does not resend the whole set.
func (a *ATP) Request(dst Addr, userData uint32, reqData []byte, xo bool, maxResp int) (Response, error) {
	if maxResp < 1 {
		maxResp = 1
	}
	if maxResp > atp.MaxResponsePackets {
		maxResp = atp.MaxResponsePackets
	}

	srcSocket, ch := a.ep.Bind()
	defer a.ep.Unbind(srcSocket)

	transID := a.tx.id()
	fullMask := uint8((1 << uint(maxResp)) - 1)

	received := make([][]byte, atp.MaxResponsePackets)
	gotPacket := make([]bool, atp.MaxResponsePackets) // presence, tracked separately from
	// the payload so a zero-length reply packet (nil payload) still counts as received.
	var eomSeq = -1 // sequence number of the EOM packet, once seen
	var respUserData uint32

	// contiguousLen returns how many packets have arrived contiguously from seq 0.
	contiguousLen := func() int {
		n := 0
		for n < len(gotPacket) && gotPacket[n] {
			n++
		}
		return n
	}

	// haveAll reports whether the response is complete. It is complete either when the
	// EOM packet and everything before it has arrived, OR when every packet the request
	// bitmap asked for has arrived — a responder that fills the whole requested set need
	// not set EOM. ERRATA: a real System 7.x ASP responder answers a single-packet
	// OpenSession/Command reply (a full 1-packet request) WITHOUT setting EOM
	// (captures/ltoudp vmac1 2026-07-23: control 0x80, EOM clear); requiring EOM here
	// deadlocked the requester so no session to a real Mac ever opened. Completing on a
	// full requested bitmap fixes it while the EOM path still handles a responder that
	// ends a multi-packet message short. See spec/errata.md.
	haveAll := func() bool {
		if eomSeq >= 0 {
			for i := 0; i <= eomSeq; i++ {
				if !gotPacket[i] {
					return false
				}
			}
			return true
		}
		return contiguousLen() >= maxResp
	}
	// missingMask returns the bitmap of packets still needed. Before EOM is known it
	// re-requests the full set; after, only the gaps up to EOM.
	missingMask := func() uint8 {
		if eomSeq < 0 {
			var m uint8
			for i := 0; i < maxResp; i++ {
				if !gotPacket[i] {
					m |= 1 << uint(i)
				}
			}
			if m == 0 {
				m = fullMask
			}
			return m
		}
		var m uint8
		for i := 0; i <= eomSeq; i++ {
			if !gotPacket[i] {
				m |= 1 << uint(i)
			}
		}
		return m
	}

	retryInterval, maxRetries := a.policy()
	atpf("ATP request → %s transID=%d userData=0x%08x reqLen=%d xo=%t maxResp=%d srcSock=%d",
		dst, transID, userData, len(reqData), xo, maxResp, srcSocket)

	var idle *time.Timer
	stopIdle := func() {
		if idle == nil {
			return
		}
		if !idle.Stop() {
			select {
			case <-idle.C:
			default:
			}
		}
	}
	defer stopIdle()
	idleC := func() <-chan time.Time {
		if idle == nil {
			return nil
		}
		return idle.C
	}
	armIdle := func() {
		if idle == nil {
			idle = time.NewTimer(atpBurstIdle)
			return
		}
		stopIdle()
		idle.Reset(atpBurstIdle)
	}

	for attempt := 0; attempt <= maxRetries; attempt++ {
		mask := fullMask
		if attempt > 0 {
			mask = missingMask()
			drain(ch)
		}
		atpf("ATP TReq → %s transID=%d attempt=%d/%d bitmap=0x%02x",
			dst, transID, attempt+1, maxRetries+1, mask)
		if err := a.sendTReq(dst, srcSocket, transID, mask, userData, reqData, xo); err != nil {
			return Response{}, err
		}

		deadline := deadlineTimer(retryInterval)
		stopIdle()
	collect:
		for {
			select {
			case d, ok := <-ch:
				if !ok {
					return Response{}, errors.New("atalk: endpoint closed")
				}
				resp, ok := decodeTResp(d, transID)
				if !ok {
					continue
				}
				// The transaction's UserData (the ASP command result / AFP result code) is
				// authoritative from the FIRST response packet (seq 0) only. ERRATA: a real
				// System 7.x ASP responder fills seq 1..N's UserData with STALE bytes from a
				// prior transaction (observed on a live LToUDP FPRead reply from System 7.5.3
				// Personal File Sharing: UserData 0x00000000 in seq 0, 0x07270011 in seq 1-7 —
				// the leftover ASPWriteContinue user bytes of an earlier write session). Taking
				// the LAST packet's UserData clobbered the real result with garbage, surfacing
				// as bogus FPRead result codes like kFP#0x0727xxxx on any read over one
				// ATP-response quantum (~4 KB). Keep seq 0's value. See spec/errata.md.
				if resp.seq == 0 {
					respUserData = resp.userData
				}
				if int(resp.seq) < len(received) {
					if !gotPacket[resp.seq] {
						// Store a non-nil slice even for an empty payload, so a
						// zero-length reply packet (e.g. an OpenSession reply, whose
						// data lives in the UserData) counts as received. Keying
						// presence off a nil payload would loop forever on it.
						if resp.payload == nil {
							received[resp.seq] = []byte{}
						} else {
							received[resp.seq] = resp.payload
						}
						gotPacket[resp.seq] = true
					}
					if resp.eom {
						eomSeq = int(resp.seq)
					}
					atpf("ATP TResp ← transID=%d seq=%d eom=%t userData=0x%08x len=%d",
						transID, resp.seq, resp.eom, resp.userData, len(resp.payload))
				}
				if haveAll() {
					break collect
				}
				// Short reply, EOM omitted, bitmap not yet full: finish after a quiet
				// burst instead of waiting out the 2s retry (classicstack-web idle-complete).
				armIdle()
			case <-idleC():
				if haveAll() {
					break collect
				}
				if n := contiguousLen(); n > 0 && eomSeq < 0 {
					atpf("ATP idle-complete transID=%d slots=0..%d of %d (no EOM)",
						transID, n-1, maxResp)
					eomSeq = n - 1
					break collect
				}
			case <-deadline:
				atpf("ATP timeout transID=%d attempt=%d/%d (no complete response in %s)",
					transID, attempt+1, maxRetries+1, retryInterval)
				break collect // retry
			}
		}

		if haveAll() {
			// Reassemble in order up to the EOM, or — when the response completed on a
			// full requested bitmap without EOM (real-Mac single-packet reply) — up to the
			// contiguous run received.
			last := eomSeq
			if last < 0 {
				last = contiguousLen() - 1
			}
			var data []byte
			for i := 0; i <= last; i++ {
				data = append(data, received[i]...)
			}
			if xo {
				a.sendTRel(dst, srcSocket, transID, userData)
			}
			return Response{UserData: respUserData, Data: data}, nil
		}
	}
	return Response{}, fmt.Errorf("%w after %d attempts", ErrATPTimeout, maxRetries+1)
}

// sendTReq builds and sends a TReq. xo sets the exactly-once bit and a TRel timeout.
func (a *ATP) sendTReq(dst Addr, srcSocket uint8, transID uint16, bitmap uint8, userData uint32, reqData []byte, xo bool) error {
	h := atp.Header{
		Control:  atp.TREQ,
		Bitmap:   bitmap,
		TransID:  transID,
		UserData: userData,
	}
	if xo {
		h.Control |= atp.XO
		h.SetTRelTimeout(atp.TRel30s)
	}
	frame := h.Encode(make([]byte, 0, atp.HeaderSize+len(reqData)))
	frame = append(frame, reqData...)
	return a.ep.Send(dst, srcSocket, atp.DDPType, frame)
}

// sendTRel releases an exactly-once transaction so the responder can drop its
// retained response (best-effort — a lost TRel only costs the server a timeout).
func (a *ATP) sendTRel(dst Addr, srcSocket uint8, transID uint16, userData uint32) {
	h := atp.Header{
		Control:  atp.TREL,
		TransID:  transID,
		UserData: userData,
	}
	frame := h.Encode(make([]byte, 0, atp.HeaderSize))
	_ = a.ep.Send(dst, srcSocket, atp.DDPType, frame)
}

// tRespPacket is one decoded TResp.
type tRespPacket struct {
	seq      uint8
	eom      bool
	userData uint32
	payload  []byte
}

// decodeTResp decodes a datagram as a TResp for transID. ok is false when the datagram
// is not an ATP TResp, or its transaction id does not match.
func decodeTResp(d ddp.Datagram, transID uint16) (tRespPacket, bool) {
	if d.DDPType != atp.DDPType {
		return tRespPacket{}, false
	}
	h, err := atp.Decode(d.Data)
	if err != nil {
		return tRespPacket{}, false
	}
	if h.FuncCode() != atp.FuncTResp || h.TransID != transID {
		return tRespPacket{}, false
	}
	return tRespPacket{
		seq:      h.Bitmap, // sequence number in a TResp
		eom:      h.EOM(),
		userData: h.UserData,
		payload:  append([]byte(nil), d.Data[atp.HeaderSize:]...),
	}, true
}
