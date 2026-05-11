package netbios

import (
	"sync"
	"sync/atomic"
)

// SessionState tracks the lifecycle of a NetBIOS session independent
// of the underlying transport.
type SessionState uint8

const (
	SessionStateInit SessionState = iota
	SessionStateActive
	SessionStateClosing
	SessionStateClosed
)

// Session is a transport-agnostic session record.
//
// RemoteAddr carries transport-specific peer addressing information
// (for example Ethernet MAC for NBF or IPX endpoint for NBIPX).
type Session[Remote comparable] struct {
	Mu sync.Mutex

	LocalNum  uint8
	RemoteNum uint8
	RemoteAddr Remote
	State     SessionState

	// LastXmitCorrelator tracks the most recent outbound correlator
	// used by transports that require wire-level ACK correlation.
	LastXmitCorrelator uint16
}

type sessionKey[Remote comparable] struct {
	remote Remote
	local  uint8
}

// SessionTable manages active sessions for a specific transport
// address type.
type SessionTable[Remote comparable] struct {
	mu       sync.RWMutex
	sessions map[sessionKey[Remote]]*Session[Remote]
	nextNum  atomic.Uint32
	minNum   uint8
	maxNum   uint8
}

// NewSessionTable creates a session table that allocates local
// session numbers in the inclusive range [minNum, maxNum].
func NewSessionTable[Remote comparable](minNum, maxNum uint8) *SessionTable[Remote] {
	if minNum == 0 {
		minNum = 1
	}
	if maxNum < minNum {
		maxNum = minNum
	}
	st := &SessionTable[Remote]{
		sessions: make(map[sessionKey[Remote]]*Session[Remote]),
		minNum:   minNum,
		maxNum:   maxNum,
	}
	st.nextNum.Store(uint32(minNum))
	return st
}

// allocNum returns the next available local session number.
func (st *SessionTable[Remote]) allocNum() uint8 {
	for {
		cur := st.nextNum.Load()
		next := cur + 1
		if next > uint32(st.maxNum) {
			next = uint32(st.minNum)
		}
		if st.nextNum.CompareAndSwap(cur, next) {
			return uint8(cur)
		}
	}
}

// Create allocates a new session for a remote peer.
func (st *SessionTable[Remote]) Create(remote Remote) *Session[Remote] {
	num := st.allocNum()
	s := &Session[Remote]{
		LocalNum:   num,
		RemoteAddr: remote,
		State:      SessionStateInit,
	}
	key := sessionKey[Remote]{remote: remote, local: num}
	st.mu.Lock()
	st.sessions[key] = s
	st.mu.Unlock()
	return s
}

// Lookup returns the session for (remote, localNum), or nil.
func (st *SessionTable[Remote]) Lookup(remote Remote, localNum uint8) *Session[Remote] {
	key := sessionKey[Remote]{remote: remote, local: localNum}
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.sessions[key]
}

// LookupByRemote returns the first session matching remote+remoteNum.
func (st *SessionTable[Remote]) LookupByRemote(remote Remote, remoteNum uint8) *Session[Remote] {
	st.mu.RLock()
	defer st.mu.RUnlock()
	for _, s := range st.sessions {
		if s.RemoteAddr == remote && s.RemoteNum == remoteNum {
			return s
		}
	}
	return nil
}

// Remove deletes a session from the table.
func (st *SessionTable[Remote]) Remove(remote Remote, localNum uint8) {
	key := sessionKey[Remote]{remote: remote, local: localNum}
	st.mu.Lock()
	delete(st.sessions, key)
	st.mu.Unlock()
}

// All returns a snapshot of active sessions.
func (st *SessionTable[Remote]) All() []*Session[Remote] {
	st.mu.RLock()
	defer st.mu.RUnlock()
	out := make([]*Session[Remote], 0, len(st.sessions))
	for _, s := range st.sessions {
		out = append(out, s)
	}
	return out
}
