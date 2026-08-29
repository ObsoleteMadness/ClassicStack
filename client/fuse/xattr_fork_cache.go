package fuse

import (
	"os"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// xattrForkIdle is how long an open AFP fork ref is kept between FUSE
// getxattr chunks on the same file. macOS reads com.apple.ResourceFork in
// ~128 KiB slices; reopening for each slice costs two ASP round trips per
// chunk on top of the FPReads inside the slice. The timer is refreshed after
// each readForkRange completes so a slow remote (many FPReads per chunk) does
// not expire the cache before the next sequential slice arrives.
const xattrForkIdle = 30 * time.Second

type xattrForkEntry struct {
	path string
	fork fs.ForkType
	f    fs.File
	at   time.Time
}

// invalidateXattrFork closes a cached xattr fork for store, if any.
func (a *Adapter) invalidateXattrFork(store string) {
	a.xattrForkMu.Lock()
	defer a.xattrForkMu.Unlock()
	if a.xattrFork != nil && a.xattrFork.path == store {
		a.closeXattrForkLocked()
	}
}

func (a *Adapter) closeXattrForkLocked() {
	if a.xattrFork == nil {
		return
	}
	_ = a.xattrFork.f.Close()
	a.xattrFork = nil
}

// acquireXattrFork returns a read-only fork handle for store, reusing the
// previous OpenFork when Finder reads the same resource fork sequentially.
func (a *Adapter) acquireXattrFork(store string, fork fs.ForkType) (fs.File, error) {
	a.xattrForkMu.Lock()
	defer a.xattrForkMu.Unlock()

	now := time.Now()
	if e := a.xattrFork; e != nil {
		if e.path == store && e.fork == fork && now.Sub(e.at) < xattrForkIdle {
			e.at = now
			return e.f, nil
		}
		a.closeXattrForkLocked()
	}
	f, err := a.fsys.OpenFork(store, fork, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	a.xattrFork = &xattrForkEntry{path: store, fork: fork, f: f, at: now}
	return f, nil
}

// touchXattrFork refreshes the idle deadline after a successful readForkRange.
func (a *Adapter) touchXattrFork() {
	a.xattrForkMu.Lock()
	defer a.xattrForkMu.Unlock()
	if a.xattrFork != nil {
		a.xattrFork.at = time.Now()
	}
}
