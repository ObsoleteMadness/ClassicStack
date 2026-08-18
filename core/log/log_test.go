package log

import (
	"os"
	"strings"
	"testing"
)

type collectSink struct {
	min  Level
	recs []Record
}

func (s *collectSink) Write(rec Record) {
	copyRec := rec
	copyRec.Fields = append([]Field(nil), rec.Fields...)
	s.recs = append(s.recs, copyRec)
}

func (s *collectSink) Min() Level   { return s.min }
func (s *collectSink) Close() error { return nil }

type lastSink struct {
	min   Level
	count int
	last  Record
}

func (s *lastSink) Write(rec Record) {
	s.count++
	s.last = rec
}

func (s *lastSink) Min() Level   { return s.min }
func (s *lastSink) Close() error { return nil }

func TestWithScopesAndFields(t *testing.T) {
	s := &collectSink{min: Info}
	root := New("afp", s)
	child := root.With(Str("volume", "docs"))

	root.Log1(Info, "root", Int("count", 1))
	child.Log1(Info, "child", Bool("ok", true))

	if len(s.recs) != 2 {
		t.Fatalf("records = %d, want 2", len(s.recs))
	}
	if s.recs[0].Scope != "afp" || s.recs[1].Scope != "afp" {
		t.Fatalf("scopes = %q,%q, want afp,afp", s.recs[0].Scope, s.recs[1].Scope)
	}
	if len(s.recs[0].Fields) != 1 {
		t.Fatalf("root fields=%d, want 1", len(s.recs[0].Fields))
	}
	if len(s.recs[1].Fields) != 2 {
		t.Fatalf("child fields=%d, want 2 (bound+call)", len(s.recs[1].Fields))
	}
	if got, want := s.recs[1].Fields[0].Key, "volume"; got != want {
		t.Fatalf("child bound field key=%q, want %q", got, want)
	}
}

func TestFanOutTwoSinks(t *testing.T) {
	a := &collectSink{min: Debug}
	b := &collectSink{min: Debug}
	l := New("router", a, b)

	l.Log0(Info, "started")

	if len(a.recs) != 1 || len(b.recs) != 1 {
		t.Fatalf("fan-out counts: a=%d b=%d, want 1 each", len(a.recs), len(b.recs))
	}
	if a.recs[0].Msg != "started" || b.recs[0].Msg != "started" {
		t.Fatalf("messages differ: a=%q b=%q", a.recs[0].Msg, b.recs[0].Msg)
	}
}

func TestEnabledFastPathNoAllocWhenDisabled(t *testing.T) {
	l := New("svc", &collectSink{min: Warn})
	if l.Enabled(Debug) {
		t.Fatal("Enabled(Debug)=true, want false for Warn min")
	}
	allocs := testing.AllocsPerRun(1000, func() {
		l.Log(Debug, "debug-disabled", Str("k", "v"))
	})
	if allocs != 0 {
		t.Fatalf("disabled Log allocs=%v, want 0", allocs)
	}
}

func TestFixedArityNoAllocWhenEnabled(t *testing.T) {
	sink := &lastSink{min: Debug}
	l := New("svc", sink)

	a0 := testing.AllocsPerRun(1000, func() {
		l.Log0(Info, "m")
	})
	a1 := testing.AllocsPerRun(1000, func() {
		l.Log1(Info, "m", Int("n", 1))
	})
	a2 := testing.AllocsPerRun(1000, func() {
		l.Log2(Info, "m", Str("a", "b"), Bool("ok", true))
	})

	if a0 != 0 || a1 != 0 || a2 != 0 {
		t.Fatalf("fixed-arity allocs: Log0=%v Log1=%v Log2=%v; want all 0", a0, a1, a2)
	}
	if sink.count == 0 {
		t.Fatal("expected sink to receive records")
	}
}

func TestRingSinkKeepsTail(t *testing.T) {
	rs, ok := NewRingSink(2, nil).(*ringSink)
	if !ok {
		t.Fatal("NewRingSink did not return *ringSink")
	}

	rs.Write(Record{Msg: "a"})
	rs.Write(Record{Msg: "b"})
	rs.Write(Record{Msg: "c"})

	recs := rs.records()
	if len(recs) != 2 {
		t.Fatalf("records=%d, want 2", len(recs))
	}
	if recs[0].Msg != "b" || recs[1].Msg != "c" {
		t.Fatalf("tail msgs=%q,%q, want b,c", recs[0].Msg, recs[1].Msg)
	}
}

// TestLevelVarRetunesLive proves the threshold lives at the sink and can be raised/lowered at
// runtime (§6b: a UI setting "AFP=debug" later) without rebuilding the logger or sink.
func TestLevelVarRetunesLive(t *testing.T) {
	lv := NewLevelVar(Info)
	rs := NewRingSink(8, lv).(*ringSink)
	l := New("afp", rs)

	if l.Enabled(Debug) {
		t.Fatal("Enabled(Debug)=true at Info threshold, want false")
	}
	l.Log0(Debug, "dropped")
	if got := len(rs.records()); got != 0 {
		t.Fatalf("records at Info=%d, want 0 (debug dropped)", got)
	}

	lv.Set(Debug) // operator turns AFP up to debug at runtime
	if !l.Enabled(Debug) {
		t.Fatal("Enabled(Debug)=false after Set(Debug), want true")
	}
	l.Log0(Debug, "kept")
	recs := rs.records()
	if len(recs) != 1 || recs[0].Msg != "kept" {
		t.Fatalf("records after retune=%v, want [kept]", recs)
	}
}

// TestPerSinkThresholds proves one logger can feed a debug ring and an info-only stderr-style
// sink at once: the debug record reaches only the lower-threshold sink.
func TestPerSinkThresholds(t *testing.T) {
	debugSink := &collectSink{min: Debug}
	infoSink := &collectSink{min: Info}
	l := New("router", debugSink, infoSink)

	l.Log0(Debug, "trace")
	l.Log0(Info, "up")

	if len(debugSink.recs) != 2 {
		t.Fatalf("debug sink recs=%d, want 2", len(debugSink.recs))
	}
	if len(infoSink.recs) != 1 || infoSink.recs[0].Msg != "up" {
		t.Fatalf("info sink recs=%v, want [up]", infoSink.recs)
	}
}

func TestFileSink(t *testing.T) {
	path := t.TempDir() + "/client.log"
	sink, err := NewFileSink(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	l := New("client", sink)
	l.Log1(Info, "scan", Str("scheme", "afp"))
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.Contains(got, "client [info] scan scheme=\"afp\"") {
		t.Fatalf("log file = %q", got)
	}
}
