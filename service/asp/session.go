//go:build afp || all

// Package asp — SessionManager.
//
// SessionManager owns the lifecycle of every open ASP session: tickle
// keep-alive, inactivity timeout, ASP-level sequence number duplicate
// filtering, and the per-session two-phase Write state. Each session has
// one goroutine driving its tickle/timeout loop; everything else runs on
// the engine's inbound goroutine and is protected by per-session locks.
package asp

import (
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pgodw/omnitalk/netlog"
	"github.com/pgodw/omnitalk/service/atp"
)

// sessionState names the lifecycle of an ASP session. Legal transitions:
//
//	stateOpen    -> stateClosing  (Close called)
//	stateClosing -> stateClosed   (teardown complete)
//
// Inbound handlers atomically check stateOpen at entry and bail if the
// session is on its way down — guarding against the race where an inbound
// frame and CloseSess interleave.
type sessionState uint32

const (
	stateOpen sessionState = iota
	stateClosing
	stateClosed
)

func (s sessionState) String() string {
	switch s {
	case stateOpen:
		return "Open"
	case stateClosing:
		return "Closing"
	case stateClosed:
		return "Closed"
	default:
		return "?"
	}
}

// Session is the per-session state owned by SessionManager.
type Session struct {
	ID uint8

	// state is read by every inbound handler and written by Close. atomic
	// because it is accessed without holding mu.
	state atomic.Uint32 // sessionState

	// Workstation address (where Tickle/WriteContinue/Attention go).
	WSNet  uint16
	WSNode uint8
	WSSkt  uint8 // workstation session socket (WSS)

	// Server address as the workstation knows it (the destination of the
	// OpenSession request). Server-initiated packets must originate here so
	// the Mac's ASP layer accepts them.
	SrvNet  uint16
	SrvNode uint8

	// mu serialises everything mutable that can be touched from both the
	// engine inbound goroutine and Close (running on the maintenance
	// goroutine or the inbound goroutine that handled CloseSess): the
	// sequence-number filter and the two-phase write state. Hold time is
	// microseconds; one lock is simpler to reason about than two.
	mu sync.Mutex

	// seq filters ASP-level duplicates per spec §"Sequencing and duplicate
	// filtration". Held under mu.
	seq seqFilter

	// Two-phase Write state (one in flight per session is sufficient — the
	// Mac client serializes Write commands behind their seqNum).
	write *writeState

	lastActivity atomic.Int64 // Unix nanoseconds

	stop chan struct{}
}

func (s *Session) touchActivity() { s.lastActivity.Store(time.Now().UnixNano()) }

// isOpen reports whether the session is still accepting inbound traffic.
// Once Close transitions it out of stateOpen, every handler should bail.
func (s *Session) isOpen() bool { return sessionState(s.state.Load()) == stateOpen }

// markClosing atomically transitions stateOpen->stateClosing. Returns true
// if this caller won the transition and is responsible for teardown.
func (s *Session) markClosing() bool {
	return s.state.CompareAndSwap(uint32(stateOpen), uint32(stateClosing))
}

// markClosed marks teardown complete. Idempotent.
func (s *Session) markClosed() { s.state.Store(uint32(stateClosed)) }

// beginWrite transitions the session's write state from Idle to AwaitingData
// and records the in-flight write. Returns false (and changes nothing) if a
// write is already in flight — protocol-wise this should not happen because
// the Mac client serialises Write commands behind seqNum, but we surface the
// invariant violation rather than silently overwrite.
func (s *Session) beginWrite(ws *writeState) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.write != nil && s.write.phase != writeIdle {
		return false
	}
	ws.phase = writeAwaitingData
	s.write = ws
	return true
}

// endWrite transitions back to Idle and clears the in-flight write,
// returning the previous state (if any) so callers can act on its pending.
// Safe to call on an already-Idle session — returns nil.
func (s *Session) endWrite() *writeState {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev := s.write
	s.write = nil
	return prev
}

// writePhase names the states of the SPWrite two-phase exchange so each
// transition is checked against a known-legal edge instead of inferred
// from field nil-ness. Legal edges:
//
//	writeIdle          -> writeAwaitingData   (handleASPWrite sent WriteContinue TReq)
//	writeAwaitingData  -> writeIdle           (completeWrite resolved or cancelled)
type writePhase uint8

const (
	writeIdle writePhase = iota
	writeAwaitingData
)

func (p writePhase) String() string {
	switch p {
	case writeIdle:
		return "Idle"
	case writeAwaitingData:
		return "AwaitingData"
	default:
		return "?"
	}
}

// writeState holds in-flight state for the two-phase aspWrite protocol.
type writeState struct {
	phase     writePhase
	seqNum    uint16
	cmdBlock  []byte
	wantBytes uint32
	reply     atp.Replier  // outstanding reply for the original Write TReq
	pending   *atp.Pending // the WriteContinue TReq we issued to the Mac
}

// SessionManager owns the live ASP sessions.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[uint8]*Session

	tickleInterval time.Duration
	maxIdle        time.Duration

	// callbacks supplied by the parent Service
	sendTickle func(*Session)
	onClose    func(*Session)
	stop       chan struct{}
}

// NewSessionManager constructs a SessionManager.
func NewSessionManager(sendTickle func(*Session)) *SessionManager {
	return &SessionManager{
		sessions:       make(map[uint8]*Session),
		tickleInterval: TickleInterval,
		maxIdle:        SessionMaintenanceTimeout,
		sendTickle:     sendTickle,
		stop:           make(chan struct{}),
	}
}

// SetOnClose registers a callback invoked whenever a session is closed.
func (m *SessionManager) SetOnClose(cb func(*Session)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onClose = cb
}

// Stop terminates all per-session goroutines.
func (m *SessionManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	close(m.stop)
	for id, sess := range m.sessions {
		close(sess.stop)
		delete(m.sessions, id)
	}
}

// Open allocates a new session ID and starts the maintenance goroutine.
// Returns 0 if no session ID is available.
func (m *SessionManager) Open(wsNet uint16, wsNode, wssSocket uint8, srvNet uint16, srvNode uint8) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	var id uint8
	for i := 1; i <= 255; i++ {
		if _, ok := m.sessions[uint8(i)]; !ok {
			id = uint8(i)
			break
		}
	}
	if id == 0 {
		return nil
	}
	sess := &Session{
		ID:      id,
		WSNet:   wsNet,
		WSNode:  wsNode,
		WSSkt:   wssSocket,
		SrvNet:  srvNet,
		SrvNode: srvNode,
		stop:    make(chan struct{}),
	}
	sess.touchActivity()
	m.sessions[id] = sess
	go m.maintenance(sess)
	return sess
}

// Get returns the session for an ID, or nil.
func (m *SessionManager) Get(id uint8) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[id]
}

// SessionIDs returns a snapshot of currently active session IDs.
func (m *SessionManager) SessionIDs() []uint8 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return slices.Collect(maps.Keys(m.sessions))
}

// Close terminates a session. The CAS on session state means concurrent
// callers (e.g. CloseSess inbound + maintenance timeout) observe a single
// teardown; only the winner runs the cancellation and onClose callback.
func (m *SessionManager) Close(id uint8) {
	m.mu.Lock()
	sess, ok := m.sessions[id]
	onClose := m.onClose
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	if !ok {
		return
	}
	if !sess.markClosing() {
		// Another goroutine already started teardown.
		return
	}
	close(sess.stop)
	if prev := sess.endWrite(); prev != nil && prev.pending != nil {
		prev.pending.Cancel()
	}
	sess.markClosed()
	if onClose != nil {
		onClose(sess)
	}
}

// seqFilter implements ASP sequence-number duplicate filtration per spec
// §"Sequencing and duplicate filtration". A request whose seqNum repeats
// the last accepted seqNum but carries a different ATP TID is a true
// ASP-level duplicate and is dropped. (Same seqNum + same TID is an ATP
// retransmission, but ATP XO already filters those before they reach us.)
//
// Stored under Session.mu; the type itself is intentionally lock-free
// so it can be unit-tested in isolation.
type seqFilter struct {
	lastSeq uint16
	lastTID uint16
	inited  bool
}

// accept records (seq, tid) and reports whether the request should be
// processed. False means duplicate — drop.
func (f *seqFilter) accept(seq, tid uint16) bool {
	if f.inited && seq == f.lastSeq && tid != f.lastTID {
		return false
	}
	f.lastSeq = seq
	f.lastTID = tid
	f.inited = true
	return true
}

// CheckDuplicate is the locked Session-level entrypoint for seqFilter.accept.
// Returns true if the request should be processed; false if it is a duplicate
// and should be silently dropped.
func (s *Session) CheckDuplicate(seqNum, tid uint16) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seq.accept(seqNum, tid)
}

// maintenance runs the per-session tickle + inactivity-timeout loop.
func (m *SessionManager) maintenance(sess *Session) {
	ticker := time.NewTicker(m.tickleInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-sess.stop:
			return
		case <-ticker.C:
			last := time.Unix(0, sess.lastActivity.Load())
			if time.Since(last) > m.maxIdle {
				netlog.Info("[ASP] session %d timed out (idle %v), closing", sess.ID, m.maxIdle)
				m.Close(sess.ID)
				return
			}
			if m.sendTickle != nil {
				m.sendTickle(sess)
			}
		}
	}
}
