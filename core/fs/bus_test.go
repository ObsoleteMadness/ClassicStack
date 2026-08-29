package fs

import (
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
)

func TestFSBus_PublishesFSMutationTopic(t *testing.T) {
	b := NewBus(2)
	ch, unsub := b.Subscribe(TopicFSMutation)
	defer unsub()

	b.Publish(Event{Op: OpModify, HostPath: "/tmp/x", Origin: "afp", Time: time.Now()})

	select {
	case ev := <-ch:
		fsev, ok := ev.(Event)
		if !ok {
			t.Fatalf("event type = %T, want fs.Event", ev)
		}
		if fsev.Topic() != TopicFSMutation {
			t.Fatalf("Topic() = %q, want %q", fsev.Topic(), TopicFSMutation)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for fs event")
	}
}

func TestSkipOrigin(t *testing.T) {
	if !SkipOrigin(Event{Origin: "afp"}, "afp") {
		t.Fatal("SkipOrigin() = false, want true for same origin")
	}
	if SkipOrigin(Event{Origin: "smb"}, "afp") {
		t.Fatal("SkipOrigin() = true, want false for different origin")
	}
	if SkipOrigin(bus.StateChanged{Component: "x"}, "afp") {
		t.Fatal("SkipOrigin() = true, want false for non-fs event")
	}
}
