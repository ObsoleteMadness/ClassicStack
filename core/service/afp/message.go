package afp

// message.go is the server-message surface: the configured login greeting
// (FPGetSrvrMsg type 0), operator messages pushed to logged-in clients
// (SPAttention AspAttnMsg → FPGetSrvrMsg type 1), and the two-phase
// disconnect-with-message an observed AppleShare server performs (a shutdown
// attention carrying a minutes countdown, the final attention at the deadline,
// then a server-initiated CloseSession). The management plane drives it through
// Sessions / SendMessage / Disconnect; Stop reuses the same pieces so stopping
// the service announces itself to connected clients before closing their
// sessions.

import (
	"errors"
	"slices"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/protocol/asp"
)

// Errors returned by the operator message/disconnect API.
var (
	// ErrNotRunning is returned when the service is stopped.
	ErrNotRunning = errors.New("afp: service not running")
	// ErrNoSuchSession is returned when the addressed session id is not live.
	ErrNoSuchSession = errors.New("afp: no such session")
)

// defaultShutdownMessage is the text announced to connected clients when the
// service stops without an operator-supplied message.
const defaultShutdownMessage = "The server is shutting down."

// messageFetchGrace is how long clients are given to fetch announced message
// text (FPGetSrvrMsg) between the final shutdown attention and the CloseSession
// that ends the session. A var, not a const, so tests can shorten it.
var messageFetchGrace = 1500 * time.Millisecond

// SetLoginMessage configures the greeting served as the AFP login message
// (FPGetSrvrMsg type 0), which clients fetch and display when mounting a
// volume. Empty disables the greeting. The compose wiring supplies it from the
// [AFP] login_message option. Idempotent; safe before Start.
func (s *Service) SetLoginMessage(msg string) {
	s.mu.Lock()
	s.loginMsg = msg
	s.mu.Unlock()
}

// loginMessage returns the configured login greeting.
func (s *Service) loginMessage() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loginMsg
}

// SessionInfo is a diagnostics snapshot of one live ASP session for the
// management plane (the AFP analogue of smb.Service.Sessions).
type SessionInfo struct {
	ID       uint8     // ASP session id (1–255) — the handle SendMessage/Disconnect address
	Network  uint16    // client AppleTalk network
	Node     uint8     // client node
	User     string    // authenticated identity; "" = guest
	LoggedIn bool      // whether FPLogin has completed on the circuit
	LastSeen time.Time // last inbound packet (tickles included)
}

// Sessions returns a snapshot of the live ASP sessions, sorted by session id.
func (s *Service) Sessions() []SessionInfo {
	ids := s.sessions.ids()
	slices.Sort(ids)
	out := make([]SessionInfo, 0, len(ids))
	for _, id := range ids {
		sess, ok := s.sessions.get(id)
		if !ok {
			continue
		}
		info := SessionInfo{ID: id, Network: sess.net, Node: sess.node}
		if sess.conn != nil {
			info.User, info.LoggedIn = sess.conn.afp.identity()
		}
		sess.mu.Lock()
		info.LastSeen = sess.lastRx
		sess.mu.Unlock()
		out = append(out, info)
	}
	return out
}

// targetSessions resolves the sessions an operator action addresses: the one
// live session with the given id, or every live session when id is 0.
func (s *Service) targetSessions(sessionID uint8) ([]*session, error) {
	if sessionID != 0 {
		sess, ok := s.sessions.get(sessionID)
		if !ok {
			return nil, ErrNoSuchSession
		}
		return []*session{sess}, nil
	}
	ids := s.sessions.ids()
	out := make([]*session, 0, len(ids))
	for _, id := range ids {
		if sess, ok := s.sessions.get(id); ok {
			out = append(out, sess)
		}
	}
	return out, nil
}

// SendMessage stores text as the pending server message of the addressed
// session (or every session, id 0) and announces it with an SPAttention
// carrying the AspAttnMsg bit; the client then fetches and displays it via
// FPGetSrvrMsg type 1.
func (s *Service) SendMessage(sessionID uint8, text string) error {
	s.mu.Lock()
	running := s.running
	s.mu.Unlock()
	if !running {
		return ErrNotRunning
	}
	targets, err := s.targetSessions(sessionID)
	if err != nil {
		return err
	}
	for _, sess := range targets {
		if sess.conn != nil {
			sess.conn.afp.setServerMsg(text)
		}
		s.sendAttention(sess, asp.AspAttnMsg)
	}
	return nil
}

// Disconnect ends the addressed session (or every session, id 0) the way an
// observed AppleShare server does: a shutdown attention announcing the
// disconnect — with the message bit when text is given, and the countdown in
// minutes in the low attention bits — then, once the countdown elapses, the
// final time-zero attention, a short grace so the client can fetch the message
// text, and a server-initiated CloseSession. minutes 0 disconnects now (one
// attention, grace, close).
func (s *Service) Disconnect(sessionID uint8, text string, minutes int) error {
	s.mu.Lock()
	running := s.running
	s.mu.Unlock()
	if !running {
		return ErrNotRunning
	}
	targets, err := s.targetSessions(sessionID)
	if err != nil {
		return err
	}
	code := asp.AspAttnServerGoingDown | asp.AspAttnNoReconnect | asp.AspAttnTime(minutes)
	if text != "" {
		code |= asp.AspAttnMsg
	}
	for _, sess := range targets {
		if text != "" && sess.conn != nil {
			sess.conn.afp.setServerMsg(text)
		}
		s.sendAttention(sess, code)
		s.wg.Add(1)
		go s.finishDisconnect(sess, text != "", time.Duration(minutes)*time.Minute)
	}
	return nil
}

// finishDisconnect completes a Disconnect after its countdown: the final
// time-zero attention (when a countdown was announced), the message-fetch
// grace (when there is text to fetch), then the server-initiated CloseSession
// and teardown. It aborts silently if the session closes first (the client
// unmounted during the countdown) or the service drains.
func (s *Service) finishDisconnect(sess *session, hasMsg bool, wait time.Duration) {
	defer s.wg.Done()
	if wait > 0 {
		code := asp.AspAttnServerGoingDown | asp.AspAttnNoReconnect
		if hasMsg {
			code |= asp.AspAttnMsg
		}
		select {
		case <-sess.stop:
			return
		case <-s.drainStop:
			return
		case <-time.After(wait):
		}
		s.sendAttention(sess, code)
	}
	if hasMsg {
		select {
		case <-sess.stop:
			return
		case <-s.drainStop:
			return
		case <-time.After(messageFetchGrace):
		}
	}
	s.sendCloseSession(sess)
	s.teardownSession(sess)
}
