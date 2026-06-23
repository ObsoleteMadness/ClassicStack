package smb

import (
	"sync"

	stdfs "io/fs"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// smbSession is the per-connection SMB state: the granted user id, the tree
// connects the client holds (TID → bound Share), the open file handles (FID →
// fileHandle), the in-progress directory searches (SID → searchHandle), and the
// id allocators. One session corresponds to one NetBIOS session (one client
// virtual circuit); the transport seam hands the service a session per connection
// and calls Dispatch with each decoded request frame. It holds no transport or
// storage knowledge — shares are reached through the bound *Share's FS().
//
// This is the core analogue of the legacy service/smb connState, but it binds a
// *Share directly (the §9 seam) rather than an index into a parallel shares
// slice, so a share removed from the Manager mid-session rides out on the held
// pointer until the tree disconnects (the RemoveShare contract).
type smbSession struct {
	mu       sync.Mutex
	uid      uint16
	user     string // authenticated identity from SESSION_SETUP; "" = guest
	trees    map[uint16]*treeConnect
	fids     map[uint16]*fileHandle
	searches map[uint16]*searchHandle
	nextTID  uint16
	nextFID  uint16
	nextSID  uint16

	// push delivers an unsolicited (server-initiated) SMB frame back over the
	// session's transport circuit. The transport installs it via Conn.SetPushWriter
	// after NewConn; nil on a transport that does not support server push, in which
	// case a deferred NOTIFY_CHANGE is simply never completed (the client times it
	// out, exactly as if the server held the request). Guarded by mu.
	push func([]byte)
	// watches holds the outstanding NT_TRANSACT NOTIFY_CHANGE requests the client
	// has posted and the server has not yet completed (§10d wire push). Each is a
	// one-shot: the first matching FS change completes and removes it.
	watches []*pendingNotify
}

// pendingNotify is one outstanding NOTIFY_CHANGE request the server is holding open
// until a change occurs under the watched tree. It captures the request ids so the
// asynchronous completion frame addresses the right client request, and the bound
// share so the reactor can match an FS event's tree.
type pendingNotify struct {
	tid    uint16
	uid    uint16
	mid    uint16
	pidLow uint16
	pidHi  uint16
	flags2 uint16 // request flags2 (Unicode/NTStatus) — the completion mirrors them
	filter uint32 // CompletionFilter bits the client asked to watch
	share  *Share // the tree's bound share; the reactor matches events under its root
}

// treeConnect is one bound tree: the Share it resolves paths against, or the
// virtual IPC$ pipe share (share == nil, ipc == true) that LANMAN/named-pipe use
// rides. The FS command engine reaches files through tc.share.FS().
type treeConnect struct {
	share *Share
	ipc   bool
}

// fileHandle is one open file: the fork.File the FID maps to (always the data
// fork — SMB has no native resource-fork concept; the AppleDouble/ADS/xattr
// container is the AFP side's concern), the store path it was opened against (so
// TRANS2 QueryFileInfo can re-Stat it), and whether the open granted write
// access. The handle reaches storage only through the share's FS, so it carries
// no storage-layout knowledge.
type fileHandle struct {
	share    *Share
	file     fs.File
	path     string // store path ('/'-separated), for re-Stat
	writable bool
	isDir    bool
}

// searchHandle is one in-progress directory enumeration (TRANS2 FIND_FIRST2 /
// FIND_NEXT2): the remaining rows not yet returned and the wire charset the
// search was opened with (so FIND_NEXT2 packs names the same way). The rows are
// snapshotted at FIND_FIRST2 time, matching the legacy behaviour and the
// connectionless-transport reality that the client may never send FIND_NEXT2.
type searchHandle struct {
	rows   []findRow
	flags2 uint16
}

// findRow is one resolved directory entry awaiting packing into a FIND_FIRST2 /
// FIND_NEXT2 record: its store-native leaf name, the derived 8.3 short name, and
// its FileInfo.
type findRow struct {
	name      string
	shortName string
	store     string // full store path, for the DOS-attribute store lookup
	info      stdfs.FileInfo
}

// newSession builds an empty per-connection session. The UID is granted on
// SESSION_SETUP_ANDX; TIDs/FIDs/SIDs are allocated by their respective commands.
func newSession() *smbSession {
	return &smbSession{
		trees:    make(map[uint16]*treeConnect),
		fids:     make(map[uint16]*fileHandle),
		searches: make(map[uint16]*searchHandle),
	}
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
// no-op so a duplicate disconnect cannot disturb the session. Open file handles
// on the tree are closed so a disconnect without explicit CLOSE does not leak.
func (sess *smbSession) dropTree(tid uint16) {
	sess.mu.Lock()
	tc := sess.trees[tid]
	delete(sess.trees, tid)
	var closing []*fileHandle
	if tc != nil && tc.share != nil {
		for fid, h := range sess.fids {
			if h.share == tc.share {
				closing = append(closing, h)
				delete(sess.fids, fid)
			}
		}
	}
	sess.mu.Unlock()
	for _, h := range closing {
		if h.file != nil {
			_ = h.file.Close()
		}
	}
}

// allocFID stores h under the next non-zero file id and returns it. FID 0 is
// reserved (it means "no file"), so the allocator skips it on wrap.
func (sess *smbSession) allocFID(h *fileHandle) uint16 {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.nextFID++
	if sess.nextFID == 0 {
		sess.nextFID++
	}
	fid := sess.nextFID
	sess.fids[fid] = h
	return fid
}

// fileByFID returns the open handle for fid, if any.
func (sess *smbSession) fileByFID(fid uint16) (*fileHandle, bool) {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	h, ok := sess.fids[fid]
	return h, ok
}

// closeFID closes and releases the handle for fid (SMB_COM_CLOSE). Closing an
// unknown id is a no-op so a duplicate close cannot disturb the session.
func (sess *smbSession) closeFID(fid uint16) {
	sess.mu.Lock()
	h, ok := sess.fids[fid]
	delete(sess.fids, fid)
	sess.mu.Unlock()
	if ok && h != nil && h.file != nil {
		_ = h.file.Close()
	}
}

// allocSID stores h under the next non-zero search id and returns it.
func (sess *smbSession) allocSID(h *searchHandle) uint16 {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.nextSID++
	if sess.nextSID == 0 {
		sess.nextSID++
	}
	sid := sess.nextSID
	sess.searches[sid] = h
	return sid
}

// search returns the in-progress search for sid, if any.
func (sess *smbSession) search(sid uint16) (*searchHandle, bool) {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	h, ok := sess.searches[sid]
	return h, ok
}

// dropSearch releases a search id (FIND_CLOSE2 / close-on-EOS flag).
func (sess *smbSession) dropSearch(sid uint16) {
	sess.mu.Lock()
	delete(sess.searches, sid)
	sess.mu.Unlock()
}

// setPush installs the transport's server-push writer (Conn.SetPushWriter). Safe to
// call once before the circuit serves messages.
func (sess *smbSession) setPush(w func([]byte)) {
	sess.mu.Lock()
	sess.push = w
	sess.mu.Unlock()
}

// addWatch registers an outstanding NOTIFY_CHANGE request (the server holds it open
// rather than replying). A session with no push writer still registers it so the
// bookkeeping is uniform; it just can never be delivered.
func (sess *smbSession) addWatch(w *pendingNotify) {
	sess.mu.Lock()
	sess.watches = append(sess.watches, w)
	sess.mu.Unlock()
}

// takeWatchesFor removes and returns the outstanding watches whose tree binds the
// given share — the one-shot completions a change under that share's root fires. It
// also returns the push writer so the caller can deliver them without holding the
// lock. NOTIFY_CHANGE is one-shot per [MS-CIFS]: a fired watch is consumed (the
// client re-arms by posting a fresh request).
func (sess *smbSession) takeWatchesFor(sh *Share) ([]*pendingNotify, func([]byte)) {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if len(sess.watches) == 0 || sess.push == nil {
		return nil, nil
	}
	var fired []*pendingNotify
	kept := sess.watches[:0]
	for _, w := range sess.watches {
		if w.share == sh {
			fired = append(fired, w)
		} else {
			kept = append(kept, w)
		}
	}
	sess.watches = kept
	return fired, sess.push
}

// closeAll closes every open file handle (called when the connection ends). The
// transport seam invokes this so a dropped connection does not leak file handles.
func (sess *smbSession) closeAll() {
	sess.mu.Lock()
	handles := make([]*fileHandle, 0, len(sess.fids))
	for _, h := range sess.fids {
		handles = append(handles, h)
	}
	sess.fids = make(map[uint16]*fileHandle)
	sess.searches = make(map[uint16]*searchHandle)
	sess.mu.Unlock()
	for _, h := range handles {
		if h != nil && h.file != nil {
			_ = h.file.Close()
		}
	}
}
