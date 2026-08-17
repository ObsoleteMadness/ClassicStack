package fuse

import (
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// openFile is one FUSE file handle. Directories carry no fs.File (ReadDir is
// path-based). A named-fork resource handle has rsrc=true and f is the resource
// fork.
type openFile struct {
	path  string
	isDir bool
	rsrc  bool
	f     fs.File
	flag  int
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
