package bus

import (
	"testing"
	"time"

	corebus "github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

// drain subscribes to the log topic and returns the first LogRecord, or fails if
// none arrives promptly. Publish is synchronous (same goroutine) but delivery is
// over a buffered channel, so a short read with a timeout keeps the test robust.
func drainOne(t *testing.T, ch <-chan corebus.Event) corebus.LogRecord {
	t.Helper()
	select {
	case ev := <-ch:
		rec, ok := ev.(corebus.LogRecord)
		if !ok {
			t.Fatalf("event type = %T, want bus.LogRecord", ev)
		}
		return rec
	case <-time.After(time.Second):
		t.Fatal("no LogRecord published")
		return corebus.LogRecord{}
	}
}

func TestSinkPublishesRecord(t *testing.T) {
	b := corebus.New(8)
	ch, cancel := b.Subscribe(corebus.TopicLog)
	defer cancel()

	// A logger feeding the bus sink (no threshold = emit everything).
	logger := log.New("afp", New(b, nil))
	logger.Log2(log.Warn, "login failed", log.Str("user", "alice"), log.Int("code", -5023))

	rec := drainOne(t, ch)
	if rec.Component != "afp" {
		t.Errorf("component = %q, want afp", rec.Component)
	}
	if rec.Level != uint8(log.Warn) {
		t.Errorf("level = %d, want %d", rec.Level, log.Warn)
	}
	if rec.Msg != "login failed" {
		t.Errorf("msg = %q, want %q", rec.Msg, "login failed")
	}
	if len(rec.Fields) != 2 {
		t.Fatalf("fields = %d, want 2", len(rec.Fields))
	}
	if f := rec.Fields[0]; f.Key != "user" || f.Kind != corebus.KindStr || f.Str != "alice" {
		t.Errorf("field0 = %+v, want user=alice (str)", f)
	}
	if f := rec.Fields[1]; f.Key != "code" || f.Kind != corebus.KindInt || f.Int != -5023 {
		t.Errorf("field1 = %+v, want code=-5023 (int)", f)
	}
}

func TestSinkTranslatesAllFieldKinds(t *testing.T) {
	b := corebus.New(8)
	ch, cancel := b.Subscribe(corebus.TopicLog)
	defer cancel()

	logger := log.New("smb", New(b, nil))
	logger.Log(log.Info, "session", log.Str("s", "v"), log.Int("i", 7), log.Bool("ok", true))

	rec := drainOne(t, ch)
	if len(rec.Fields) != 3 {
		t.Fatalf("fields = %d, want 3", len(rec.Fields))
	}
	want := []corebus.Field{
		{Key: "s", Kind: corebus.KindStr, Str: "v"},
		{Key: "i", Kind: corebus.KindInt, Int: 7},
		{Key: "ok", Kind: corebus.KindBool, Bool: true},
	}
	for i, w := range want {
		if rec.Fields[i] != w {
			t.Errorf("field %d = %+v, want %+v", i, rec.Fields[i], w)
		}
	}
}

func TestSinkRespectsThreshold(t *testing.T) {
	b := corebus.New(8)
	ch, cancel := b.Subscribe(corebus.TopicLog)
	defer cancel()

	// Threshold Warn: Info must be dropped by the logger before it reaches the sink
	// (Enabled() folds the sink's Min()), and Warn must pass.
	lv := log.NewLevelVar(log.Warn)
	logger := log.New("ddp", New(b, lv))

	logger.Log0(log.Info, "below threshold")
	logger.Log0(log.Warn, "at threshold")

	rec := drainOne(t, ch)
	if rec.Msg != "at threshold" {
		t.Fatalf("first delivered = %q, want the Warn record (Info should have been dropped)", rec.Msg)
	}
	// No second event should be queued.
	select {
	case ev := <-ch:
		t.Fatalf("unexpected second event: %+v", ev)
	default:
	}
}

func TestSinkThresholdRetunesLive(t *testing.T) {
	b := corebus.New(8)
	ch, cancel := b.Subscribe(corebus.TopicLog)
	defer cancel()

	lv := log.NewLevelVar(log.Warn)
	logger := log.New("zip", New(b, lv))

	logger.Log0(log.Info, "dropped")
	lv.Set(log.Info) // a UI lowers the threshold at runtime (§6b)
	logger.Log0(log.Info, "now passes")

	rec := drainOne(t, ch)
	if rec.Msg != "now passes" {
		t.Fatalf("delivered = %q, want %q after lowering threshold", rec.Msg, "now passes")
	}
}

func TestSinkMinReportsThreshold(t *testing.T) {
	if got := New(corebus.New(1), nil).Min(); got != log.Debug {
		t.Errorf("nil-threshold Min() = %v, want Debug", got)
	}
	if got := New(corebus.New(1), log.NewLevelVar(log.Error)).Min(); got != log.Error {
		t.Errorf("Min() = %v, want Error", got)
	}
}

func TestSinkNilBusIsNoOp(t *testing.T) {
	s := New(nil, nil)
	// Must not panic; a build without a telemetry bus can still install the sink.
	s.Write(log.Record{Scope: "x", Level: log.Info, Msg: "m"})
	if err := s.Close(); err != nil {
		t.Errorf("Close() = %v, want nil", err)
	}
}

// TestSinkOwnsFieldsAfterWrite guards the scratch-buffer aliasing hazard: the
// logger reuses one backing array for a record's Fields across calls, so the sink
// must copy them into the published event. We log twice and confirm the first
// event's fields were not overwritten by the second.
func TestSinkOwnsFieldsAfterWrite(t *testing.T) {
	b := corebus.New(8)
	ch, cancel := b.Subscribe(corebus.TopicLog)
	defer cancel()

	logger := log.New("afp", New(b, nil))
	logger.Log1(log.Info, "first", log.Str("v", "one"))
	logger.Log1(log.Info, "second", log.Str("v", "two"))

	first := drainOne(t, ch)
	second := drainOne(t, ch)
	if first.Fields[0].Str != "one" {
		t.Errorf("first field clobbered: got %q, want one", first.Fields[0].Str)
	}
	if second.Fields[0].Str != "two" {
		t.Errorf("second field = %q, want two", second.Fields[0].Str)
	}
}
