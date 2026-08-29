package fs

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
)

// drainOne waits for one fs.Event on ch (the wrapper forwards to the underlying bus).
func drainOne(t *testing.T, ch <-chan bus.Event) Event {
	t.Helper()
	select {
	case ev := <-ch:
		e, ok := ev.(Event)
		if !ok {
			t.Fatalf("event type = %T, want fs.Event", ev)
		}
		return e
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for fs event")
		return Event{}
	}
}

// TestOriginBusStampsBlankOrigin: an event published with no Origin gets the
// wrapper's origin; one already carrying an origin is left as-is.
func TestOriginBusStampsBlankOrigin(t *testing.T) {
	base := NewBus(4)
	ch, unsub := base.Subscribe(TopicFSMutation)
	defer unsub()

	ob := OriginBus(base, "afp")
	ob.Publish(Event{Op: OpModify, HostPath: "/srv/x"})
	if got := drainOne(t, ch).Origin; got != "afp" {
		t.Fatalf("blank origin not stamped: got %q, want afp", got)
	}

	ob.Publish(Event{Op: OpModify, HostPath: "/srv/y", Origin: "preset"})
	if got := drainOne(t, ch).Origin; got != "preset" {
		t.Fatalf("preset origin overwritten: got %q, want preset", got)
	}
}

// TestOriginBusSharedUnderlying: two wrappers over the SAME base bus with different
// origins both reach a subscriber on the base — the basis for §10d coordination
// (AFP and SMB wrap one shared bus, each sees the other's stamped events).
func TestOriginBusSharedUnderlying(t *testing.T) {
	base := NewBus(8)
	ch, unsub := base.Subscribe(TopicFSMutation)
	defer unsub()

	OriginBus(base, "afp").Publish(Event{Op: OpCreate, HostPath: "/srv/a"})
	OriginBus(base, "smb").Publish(Event{Op: OpDelete, HostPath: "/srv/b"})

	got := map[string]bool{}
	got[drainOne(t, ch).Origin] = true
	got[drainOne(t, ch).Origin] = true
	if !got["afp"] || !got["smb"] {
		t.Fatalf("both wrappers should reach the shared subscriber, saw %v", got)
	}
}

// TestOriginBusNilAndEmpty: a nil bus stays nil; an empty origin returns the bus
// unwrapped (nothing to stamp).
func TestOriginBusNilAndEmpty(t *testing.T) {
	if OriginBus(nil, "afp") != nil {
		t.Fatal("OriginBus(nil, …) should be nil")
	}
	base := NewBus(1)
	if OriginBus(base, "") != base {
		t.Fatal("OriginBus(b, \"\") should return b unwrapped")
	}
}

// TestLocalFSPublishesMutations: local_fs publishes Create/Modify/Rename/Delete on
// its bus, with the absolute host path (and OldPath on rename).
func TestLocalFSPublishesMutations(t *testing.T) {
	root := t.TempDir()
	base := NewBus(16)
	ch, unsub := base.Subscribe(TopicFSMutation)
	defer unsub()

	l, err := newLocalFS(ShareSpec{FSType: "local_fs", Path: root}, base)
	if err != nil {
		t.Fatalf("newLocalFS: %v", err)
	}

	// Create a file, write to it, close → OpCreate then OpModify.
	f, err := l.CreateFile("hello.txt")
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	if ev := drainOne(t, ch); ev.Op != OpCreate {
		t.Fatalf("first event Op = %v, want OpCreate", ev.Op)
	}
	if _, err := f.WriteAt([]byte("hi"), 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	mod := drainOne(t, ch)
	if mod.Op != OpModify {
		t.Fatalf("after write+close Op = %v, want OpModify", mod.Op)
	}
	if mod.HostPath != filepath.Join(root, "hello.txt") {
		t.Fatalf("host path = %q, want %q", mod.HostPath, filepath.Join(root, "hello.txt"))
	}

	// Rename → OpRename with OldPath set.
	if err := l.Rename("hello.txt", "bye.txt"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	rn := drainOne(t, ch)
	if rn.Op != OpRename || rn.OldPath != filepath.Join(root, "hello.txt") || rn.HostPath != filepath.Join(root, "bye.txt") {
		t.Fatalf("rename event = %+v", rn)
	}

	// Remove → OpDelete.
	if err := l.Remove("bye.txt"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if ev := drainOne(t, ch); ev.Op != OpDelete {
		t.Fatalf("after remove Op = %v, want OpDelete", ev.Op)
	}
}

// TestLocalFSReadOnlyOpenIsSilent: opening a file and only reading it publishes no
// OpModify (only a dirtying write/truncate does).
func TestLocalFSReadOnlyOpenIsSilent(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("data"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	base := NewBus(4)
	ch, unsub := base.Subscribe(TopicFSMutation)
	defer unsub()
	l, err := newLocalFS(ShareSpec{FSType: "local_fs", Path: root}, base)
	if err != nil {
		t.Fatalf("newLocalFS: %v", err)
	}

	f, err := l.OpenFile("f.txt", os.O_RDONLY)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	buf := make([]byte, 4)
	if _, err := f.ReadAt(buf, 0); err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case ev := <-ch:
		t.Fatalf("read-only open should publish nothing, got %+v", ev)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestLocalFSNilBusNoPanic: a local_fs with no bus simply doesn't publish.
func TestLocalFSNilBusNoPanic(t *testing.T) {
	root := t.TempDir()
	l, err := newLocalFS(ShareSpec{FSType: "local_fs", Path: root}, nil)
	if err != nil {
		t.Fatalf("newLocalFS: %v", err)
	}
	if err := l.CreateDir("d"); err != nil {
		t.Fatalf("CreateDir with nil bus: %v", err)
	}
}
