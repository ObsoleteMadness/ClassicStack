//go:build fswatch || all

// Package fswatch is the §10e inbound edge of the FS-mutation bus: a host-filesystem
// watcher (fsnotify) that turns out-of-band changes — something edits a file UNDER a
// share root OUTSIDE ClassicStack — into fs.Event{Origin:"fsnotify"} published on the
// same shared bus the file services' reactors subscribe to (§10d). The SMB reactor
// then completes any held NOTIFY_CHANGE, so a Windows client refreshes its view; the
// AFP side observes it (AFP has no per-dir push, by protocol).
//
// Ring: ADAPTER. It imports a heavy, OS-specific dependency (fsnotify) and uses
// os/filepath, so it lives outside core and is build-tagged (fswatch || all). A
// platform or build without it simply omits the watcher — an embedded FS-image
// backend has no external mutator and needs none. The watcher holds NO protocol or
// storage-layout knowledge: it publishes generic fs.Events keyed by host path; the
// per-host-path bus routing and the Origin-stamping are supplied by the caller
// (compose), exactly like a file service's own FS publisher.
package fswatch

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// Name is the component name for the host watcher.
const Name = "FSWatch"

// BusFor resolves the shared FS-mutation bus for a host path (the compose fsBus
// broker's busFor) so a watcher event lands on the SAME bus a same-path AFP volume /
// SMB share holds. Mirrors the file-service bus resolver.
type BusFor func(hostPath string) bus.Bus

// Watcher watches a set of host roots and republishes their changes onto the FS bus
// as Origin:"fsnotify" events. It is a component.Component: Start opens the OS
// watcher and walks each root; Stop closes it. Idempotent per the component
// contract.
type Watcher struct {
	logger Logger
	roots  []string
	busFor BusFor

	mu      sync.Mutex
	w       *fsnotify.Watcher
	cancel  context.CancelFunc
	running bool
}

// Logger is the minimal logging seam (so the adapter need not import core/log's
// full surface). A nil logger silences the watcher.
type Logger interface {
	Logf(format string, args ...any)
}

// New builds a watcher over the given host roots, publishing through busFor. A root
// that does not exist (or is a file) is skipped at Start with a log line, not a
// fatal error — a share may point at a path created later. busFor must be non-nil
// (the watcher has nowhere to publish otherwise).
func New(logger Logger, busFor BusFor, roots []string) *Watcher {
	return &Watcher{logger: logger, busFor: busFor, roots: append([]string(nil), roots...)}
}

// Name returns the component name.
func (w *Watcher) Name() string { return Name }

// Start opens the OS watcher, adds each existing root and its subdirectories (fsnotify
// watches directories, not trees), and runs the translation loop. Idempotent.
func (w *Watcher) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.running {
		return nil
	}
	if w.busFor == nil {
		return nil // nothing to publish to — stay inert rather than error
	}
	nw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	for _, root := range w.roots {
		w.addTree(nw, root)
	}
	loopCtx, cancel := context.WithCancel(context.Background())
	w.w = nw
	w.cancel = cancel
	w.running = true
	go w.loop(loopCtx, nw)
	return nil
}

// Stop closes the OS watcher and ends the loop. Safe after a failed/partial Start.
func (w *Watcher) Stop(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.running {
		return nil
	}
	w.running = false
	if w.cancel != nil {
		w.cancel()
	}
	err := w.w.Close()
	w.w = nil
	return err
}

// addTree adds dir and every subdirectory under it to the watcher (fsnotify watches a
// directory's immediate entries, so a recursive watch is "add every dir"). A path
// that is not a directory, or cannot be walked, is logged and skipped.
func (w *Watcher) addTree(nw *fsnotify.Watcher, root string) {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		w.logf("fswatch: skipping non-directory root %q", root)
		return
	}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries, keep walking
		}
		if d.IsDir() {
			if addErr := nw.Add(path); addErr != nil {
				w.logf("fswatch: add %q: %v", path, addErr)
			}
		}
		return nil
	})
}

// loop translates fsnotify events to fs.Events and publishes them. A newly-created
// directory is added to the watch so its future contents are covered. The loop ends
// when the context is cancelled (Stop) or the events channel closes.
func (w *Watcher) loop(ctx context.Context, nw *fsnotify.Watcher) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-nw.Events:
			if !ok {
				return
			}
			w.handle(nw, ev)
		case err, ok := <-nw.Errors:
			if !ok {
				return
			}
			w.logf("fswatch: %v", err)
		}
	}
}

// handle maps one fsnotify event to an fs.Event and publishes it on the bus for the
// event's host path, stamped Origin:"fsnotify". A created directory is added to the
// watch so the recursive coverage follows new subtrees.
func (w *Watcher) handle(nw *fsnotify.Watcher, ev fsnotify.Event) {
	op, ok := mapOp(ev.Op)
	if !ok {
		return // a no-op event class (e.g. Chmod-only on some platforms)
	}
	if op == fs.OpCreate {
		if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
			if addErr := nw.Add(ev.Name); addErr != nil {
				w.logf("fswatch: add new dir %q: %v", ev.Name, addErr)
			}
		}
	}
	b := w.busFor(ev.Name)
	if b == nil {
		return // no share holds this path's bus (shouldn't happen for a watched root)
	}
	fs.OriginBus(b, fs.OriginFSNotify).Publish(fs.Event{Op: op, HostPath: ev.Name, Time: time.Now()})
}

// mapOp maps an fsnotify op set to a single fs.Op. fsnotify coalesces flags; the
// strongest mutation wins (Remove > Rename > Create > Write), so a combined
// create+write reports a create (the §10d reactor is coarse — the client re-reads).
// A Chmod-only event maps to OpAttrChange.
func mapOp(op fsnotify.Op) (fs.Op, bool) {
	switch {
	case op&fsnotify.Remove != 0:
		return fs.OpDelete, true
	case op&fsnotify.Rename != 0:
		return fs.OpRename, true
	case op&fsnotify.Create != 0:
		return fs.OpCreate, true
	case op&fsnotify.Write != 0:
		return fs.OpModify, true
	case op&fsnotify.Chmod != 0:
		return fs.OpAttrChange, true
	default:
		return 0, false
	}
}

func (w *Watcher) logf(format string, args ...any) {
	if w.logger != nil {
		w.logger.Logf(format, args...)
	}
}

// compile-time assertion: the watcher is a lifecycle component.
var _ component.Component = (*Watcher)(nil)
