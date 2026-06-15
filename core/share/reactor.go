package share

import (
	"strings"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// Reactor is the §10d coordination consumer a file service installs on the shared
// FS-mutation bus. When an AFP volume and an SMB share back the same host path they
// share one bus (the compose broker hands them the same instance); a mutation by one
// is published with that service's Origin, and the OTHER service's Reactor delivers
// it as a pending client notification. The Reactor:
//
//   - subscribes to the FS-mutation topic on each bus it is given,
//   - drops events it originated itself (fs.SkipOrigin on the owner's Origin), so a
//     service never reacts to its own writes (no feedback loop),
//   - resolves which of the owner's shares the event's HostPath falls under (a share
//     whose configured Path is a prefix of the mutated host path), and
//   - hands each (share-name, event) to the owner's notify sink.
//
// The notify sink is where protocol change-notify WOULD be emitted (AFP attention /
// SMB CHANGE_NOTIFY). That wire push does not exist yet (the SMB session seam is
// request→response only, and classic AFP has no per-directory push), so this slice
// stops at the resolved, Origin-filtered, share-attributed notification — a clean
// hook a later slice turns into wire frames. Until then the default sink simply
// counts, which is what the tests and diagnostics observe.
type Reactor struct {
	origin string                          // the OWNER's origin; events with this Origin are skipped
	roots  func() []NamedPath              // the owner's current shares as (name, host-root) pairs
	notify func(share string, ev fs.Event) // delivery sink (push deferred); never nil after New

	mu     sync.Mutex
	cancel []func()
	count  uint64 // foreign events delivered to the sink (diagnostics / tests)
}

// NamedPath pairs a share's name with its configured host root, for path matching.
type NamedPath struct {
	Name string
	Root string
}

// NewReactor builds a Reactor for the owning service. origin is the owner's Origin
// (skipped on receive); roots returns the owner's current shares (re-read per event
// so a reconcile is reflected without re-subscribing); notify is the delivery sink
// (nil installs a count-only default).
func NewReactor(origin string, roots func() []NamedPath, notify func(share string, ev fs.Event)) *Reactor {
	r := &Reactor{origin: origin, roots: roots, notify: notify}
	if r.notify == nil {
		r.notify = func(string, fs.Event) {}
	}
	return r
}

// Subscribe attaches the reactor to one FS-mutation bus. A service calls it once per
// distinct bus among its shares (compose hands one bus per host path). Safe to call
// for a nil bus (no-op). Each subscription runs a goroutine until Stop.
func (r *Reactor) Subscribe(b bus.Bus) {
	if b == nil {
		return
	}
	ch, cancel := b.Subscribe(fs.TopicFSMutation)
	r.mu.Lock()
	r.cancel = append(r.cancel, cancel)
	r.mu.Unlock()
	go r.loop(ch)
}

// loop delivers foreign-origin FS events to the sink until the channel closes.
func (r *Reactor) loop(ch <-chan bus.Event) {
	for ev := range ch {
		if fs.SkipOrigin(ev, r.origin) {
			continue // our own mutation — no self-notify
		}
		fe, ok := asFSEvent(ev)
		if !ok {
			continue
		}
		for _, np := range r.roots() {
			if underRoot(fe.HostPath, np.Root) || (fe.OldPath != "" && underRoot(fe.OldPath, np.Root)) {
				r.mu.Lock()
				r.count++
				r.mu.Unlock()
				r.notify(np.Name, fe)
			}
		}
	}
}

// Stop cancels every subscription, ending the reactor goroutines. Idempotent.
func (r *Reactor) Stop() {
	r.mu.Lock()
	cancels := r.cancel
	r.cancel = nil
	r.mu.Unlock()
	for _, c := range cancels {
		c()
	}
}

// Delivered reports how many foreign events the reactor has delivered to the sink
// (diagnostics / tests). It is the observable proof that coordination is occurring
// until the wire-push slice lands.
func (r *Reactor) Delivered() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count
}

// asFSEvent unwraps a bus.Event to an fs.Event (value or pointer form).
func asFSEvent(ev bus.Event) (fs.Event, bool) {
	switch e := ev.(type) {
	case fs.Event:
		return e, true
	case *fs.Event:
		if e == nil {
			return fs.Event{}, false
		}
		return *e, true
	default:
		return fs.Event{}, false
	}
}

// underRoot reports whether hostPath is the root itself or sits beneath it. Both are
// compared case-folded (the broker keys host paths case-insensitively); an empty
// root matches nothing (a pathless backend has no host tree to coordinate on).
func underRoot(hostPath, root string) bool {
	if root == "" || hostPath == "" {
		return false
	}
	h := strings.ToLower(strings.TrimRight(hostPath, `/\`))
	r := strings.ToLower(strings.TrimRight(root, `/\`))
	if h == r {
		return true
	}
	return strings.HasPrefix(h, r+"/") || strings.HasPrefix(h, r+`\`)
}
