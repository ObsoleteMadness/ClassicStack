package etherdfs

import (
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// sessionTTL bounds how long a client's open-file/find state lingers without
// traffic. EtherDFS is otherwise stateless (DOS clients do not log off), so the
// state is reclaimed on idle rather than on an explicit teardown.
const sessionTTL = 5 * time.Minute

// maxOpenFiles caps the per-client open-file table so a misbehaving or departed
// client cannot pin unbounded handles.
const maxOpenFiles = 64

// openFile is one server-side open handle: the bound File, its store path, and
// whether the open is read-only. The DOS client tracks the seek position itself
// and passes an explicit offset on every READ/WRITE, so the server holds none.
type openFile struct {
	file     fs.File
	path     string
	readOnly bool
}

// findCursor is the in-progress directory enumeration a FINDFIRST opened and
// FINDNEXT advances: the resolved directory entries (already short-name mapped)
// and the attribute filter, indexed by the position the client echoes back.
type findCursor struct {
	entries []findEntry
	attr    uint8
}

// findEntry is one pre-resolved directory match: its 8.3 short name, size, modtime
// and FAT attribute, captured at FINDFIRST time so FINDNEXT is a pure cursor walk.
type findEntry struct {
	shortName string
	size      uint32
	dosTime   uint32
	attr      uint8
}

// session holds one client's transient state, keyed by its MAC. It guards an
// open-file table (file ID → openFile), the active find cursors (dir ID →
// findCursor), and a one-entry reply cache for request-sequence dedup (a repeated
// sequence replays the cached reply rather than re-running the side effect).
type session struct {
	mu sync.Mutex

	files   map[uint16]*openFile
	nextFID uint16

	cursors map[uint16]*findCursor
	nextDIR uint16

	lastSeq     uint8
	lastStatus  uint16
	lastPayload []byte
	haveLast    bool

	lastSeen time.Time
}

func newSession() *session {
	return &session{
		files:    make(map[uint16]*openFile),
		nextFID:  1,
		cursors:  make(map[uint16]*findCursor),
		nextDIR:  1,
		lastSeen: time.Now(),
	}
}

// addFile registers an open handle and returns its file ID, or ok=false when the
// per-client table is full.
func (s *session) addFile(of *openFile) (uint16, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.files) >= maxOpenFiles {
		return 0, false
	}
	id := s.nextFID
	s.nextFID++
	if s.nextFID == 0 {
		s.nextFID = 1
	}
	s.files[id] = of
	return id, true
}

// file returns the open handle for a file ID.
func (s *session) file(id uint16) (*openFile, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	of, ok := s.files[id]
	return of, ok
}

// closeFile closes and removes a handle. A missing ID is a no-op (EtherDFS CLOSE
// is best-effort).
func (s *session) closeFile(id uint16) {
	s.mu.Lock()
	of, ok := s.files[id]
	delete(s.files, id)
	s.mu.Unlock()
	if ok && of.file != nil {
		_ = of.file.Close()
	}
}

// addCursor registers a find cursor and returns its directory ID.
func (s *session) addCursor(c *findCursor) uint16 {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextDIR
	s.nextDIR++
	if s.nextDIR == 0 {
		s.nextDIR = 1
	}
	s.cursors[id] = c
	return id
}

// cursor returns the find cursor for a directory ID.
func (s *session) cursor(id uint16) (*findCursor, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.cursors[id]
	return c, ok
}

// cachedReply returns the cached (status, payload) for seq when it matches the
// last handled sequence (a retransmit), so the dispatch can replay it without
// re-running the side effect.
func (s *session) cachedReply(seq uint8) (status uint16, payload []byte, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.haveLast && s.lastSeq == seq {
		return s.lastStatus, s.lastPayload, true
	}
	return 0, nil, false
}

// cacheReply records the (status, payload) produced for seq, for retransmit dedup.
func (s *session) cacheReply(seq uint8, status uint16, payload []byte) {
	s.mu.Lock()
	s.lastSeq = seq
	s.lastStatus = status
	s.lastPayload = payload
	s.haveLast = true
	s.lastSeen = time.Now()
	s.mu.Unlock()
}

// closeAll closes every open handle (session reclamation).
func (s *session) closeAll() {
	s.mu.Lock()
	files := s.files
	s.files = make(map[uint16]*openFile)
	s.cursors = make(map[uint16]*findCursor)
	s.mu.Unlock()
	for _, of := range files {
		if of.file != nil {
			_ = of.file.Close()
		}
	}
}

// sessionTable maps a client MAC to its session, reclaiming idle ones.
type sessionTable struct {
	mu       sync.Mutex
	sessions map[[6]byte]*session
}

func newSessionTable() *sessionTable {
	return &sessionTable{sessions: make(map[[6]byte]*session)}
}

// get returns the session for mac, creating one on first contact and opportunistically
// reclaiming sessions idle past sessionTTL. changed reports whether the live session
// count changed (a session was created and/or one or more were reclaimed), so a
// caller can publish a session-table-changed event only when it actually is one —
// EtherDFS has no distinct connect opcode, so get() is called on every request and
// most calls touch no membership change at all.
func (t *sessionTable) get(mac [6]byte) (s *session, changed bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	s, ok := t.sessions[mac]
	if !ok {
		s = newSession()
		t.sessions[mac] = s
		changed = true
	} else {
		s.mu.Lock()
		s.lastSeen = now
		s.mu.Unlock()
	}
	for m, other := range t.sessions {
		if m == mac {
			continue
		}
		other.mu.Lock()
		idle := now.Sub(other.lastSeen) > sessionTTL
		other.mu.Unlock()
		if idle {
			other.closeAll()
			delete(t.sessions, m)
			changed = true
		}
	}
	return s, changed
}

// count returns the number of live client sessions (diagnostics / Stats gauge).
func (t *sessionTable) count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.sessions)
}

// SessionInfo is a diagnostics snapshot of one EtherDFS client.
type SessionInfo struct {
	MAC       string
	OpenFiles int
	LastSeen  time.Time
}

func formatSessionMAC(mac [6]byte) string {
	const hex = "0123456789abcdef"
	b := make([]byte, 0, 17)
	for i, v := range mac {
		if i > 0 {
			b = append(b, ':')
		}
		b = append(b, hex[v>>4], hex[v&0x0F])
	}
	return string(b)
}

// list snapshots live client sessions for the Sharing Monitor.
func (t *sessionTable) list() []SessionInfo {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]SessionInfo, 0, len(t.sessions))
	for mac, s := range t.sessions {
		s.mu.Lock()
		out = append(out, SessionInfo{
			MAC:       formatSessionMAC(mac),
			OpenFiles: len(s.files),
			LastSeen:  s.lastSeen,
		})
		s.mu.Unlock()
	}
	return out
}

// closeAll tears down every session (service Stop) and reports how many there were.
func (t *sessionTable) closeAll() int {
	t.mu.Lock()
	sessions := t.sessions
	t.sessions = make(map[[6]byte]*session)
	t.mu.Unlock()
	for _, s := range sessions {
		s.closeAll()
	}
	return len(sessions)
}
