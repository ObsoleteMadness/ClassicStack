package finder

import (
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
)

func TestOnServerMessagePublishes(t *testing.T) {
	b := bus.New(8)
	ch, cancel := b.Subscribe(bus.TopicMessage)
	defer cancel()
	s := New(nil, nil)
	s.SetPublisher(b)
	s.onServerMessage("login", "ClassicStack", "Welcome")
	select {
	case ev := <-ch:
		mr, ok := ev.(bus.MessageReceived)
		if !ok {
			t.Fatalf("event is %T, want MessageReceived", ev)
		}
		if mr.Kind != bus.MessageKindAFP || mr.From != "ClassicStack" || mr.Text != "Welcome" {
			t.Fatalf("event = %+v", mr)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for AFP pop-up")
	}
}

func TestOnServerMessageSkipsEmpty(t *testing.T) {
	b := bus.New(8)
	ch, cancel := b.Subscribe(bus.TopicMessage)
	defer cancel()
	s := New(nil, nil)
	s.SetPublisher(b)
	s.onServerMessage("login", "X", "   ")
	select {
	case ev := <-ch:
		t.Fatalf("published %+v for blank text", ev)
	case <-time.After(50 * time.Millisecond):
	}
}
