//go:build fswatch || all

package fswatch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// TestMapOp checks the fsnotify-op → fs.Op precedence (strongest mutation wins).
func TestMapOp(t *testing.T) {
	cases := []struct {
		in   fsnotify.Op
		want fs.Op
		ok   bool
	}{
		{fsnotify.Create, fs.OpCreate, true},
		{fsnotify.Write, fs.OpModify, true},
		{fsnotify.Remove, fs.OpDelete, true},
		{fsnotify.Rename, fs.OpRename, true},
		{fsnotify.Chmod, fs.OpAttrChange, true},
		{fsnotify.Create | fsnotify.Write, fs.OpCreate, true}, // create+write → create
		{fsnotify.Remove | fsnotify.Write, fs.OpDelete, true}, // remove wins
		{0, 0, false},
	}
	for _, c := range cases {
		got, ok := mapOp(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("mapOp(%v) = (%v,%v), want (%v,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

// TestWatcherPublishesOnRealChange drives a real fsnotify event: writing a file under
// a watched root produces an fs.Event on the path's bus, stamped Origin:"fsnotify".
func TestWatcherPublishesOnRealChange(t *testing.T) {
	root := t.TempDir()
	b := fs.NewBus(16)
	ch, unsub := b.Subscribe(fs.TopicFSMutation)
	defer unsub()

	// One bus for every path (the test's whole tree shares the root's bus).
	w := New(nil, func(string) bus.Bus { return b }, []string{root})
	if err := w.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer w.Stop(context.Background())

	// Create a file under the watched root.
	target := filepath.Join(root, "hello.txt")
	if err := os.WriteFile(target, []byte("hi"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ev := awaitEvent(t, ch)
	if ev.Origin != fs.OriginFSNotify {
		t.Errorf("Origin = %q, want %q", ev.Origin, fs.OriginFSNotify)
	}
	if ev.HostPath != target {
		t.Errorf("HostPath = %q, want %q", ev.HostPath, target)
	}
	if ev.Op != fs.OpCreate && ev.Op != fs.OpModify {
		t.Errorf("Op = %v, want create or modify", ev.Op)
	}
}

// TestWatcherStopIsClean: Stop before any event, and Start/Stop idempotency.
func TestWatcherStopIsClean(t *testing.T) {
	root := t.TempDir()
	w := New(nil, func(string) bus.Bus { return fs.NewBus(1) }, []string{root})
	ctx := context.Background()
	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := w.Start(ctx); err != nil {
		t.Fatalf("second Start (idempotent): %v", err)
	}
	if err := w.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := w.Stop(ctx); err != nil {
		t.Fatalf("second Stop (idempotent): %v", err)
	}
}

// TestWatcherMissingRootSkipped: a non-existent root is skipped, not fatal.
func TestWatcherMissingRootSkipped(t *testing.T) {
	w := New(nil, func(string) bus.Bus { return fs.NewBus(1) }, []string{"/no/such/dir/at/all"})
	if err := w.Start(context.Background()); err != nil {
		t.Fatalf("Start with missing root should not error: %v", err)
	}
	_ = w.Stop(context.Background())
}

// awaitEvent waits for one fs.Event (the OS watcher may coalesce/emit more than one;
// take the first that names our file).
func awaitEvent(t *testing.T, ch <-chan bus.Event) fs.Event {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case e := <-ch:
			if fe, ok := e.(fs.Event); ok {
				return fe
			}
		case <-deadline:
			t.Fatal("timed out waiting for a watcher fs.Event")
			return fs.Event{}
		}
	}
}
