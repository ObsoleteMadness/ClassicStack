package log

import "testing"

type collectSink struct {
	recs []Record
}

func (s *collectSink) Write(rec Record) {
	copyRec := rec
	copyRec.Fields = append([]Field(nil), rec.Fields...)
	s.recs = append(s.recs, copyRec)
}

func (s *collectSink) Close() error { return nil }

func TestWithScopesAndFields(t *testing.T) {
	s := &collectSink{}
	root := New("afp", Info, s)
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
	a := &collectSink{}
	b := &collectSink{}
	l := New("router", Debug, a, b)

	l.Log0(Info, "started")

	if len(a.recs) != 1 || len(b.recs) != 1 {
		t.Fatalf("fan-out counts: a=%d b=%d, want 1 each", len(a.recs), len(b.recs))
	}
	if a.recs[0].Msg != "started" || b.recs[0].Msg != "started" {
		t.Fatalf("messages differ: a=%q b=%q", a.recs[0].Msg, b.recs[0].Msg)
	}
}

func TestEnabledFastPathNoAllocWhenDisabled(t *testing.T) {
	l := New("svc", Warn)
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
	// No sinks means an enabled hot-path call exits before building any heap-backed slices.
	l := New("svc", Debug)

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
}

func TestRingSinkKeepsTail(t *testing.T) {
	rs, ok := NewRingSink(2).(*ringSink)
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
