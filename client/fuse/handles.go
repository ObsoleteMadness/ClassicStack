package fuse

import (
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// openFile is one FUSE file handle. Directories carry no fs.File (ReadDir is
// path-based). A named-fork resource handle has rsrc=true and f is the resource
// fork.
type openFile struct {
	path    string
	isDir   bool
	rsrc    bool
	f       fs.File
	flag    int
	size    int64
	hasSize bool
}

type handleTable struct {
	mu   sync.Mutex
	next uint64
	m    map[uint64]*openFile
}

func newHandleTable() *handleTable {
	return &handleTable{next: 1, m: map[uint64]*openFile{}}
}

func (t *handleTable) add(h *openFile) uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := t.next
	t.next++
	t.m[key] = h
	return key
}

func (t *handleTable) get(fh uint64) (*openFile, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	h, ok := t.m[fh]
	return h, ok
}

func (t *handleTable) remove(fh uint64) (*openFile, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	h, ok := t.m[fh]
	delete(t.m, fh)
	return h, ok
}

// capReadBuf shortens a FUSE read to the remaining known size so the AFP
// client can size the ATP bitmap to the byte count (classicstack-web
// readForkRange). A zero-length result is EOF.
func capReadBuf(buf []byte, off, size int64, hasSize bool) []byte {
	if !hasSize {
		return buf
	}
	remain := size - off
	if remain <= 0 {
		return buf[:0]
	}
	if int64(len(buf)) > remain {
		return buf[:remain]
	}
	return buf
}
