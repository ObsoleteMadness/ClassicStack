package vfs

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBusFanOutAndOriginFilter(t *testing.T) {
	bus := NewBus(BusOptions{})

	var (
		mu        sync.Mutex
		seenSMB   []Event
		seenOther []Event
	)

	cancelSMB := bus.Subscribe(SubscriberFunc(func(ev Event) {
		// SMB subscriber filters out its own events.
		if ev.Origin == "smb" {
			return
		}
		mu.Lock()
		seenSMB = append(seenSMB, ev)
		mu.Unlock()
	}))
	defer cancelSMB()

	cancelOther := bus.Subscribe(SubscriberFunc(func(ev Event) {
		mu.Lock()
		seenOther = append(seenOther, ev)
		mu.Unlock()
	}))
	defer cancelOther()

	bus.Publish(Event{Op: OpRename, HostPath: "/a", OldPath: "/b", Origin: "smb"})
	bus.Publish(Event{Op: OpCreate, HostPath: "/c", Origin: "afp"})

	// Allow async dispatch goroutines to drain.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		ok := len(seenOther) == 2 && len(seenSMB) == 1
		mu.Unlock()
		if ok {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seenOther) != 2 {
		t.Fatalf("Other subscriber: got %d events, want 2", len(seenOther))
	}
	if len(seenSMB) != 1 || seenSMB[0].Origin != "afp" {
		t.Fatalf("SMB subscriber filtered wrong: %#v", seenSMB)
	}
}

func TestBusDropsOnSlowSubscriber(t *testing.T) {
	var dropMsgs atomic.Int32
	bus := NewBus(BusOptions{
		SubscriberBuffer: 2,
		DropWarnInterval: time.Millisecond,
		DropLogger:       func(string) { dropMsgs.Add(1) },
	})

	block := make(chan struct{})
	cancel := bus.Subscribe(SubscriberFunc(func(ev Event) {
		<-block // Pin the dispatch goroutine until we release.
	}))
	defer func() {
		close(block)
		cancel()
	}()

	// 1 event is in-flight (held by our blocked handler), 2 fit in the
	// buffer, the rest must be dropped without blocking Publish.
	for range 50 {
		bus.Publish(Event{Op: OpModify, HostPath: "/x", Origin: "test"})
	}

	// At least one drop warning should have surfaced via DropLogger.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if dropMsgs.Load() > 0 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("expected at least one drop warning, got none")
}
