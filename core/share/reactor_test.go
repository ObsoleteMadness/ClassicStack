package share

import (
	"sync"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// recordingSink captures the (share, event) pairs the reactor delivers.
type recordingSink struct {
	mu   sync.Mutex
	hits []string // "share:op" per delivery
}

func (r *recordingSink) notify(share string, ev fs.Event) {
	r.mu.Lock()
	r.hits = append(r.hits, share+":"+ev.Op.String())
	r.mu.Unlock()
}

func (r *recordingSink) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.hits...)
}

// waitFor polls until pred() or the deadline (the reactor delivers asynchronously).
func waitFor(t *testing.T, pred func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if pred() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition not met before deadline")
}

// TestReactorSkipsOwnOriginDeliversForeign: the reactor ignores an event it
// originated and delivers one from another origin that falls under a share root.
func TestReactorSkipsOwnOriginDeliversForeign(t *testing.T) {
	sink := &recordingSink{}
	r := NewReactor("smb", func() []NamedPath {
		return []NamedPath{{Name: "Public", Root: "/srv/public"}}
	}, sink.notify)
	b := fs.NewBus(8)
	r.Subscribe(b)
	defer r.Stop()

	// Our own event (origin smb) — must be skipped.
	b.Publish(fs.Event{Op: fs.OpModify, HostPath: "/srv/public/x", Origin: "smb"})
	// A foreign event (origin afp) under our share — must be delivered.
	b.Publish(fs.Event{Op: fs.OpCreate, HostPath: "/srv/public/y", Origin: "afp"})

	waitFor(t, func() bool { return r.Delivered() == 1 })
	if got := sink.snapshot(); len(got) != 1 || got[0] != "Public:create" {
		t.Fatalf("deliveries = %v, want [Public:create]", got)
	}
}

// TestReactorIgnoresEventsOutsideRoots: a foreign event whose host path is under no
// share root is not delivered.
func TestReactorIgnoresEventsOutsideRoots(t *testing.T) {
	sink := &recordingSink{}
	r := NewReactor("afp", func() []NamedPath {
		return []NamedPath{{Name: "Vol", Root: "/srv/vol"}}
	}, sink.notify)
	b := fs.NewBus(8)
	r.Subscribe(b)
	defer r.Stop()

	b.Publish(fs.Event{Op: fs.OpDelete, HostPath: "/elsewhere/z", Origin: "smb"})
	// Give the goroutine a moment; nothing should land.
	time.Sleep(50 * time.Millisecond)
	if r.Delivered() != 0 {
		t.Fatalf("delivered %d, want 0 for an out-of-root event", r.Delivered())
	}
}

// TestReactorRenameMatchesEitherEnd: a rename is delivered if EITHER the new or old
// host path falls under a share root (a move out of / into the share both matter).
func TestReactorRenameMatchesEitherEnd(t *testing.T) {
	sink := &recordingSink{}
	r := NewReactor("afp", func() []NamedPath {
		return []NamedPath{{Name: "Vol", Root: "/srv/vol"}}
	}, sink.notify)
	b := fs.NewBus(8)
	r.Subscribe(b)
	defer r.Stop()

	// OldPath under the root, new path elsewhere (a move OUT) — delivered.
	b.Publish(fs.Event{Op: fs.OpRename, OldPath: "/srv/vol/a", HostPath: "/tmp/a", Origin: "smb"})
	waitFor(t, func() bool { return r.Delivered() == 1 })
}

// TestReactorMultipleSharesSamePath: when two shares share a host root, a single
// foreign event is delivered once per matching share (each gets its own notify).
func TestReactorMultipleSharesSamePath(t *testing.T) {
	sink := &recordingSink{}
	r := NewReactor("afp", func() []NamedPath {
		return []NamedPath{{Name: "A", Root: "/srv/shared"}, {Name: "B", Root: "/srv/shared"}}
	}, sink.notify)
	b := fs.NewBus(8)
	r.Subscribe(b)
	defer r.Stop()

	b.Publish(fs.Event{Op: fs.OpModify, HostPath: "/srv/shared/file", Origin: "smb"})
	waitFor(t, func() bool { return r.Delivered() == 2 })
	got := sink.snapshot()
	if len(got) != 2 {
		t.Fatalf("deliveries = %v, want 2 (one per matching share)", got)
	}
}

// TestReactorStopEndsDelivery: after Stop, further events are not delivered.
func TestReactorStopEndsDelivery(t *testing.T) {
	sink := &recordingSink{}
	r := NewReactor("afp", func() []NamedPath {
		return []NamedPath{{Name: "Vol", Root: "/srv/vol"}}
	}, sink.notify)
	b := fs.NewBus(8)
	r.Subscribe(b)

	b.Publish(fs.Event{Op: fs.OpCreate, HostPath: "/srv/vol/a", Origin: "smb"})
	waitFor(t, func() bool { return r.Delivered() == 1 })

	r.Stop()
	// Unsubscribed: the bus no longer enqueues to the cancelled subscription.
	b.Publish(fs.Event{Op: fs.OpCreate, HostPath: "/srv/vol/b", Origin: "smb"})
	time.Sleep(50 * time.Millisecond)
	if r.Delivered() != 1 {
		t.Fatalf("delivered %d after Stop, want 1 (no further delivery)", r.Delivered())
	}
}

// TestReactorNilBusNoOp: subscribing a nil bus is a harmless no-op.
func TestReactorNilBusNoOp(t *testing.T) {
	r := NewReactor("afp", func() []NamedPath { return nil }, nil)
	r.Subscribe(nil)
	r.Stop()
}
