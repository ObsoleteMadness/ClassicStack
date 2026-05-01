package telemetry

import (
	"expvar"
	"testing"
)

func TestCounter(t *testing.T) {
	t.Parallel()
	c := NewCounter("test_counter_total")
	c.Inc()
	c.Add(4)
	if c.Value() != 5 {
		t.Fatalf("Value = %d, want 5", c.Value())
	}
	if v := expvar.Get("test_counter_total"); v == nil || v.String() != "5" {
		t.Fatalf("expvar publish mismatch: %v", v)
	}
}

func TestCounterReregistration(t *testing.T) {
	t.Parallel()
	a := NewCounter("test_reregister_total")
	a.Add(3)
	b := NewCounter("test_reregister_total")
	if b.Value() != 3 {
		t.Fatalf("re-registered counter lost state: %d", b.Value())
	}
}

func TestGauge(t *testing.T) {
	t.Parallel()
	g := NewGauge("test_gauge")
	g.Set(10)
	g.Add(-3)
	if g.Value() != 7 {
		t.Fatalf("Value = %d, want 7", g.Value())
	}
}

func TestHistogram(t *testing.T) {
	t.Parallel()
	h := NewHistogram("test_hist")
	h.Observe(1.5)
	h.Observe(2.5)
	s := h.(*expvarHistogram).String()
	if s != `{"count":2,"sum":4}` {
		t.Fatalf("String = %q", s)
	}
}
