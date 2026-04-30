//go:build afp || all

package afp

import "sync"

// sessionState owns the small set of fields used by Login / AddUser to
// authenticate clients and hand out session reference numbers. Carved out of
// Service so that auth-path code paths do not contend with fork, desktop, or
// volume state under a single shared mutex.
type sessionState struct {
	mu       sync.Mutex
	users    map[string]string // map[username]password
	nextSRef uint16
}

func newSessionState() sessionState {
	return sessionState{
		users:    make(map[string]string),
		nextSRef: 1,
	}
}

// allocSRef returns the next session reference number.
func (s *sessionState) allocSRef() uint16 {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.nextSRef
	s.nextSRef++
	return n
}

// checkPassword returns true when the supplied credentials match a registered
// user. An unknown username yields false without distinguishing it from a
// password mismatch.
func (s *sessionState) checkPassword(username, password string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	expected, ok := s.users[username]
	return ok && expected == password
}

func (s *sessionState) addUser(username, password string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users[username] = password
}
