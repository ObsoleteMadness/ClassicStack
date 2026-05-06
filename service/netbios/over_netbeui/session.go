package over_netbeui

import (
	"sync"
	"sync/atomic"
)

// sessionState tracks the lifecycle of an NBF session.
type sessionState uint8

const (
	sessionStateInit    sessionState = iota // SESSION_INITIALIZE sent/received
	sessionStateActive                      // SESSION_CONFIRM exchanged; data flows
	sessionStateClosing                     // SESSION_END sent
	sessionStateClosed                      // fully torn down
)

// session tracks a single NBF session between this node and a remote.
type session struct {
	mu sync.Mutex

	localNum  uint8      // our session number
	remoteNum uint8      // peer's session number
	remoteMAC [6]byte    // peer Ethernet address
	state     sessionState

	// Correlator tracking for DATA_ACK.
	lastXmitCorrelator uint16
}

// sessionKey uniquely identifies a session by the (remoteMAC,
// localSessionNumber) pair. The remote's session number is learned
// during SESSION_INITIALIZE but the local number is what we allocate.
type sessionKey struct {
	remoteMAC [6]byte
	localNum  uint8
}

// sessionTable manages active sessions. It is thread-safe.
type sessionTable struct {
	mu       sync.RWMutex
	sessions map[sessionKey]*session
	nextNum  atomic.Uint32 // next local session number (1–254)
}

func newSessionTable() *sessionTable {
	st := &sessionTable{
		sessions: make(map[sessionKey]*session),
	}
	st.nextNum.Store(1)
	return st
}

// allocNum returns the next available local session number (1–254).
// It wraps around to 1 after 254.
func (st *sessionTable) allocNum() uint8 {
	for {
		cur := st.nextNum.Load()
		next := cur + 1
		if next > 254 {
			next = 1
		}
		if st.nextNum.CompareAndSwap(cur, next) {
			return uint8(cur)
		}
	}
}

// Create allocates a new session with the given remote MAC and
// returns it. The session starts in sessionStateInit.
func (st *sessionTable) Create(remoteMAC [6]byte) *session {
	num := st.allocNum()
	s := &session{
		localNum:  num,
		remoteMAC: remoteMAC,
		state:     sessionStateInit,
	}
	key := sessionKey{remoteMAC: remoteMAC, localNum: num}
	st.mu.Lock()
	st.sessions[key] = s
	st.mu.Unlock()
	return s
}

// Lookup returns the session for the given key, or nil.
func (st *sessionTable) Lookup(remoteMAC [6]byte, localNum uint8) *session {
	key := sessionKey{remoteMAC: remoteMAC, localNum: localNum}
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.sessions[key]
}

// LookupByRemote returns the first session matching the remote MAC
// and remote session number. Used when receiving frames that identify
// sessions by the remote's number.
func (st *sessionTable) LookupByRemote(remoteMAC [6]byte, remoteNum uint8) *session {
	st.mu.RLock()
	defer st.mu.RUnlock()
	for _, s := range st.sessions {
		if s.remoteMAC == remoteMAC && s.remoteNum == remoteNum {
			return s
		}
	}
	return nil
}

// Remove deletes a session from the table.
func (st *sessionTable) Remove(remoteMAC [6]byte, localNum uint8) {
	key := sessionKey{remoteMAC: remoteMAC, localNum: localNum}
	st.mu.Lock()
	delete(st.sessions, key)
	st.mu.Unlock()
}

// All returns a snapshot of all active sessions.
func (st *sessionTable) All() []*session {
	st.mu.RLock()
	defer st.mu.RUnlock()
	out := make([]*session, 0, len(st.sessions))
	for _, s := range st.sessions {
		out = append(out, s)
	}
	return out
}
