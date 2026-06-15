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
