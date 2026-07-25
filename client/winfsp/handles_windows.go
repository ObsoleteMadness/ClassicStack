//go:build windows

package winfsp

import (
	"sync"

	winfsp "github.com/winfsp/go-winfsp"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// openFile is one open WinFsp handle. WinFsp is handle-oriented and so is core/fs.File, so
// the mapping is 1:1 — no per-request re-open. A directory carries no fs.File (ReadDir is
// path-based); dirBuf is the go-winfsp directory buffer WinFsp requires per open dir.
type openFile struct {
	path       string // '/'-separated store path ("" = root)
	isDir      bool
	f          fs.File // nil for directories
	dirBuf     winfsp.DirBuffer
	dirBufUsed bool // true once WinFsp took the buffer via GetOrNewDirBuffer
}

// handleTable maps the opaque WinFsp fileContext uintptr to our *openFile. The context is
// handed back on every Read/Write/GetFileInfo/Cleanup/Close, so we allocate a monotonic
// key and store the handle here.
type handleTable struct {
	mu   sync.Mutex
	next uintptr
	m    map[uintptr]*openFile
}

func newHandleTable() *handleTable {
	return &handleTable{next: 1, m: map[uintptr]*openFile{}}
}

// add stores h and returns its context key (never 0, which WinFsp treats as "no context").
func (t *handleTable) add(h *openFile) uintptr {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := t.next
	t.next++
	t.m[key] = h
	return key
}

// get returns the handle for a context key.
func (t *handleTable) get(ctx uintptr) (*openFile, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	h, ok := t.m[ctx]
	return h, ok
}

// remove drops a context key and returns the handle it held (for teardown).
func (t *handleTable) remove(ctx uintptr) (*openFile, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	h, ok := t.m[ctx]
	delete(t.m, ctx)
	return h, ok
}
