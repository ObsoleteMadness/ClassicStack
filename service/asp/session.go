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

// Session is the per-session state owned by SessionManager.
type Session struct {
	ID uint8

	// Workstation address (where Tickle/WriteContinue/Attention go).
	WSNet  uint16
	WSNode uint8
	WSSkt  uint8 // workstation session socket (WSS)

	// Server address as the workstation knows it (the destination of the
	// OpenSession request). Server-initiated packets must originate here so
	// the Mac's ASP layer accepts them.
	SrvNet  uint16
	SrvNode uint8

	// Sequence number duplicate filtering (spec §"Sequencing and duplicate
	// filtration"). Same seqNum + different ATP TID = true ASP duplicate
	// (drop). Same seqNum + same TID = ATP retransmission — but ATP XO
	// already filters those before they reach us, so we can drop them.
	seqMu      sync.Mutex
	lastReqNum uint16
	lastTID    uint16
	seqInited  bool

	// Two-phase Write state (one in flight per session is sufficient — the
	// Mac client serializes Write commands behind their seqNum).
	writeMu sync.Mutex
	write   *writeState

	lastActivity atomic.Int64 // Unix nanoseconds

	stop chan struct{}
}

func (s *Session) touchActivity() { s.lastActivity.Store(time.Now().UnixNano()) }

// beginWrite transitions the session's write state from Idle to AwaitingData
// and records the in-flight write. Returns false (and changes nothing) if a
// write is already in flight — protocol-wise this should not happen because
// the Mac client serialises Write commands behind seqNum, but we surface the
// invariant violation rather than silently overwrite.
func (s *Session) beginWrite(ws *writeState) bool {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
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
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
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

// Close terminates a session.
func (m *SessionManager) Close(id uint8) {
	m.mu.Lock()
	sess, ok := m.sessions[id]
	onClose := m.onClose
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	if ok {
		close(sess.stop)
		// Cancel any in-flight WriteContinue.
		if prev := sess.endWrite(); prev != nil && prev.pending != nil {
			prev.pending.Cancel()
		}
		if onClose != nil {
			onClose(sess)
		}
	}
}

// CheckDuplicate implements ASP sequence-number duplicate filtration.
// Returns true if the request should be processed; false if it is a duplicate
// and should be silently dropped.
func (s *Session) CheckDuplicate(seqNum, tid uint16) bool {
	s.seqMu.Lock()
	defer s.seqMu.Unlock()
	if s.seqInited && seqNum == s.lastReqNum && tid != s.lastTID {
		return false
	}
	s.lastReqNum = seqNum
	s.lastTID = tid
	s.seqInited = true
	return true
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
