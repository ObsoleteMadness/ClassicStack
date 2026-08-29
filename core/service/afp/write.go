package afp

import (
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/protocol/asp"
)

// --- two-phase ASPWrite data path (spec/10 §"Two-Phase Write Protocol").
//
// When a workstation issues ASPUserWrite, the .XPP driver delivers the AFP
// command block (e.g. an FPWrite header) and the bulk write data in two ATP
// transactions:
//
//	phase 1  aspWrite (SPFunc 6)  WS → server   TReq, command block only, no data
//	phase 2a aspDataWrite (7)     server → WS    server-initiated TReq to the WS
//	                                             session socket, "send N bytes"
//	phase 2b data response        WS → server    TResp packets carrying the data
//	phase 3  final reply          server → WS    TResp to the *original* aspWrite
//	                                             TReq, carrying the AFP result
//
// The server is the *initiator* of the phase-2a transaction, so unlike every
// other exchange in this spine it must send a TReq of its own and correlate the
// workstation's TResp back to the pending write. The pendingWriteTable holds that
// in-flight state keyed by the transaction id the server stamps into its
// aspDataWrite TReq; the workstation echoes that id in its TResp. ---

// writeQuantum is the most write data the server pulls in one aspDataWrite
// transaction: 8 ATP packets × 578 bytes (spec/10 "quantumSize"). The .XPP driver
// caps each ASPUserWrite at the same quantum, so one aspDataWrite covers the
// reqCount of any single phase-1 aspWrite we will see.
const writeQuantum = asp.QuantumSize

// writeRetryInterval / writeMaxRetries bound the resend of a server-initiated
// aspDataWrite whose request or data response was lost (the spine drives it as a
// raw TReq, so it has no endpoint-level retransmission of its own). After
// writeMaxRetries unanswered attempts the write is abandoned and the phase-1
// aspWrite is failed. Chosen to match main's WriteContinue SendRequest
// (RetryTimeout 2s, MaxRetries 8).
const (
	writeRetryInterval = 2 * time.Second
	writeMaxRetries    = 8
)

// pendingWrite is one in-flight two-phase write: the phase-1 aspWrite request we
// must answer once the data arrives, the FPWrite command block bound to the AFP
// session, how many data bytes we asked the workstation for, and the data
// accumulated from the workstation's TResp packets so far.
type pendingWrite struct {
	orig   atpRequest // the phase-1 aspWrite TReq — phase 3 replies to this
	sess   *session   // the ASP session the write belongs to
	cmdBlk []byte     // the command block (FPWrite/FPAddIcon header) from phase 1
	hdrLen int        // fixed header length to splice the data back onto
	want   int        // bytes requested in the aspDataWrite (data is clamped to it)
	seq    uint16     // the phase-1 aspWrite ASP seqNum (echoed on aspDataWrite resends)
	data   []byte     // write data accumulated from TResp packets
}

// pendingWriteTable holds the in-flight two-phase writes keyed by the ATP
// transaction id the server assigned to the aspDataWrite TReq it sent. The
// workstation stamps that same id into its TResp, so the table demuxes the
// inbound data back to the right write.
type pendingWriteTable struct {
	mu     sync.Mutex
	byTID  map[uint16]*pendingWrite
	nextID uint16
}

func newPendingWriteTable() *pendingWriteTable {
	return &pendingWriteTable{byTID: make(map[uint16]*pendingWrite), nextID: 1}
}

// add registers a pending write under a freshly allocated transaction id and
// returns that id (which the caller stamps into the aspDataWrite TReq).
func (t *pendingWriteTable) add(pw *pendingWrite) uint16 {
	t.mu.Lock()
	defer t.mu.Unlock()
	tid := t.nextID
	for {
		if tid == 0 {
			tid = 1
		}
		if _, taken := t.byTID[tid]; !taken {
			break
		}
		tid++
	}
	t.byTID[tid] = pw
	t.nextID = tid + 1
	return tid
}

// get returns the pending write for a transaction id, if any.
func (t *pendingWriteTable) get(tid uint16) (*pendingWrite, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	pw, ok := t.byTID[tid]
	return pw, ok
}

// remove drops a completed (or abandoned) pending write.
func (t *pendingWriteTable) remove(tid uint16) {
	t.mu.Lock()
	delete(t.byTID, tid)
	t.mu.Unlock()
}
