//go:build afp || all

package afp

import "sync"

// forkState owns the open-fork table, the next-fork allocator, and the
// byte-range lock list. AFP fork operations (FPOpenFork / FPCloseFork /
// FPRead / FPWrite / FPByteRangeLock / FPGetForkParms / FPSetForkParms /
// FPFlush*) hammer this state on every active session, so it lives behind
// its own RWMutex to keep auth, desktop, and volume traffic off the same
// contention domain.
type forkState struct {
	mu       sync.RWMutex
	forks    map[uint16]*forkHandle
	nextFork uint16
	locks    []byteRangeLock
	maxLocks int
}

func newForkState(maxLocks int) forkState {
	return forkState{
		forks:    make(map[uint16]*forkHandle),
		nextFork: 1,
		locks:    make([]byteRangeLock, 0),
		maxLocks: maxLocks,
	}
}

// register installs handle and returns the new fork id.
func (f *forkState) register(handle *forkHandle) uint16 {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := f.nextFork
	f.nextFork++
	f.forks[id] = handle
	return id
}

// get returns the handle bound to id (or nil + false). Read-locked, suitable
// for the hot Read/Write path.
func (f *forkState) get(id uint16) (*forkHandle, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	h, ok := f.forks[id]
	return h, ok
}

// close drops the fork id, evicts every byte-range lock owned by it, and
// returns the previously-bound handle. The caller is responsible for any
// I/O cleanup (file.Close) outside the lock.
func (f *forkState) close(id uint16) (*forkHandle, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	h, ok := f.forks[id]
	if !ok {
		return nil, false
	}
	delete(f.forks, id)
	if len(f.locks) > 0 {
		filtered := f.locks[:0]
		for i := range f.locks {
			if f.locks[i].ownerFork != id {
				filtered = append(filtered, f.locks[i])
			}
		}
		f.locks = filtered
	}
	return h, true
}

// snapshot returns a copy of every currently-open fork handle. Used by
// FPFlush so the actual file.Sync calls can run without holding the fork
// lock.
func (f *forkState) snapshot() []*forkHandle {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]*forkHandle, 0, len(f.forks))
	for _, h := range f.forks {
		out = append(out, h)
	}
	return out
}

// lock acquires the write lock and returns an unlock func. The byte-range
// lock state machine in fork.go takes the write lock for the duration of
// its handle validation + lock-list scan + insertion.
func (f *forkState) lock() func() {
	f.mu.Lock()
	return f.mu.Unlock
}
