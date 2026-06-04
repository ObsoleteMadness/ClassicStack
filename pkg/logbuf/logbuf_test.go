package logbuf

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

func TestSnapshotOrderingAndEviction(t *testing.T) {
	b := New(3)
	for i := range 5 {
		b.Append(Entry{UnixMilli: int64(i), Message: string(rune('a' + i))})
	}
	got := b.Snapshot()
	if len(got) != 3 {
		t.Fatalf("snapshot len = %d, want 3", len(got))
	}
	// Oldest two ("a","b") evicted; expect c, d, e oldest-first.
	want := []string{"c", "d", "e"}
	for i, e := range got {
		if e.Message != want[i] {
			t.Errorf("entry %d = %q, want %q", i, e.Message, want[i])
		}
	}
}

func TestSubscribeReceivesAppended(t *testing.T) {
	b := New(8)
	ch, cancel := b.Subscribe()
	defer cancel()

	b.Append(Entry{Message: "hello"})
	select {
	case e := <-ch:
		if e.Message != "hello" {
			t.Fatalf("got %q, want hello", e.Message)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive entry")
	}
}

func TestSlowSubscriberDoesNotBlock(t *testing.T) {
	b := New(8)
	// Subscribe but never drain; Append must not block once the channel fills.
	_, cancel := b.Subscribe()
	defer cancel()

	done := make(chan struct{})
	go func() {
		for range 1000 {
			b.Append(Entry{Message: "x"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Append blocked on a slow subscriber")
	}
}

func TestCancelUnsubscribes(t *testing.T) {
	b := New(8)
	ch, cancel := b.Subscribe()
	cancel()
	b.Append(Entry{Message: "after-cancel"})
	if _, ok := <-ch; ok {
		t.Fatal("channel should be closed and drained after cancel")
	}
}

func TestHandlerCapturesRecords(t *testing.T) {
	b := New(8)
	h := NewHandler(b, slog.LevelInfo)
	l := slog.New(h).With("source", "AFP")
	l.Info("volume opened", "name", "Public")

	got := b.Snapshot()
	if len(got) != 1 {
		t.Fatalf("snapshot len = %d, want 1", len(got))
	}
	e := got[0]
	if e.Level != slog.LevelInfo.String() {
		t.Errorf("level = %q, want %q", e.Level, slog.LevelInfo.String())
	}
	if want := "[AFP] volume opened name=Public"; e.Message != want {
		t.Errorf("message = %q, want %q", e.Message, want)
	}
}

func TestHandlerRespectsLevel(t *testing.T) {
	b := New(8)
	h := NewHandler(b, slog.LevelWarn)
	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("info should be disabled at warn level")
	}
	l := slog.New(h)
	l.Info("dropped")
	l.Warn("kept")
	got := b.Snapshot()
	if len(got) != 1 || got[0].Message != "kept" {
		t.Fatalf("snapshot = %+v, want only 'kept'", got)
	}
}
