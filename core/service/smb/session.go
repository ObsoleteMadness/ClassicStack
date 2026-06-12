package smb

import "sync"

// smbSession is the per-connection SMB state: the granted user id, the tree
// connects the client holds (TID → bound Share), and the id allocators. One
// session corresponds to one NetBIOS session (one client virtual circuit); the
// transport seam hands the service a session per connection and calls Dispatch
// with each decoded request frame. It holds no transport or storage knowledge —
// shares are reached through the bound *Share's FS().
//
// This is the core analogue of the legacy service/smb connState, but it binds a
// *Share directly (the §9 seam) rather than an index into a parallel shares
// slice, so a share removed from the Manager mid-session rides out on the held
// pointer until the tree disconnects (the RemoveShare contract).
type smbSession struct {
	mu      sync.Mutex
	uid     uint16
	trees   map[uint16]*treeConnect
	nextTID uint16
}

// treeConnect is one bound tree: the Share it resolves paths against, or the
// virtual IPC$ pipe share (share == nil, ipc == true) that LANMAN/named-pipe use
// rides. The FS command engine (later slice) reaches files through tc.share.FS().
type treeConnect struct {
	share *Share
	ipc   bool
}

// newSession builds an empty per-connection session. The UID is granted on
// SESSION_SETUP_ANDX; TIDs are allocated on TREE_CONNECT.
func newSession() *smbSession {
	return &smbSession{trees: make(map[uint16]*treeConnect)}
}

// allocTID hands out the next non-zero tree id and binds it to tc. TID 0 is
// reserved (it means "no tree"), so the allocator skips it on wrap.
func (sess *smbSession) allocTID(tc *treeConnect) uint16 {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.nextTID++
	if sess.nextTID == 0 {
		sess.nextTID++
	}
	tid := sess.nextTID
	sess.trees[tid] = tc
	return tid
}

// tree returns the tree connect bound to tid, if any.
func (sess *smbSession) tree(tid uint16) (*treeConnect, bool) {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	tc, ok := sess.trees[tid]
	return tc, ok
}

// dropTree releases a tree id (TREE_DISCONNECT). Releasing an unknown id is a
// no-op so a duplicate disconnect cannot disturb the session.
func (sess *smbSession) dropTree(tid uint16) {
	sess.mu.Lock()
	delete(sess.trees, tid)
	sess.mu.Unlock()
}
