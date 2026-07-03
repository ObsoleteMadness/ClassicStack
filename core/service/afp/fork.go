package afp

import (
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// forkHandle is one open fork held by a session: the fork engine's File plus the
// volume it belongs to, the store path it backs, the fork type, and whether the
// open is writable. The handle reaches storage only through the File (positional
// ReadAt/WriteAt/Truncate), so it carries no AppleDouble/stream/EA knowledge —
// that is the fork engine's concern, behind v.FS().OpenFork. The data fork *is*
// the file; the resource fork is whatever container the share's fork backend
// presents, identically shaped to the caller.
type forkHandle struct {
	vol      *Volume
	file     fs.File
	path     string
	fork     fs.ForkType
	writable bool
}

// forkTable holds a session's open forks keyed by a 16-bit fork reference number
// (OForkRefNum on the wire), and allocates new ones. Fork refs are per session,
// so closing the session (or never closing a fork) reclaims them with the
// session; a fork ref means nothing to another session. Allocation walks from the
// last ref so a busy session reuses freed refs predictably. Ref 0 is reserved as
// "no fork" the way 0 is reserved for session ids.
type forkTable struct {
	mu      sync.Mutex
	byRef   map[uint16]*forkHandle
	nextRef uint16
	locks   []byteRangeLock // active FPByteRangeLock ranges for this session
}

// maxByteRangeLocks caps a session's simultaneous byte-range locks; a request
// past this answers kFPNoMoreLocks (Inside Macintosh: Networking, FPByteRangeLock).
const maxByteRangeLocks = 4096

// byteRangeLock is one held FPByteRangeLock range. lockKey scopes the lock to a
// fork of a file ("data:"/"rsrc:" + path) so two forks of the same file share a
// lock namespace, matching the Mac's per-fork locking. length == -1 locks from
// start to end of fork (the open-ended range the spec allows).
type byteRangeLock struct {
	lockKey   string
	ownerFork uint16
	start     int64
	length    int64
}

func newForkTable() *forkTable {
	return &forkTable{byRef: make(map[uint16]*forkHandle), nextRef: 1}
}

// open registers a handle and returns its fork ref, or ok=false if all 65535
// refs are in use (the client sees kFPTooManyFilesOpen → kFPMiscErr here).
func (t *forkTable) open(h *forkHandle) (uint16, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.byRef) >= 0xFFFF {
		return 0, false
	}
	ref := t.nextRef
	for {
		if ref == 0 {
			ref = 1
		}
		if _, taken := t.byRef[ref]; !taken {
			break
		}
		ref++
	}
	t.byRef[ref] = h
	t.nextRef = ref + 1
	return ref, true
}

// get returns the handle for a fork ref, if open in this session.
func (t *forkTable) get(ref uint16) (*forkHandle, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	h, ok := t.byRef[ref]
	return h, ok
}

// close removes a fork ref and returns its handle so the caller can Close the
// File. ok=false if the ref was never open.
func (t *forkTable) close(ref uint16) (*forkHandle, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	h, ok := t.byRef[ref]
	if ok {
		delete(t.byRef, ref)
	}
	return h, ok
}

// closeAll closes every open fork (session teardown), draining the table.
func (t *forkTable) closeAll() {
	t.mu.Lock()
	handles := make([]*forkHandle, 0, len(t.byRef))
	for _, h := range t.byRef {
		handles = append(handles, h)
	}
	t.byRef = make(map[uint16]*forkHandle)
	t.mu.Unlock()
	for _, h := range handles {
		if h.file != nil {
			_ = h.file.Close()
		}
	}
}
