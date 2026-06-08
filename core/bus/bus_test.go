package bus

import (
	"testing"
	"time"
)

func TestTopicScoping(t *testing.T) {
	b := New(8)
	ch, unsub := b.Subscribe(TopicState)
	defer unsub()

	// A non-requested topic must never be enqueued.
	b.Publish(StatSample{Component: "x"})                  // topic "stats" — not requested
	b.Publish(StateChanged{Component: "x", To: "running"}) // topic "state" — requested

	select {
	case ev := <-ch:
		sc, ok := ev.(StateChanged)
		if !ok {
			t.Fatalf("expected StateChanged, got %T", ev)
		}
		if sc.To != "running" {
			t.Fatalf("unexpected event: %+v", sc)
		}
	case <-time.After(time.Second):
		t.Fatal("expected a state event")
	}

	// Nothing else should be queued (the stats event was filtered out).
	select {
	case ev := <-ch:
		t.Fatalf("unexpected extra event: %T %+v", ev, ev)
	default:
	}
}

func TestSubscribeAllTopics(t *testing.T) {
	b := New(8)
	ch, unsub := b.Subscribe() // no topics → all
	defer unsub()

	b.Publish(StateChanged{To: "a"})
	b.Publish(StatSample{Component: "b"})

	got := 0
	for got < 2 {
		select {
		case <-ch:
			got++
		case <-time.After(time.Second):
			t.Fatalf("expected 2 events, got %d", got)
		}
	}
}

func TestDropToleranceNeverBlocksPublisher(t *testing.T) {
	b := New(1) // tiny buffer
	_, unsub := b.Subscribe(TopicState)
	defer unsub()

	// Publish far more than the buffer can hold. A slow subscriber (we never read)
	// must cause drops, NOT block the publisher. If Publish blocked, this test hangs
	// and the -timeout fires.
	done := make(chan struct{})
	go func() {
		for range 1000 {
			b.Publish(StateChanged{To: "x"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a full subscriber (drop-tolerance violated)")
	}
}

func TestUnsubscribeStopsDeliveryAndIsIdempotent(t *testing.T) {
	b := New(8)
	ch, unsub := b.Subscribe(TopicState)

	unsub()
	unsub() // idempotent: must not panic / double-close

	// After unsubscribe the channel is closed; publishing must not panic and must
	// not deliver.
	b.Publish(StateChanged{To: "x"})
	if _, open := <-ch; open {
		t.Fatal("expected closed channel after unsubscribe")
	}
}

// TestNoAllocOnUnrequestedTopic asserts the §1 promise: an event whose topic was
// not requested causes no allocation in Publish (no enqueue, no wakeup).
func TestNoAllocOnUnrequestedTopic(t *testing.T) {
	b := New(8)
	_, unsub := b.Subscribe(TopicState)
	defer unsub()

	var ev Event = StatSample{Component: "x"} // pre-boxed; topic "stats", never requested
	allocs := testing.AllocsPerRun(100, func() {
		b.Publish(ev)
	})
	if allocs != 0 {
		t.Fatalf("publishing an unrequested-topic event allocated %v (want 0)", allocs)
	}
}
