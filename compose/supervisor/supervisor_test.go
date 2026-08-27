package supervisor

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

// recordingComponent appends its name to a shared log on Start/Stop so tests can assert order.
type recordingComponent struct {
	name string
	log  *orderLog
}

func (c *recordingComponent) Name() string { return c.name }
func (c *recordingComponent) Start(context.Context) error {
	c.log.add("start:" + c.name)
	return nil
}
func (c *recordingComponent) Stop(context.Context) error {
	c.log.add("stop:" + c.name)
	return nil
}

type orderLog struct {
	mu  sync.Mutex
	seq []string
}

func (l *orderLog) add(s string) {
	l.mu.Lock()
	l.seq = append(l.seq, s)
	l.mu.Unlock()
}

func TestStartStopOrdering(t *testing.T) {
	log := &orderLog{}
	s := New(config.NewModel(), nil)

	// DAG: router depends on port; afp depends on router. Expect start port->router->afp.
	s.Add(&recordingComponent{name: "port", log: log}, nil)
	s.Add(&recordingComponent{name: "router", log: log}, []string{"port"})
	s.Add(&recordingComponent{name: "afp", log: log}, []string{"router"})

	if err := s.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	wantStart := []string{"start:port", "start:router", "start:afp"}
	if !reflect.DeepEqual(log.seq, wantStart) {
		t.Fatalf("start order = %v, want %v", log.seq, wantStart)
	}

	log.seq = nil
	if err := s.StopAll(context.Background()); err != nil {
		t.Fatalf("StopAll: %v", err)
	}
	wantStop := []string{"stop:afp", "stop:router", "stop:port"}
	if !reflect.DeepEqual(log.seq, wantStop) {
		t.Fatalf("stop order = %v, want %v", log.seq, wantStop)
	}
}

func TestStateChangedPublished(t *testing.T) {
	telemetry := bus.New(16)
	ch, cancel := telemetry.Subscribe(bus.TopicState)
	defer cancel()

	s := New(config.NewModel(), telemetry)
	s.Add(&recordingComponent{name: "port", log: &orderLog{}}, nil)

	if err := s.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	ev := (<-ch).(bus.StateChanged)
	if ev.Component != "port" || ev.From != stateStopped || ev.To != stateRunning {
		t.Fatalf("start transition = %+v, want port stopped->running", ev)
	}

	if err := s.StopAll(context.Background()); err != nil {
		t.Fatalf("StopAll: %v", err)
	}
	ev = (<-ch).(bus.StateChanged)
	if ev.Component != "port" || ev.From != stateRunning || ev.To != stateStopped {
		t.Fatalf("stop transition = %+v, want port running->stopped", ev)
	}
}

type failingComponent struct {
	name string
	err  error
}

func (c *failingComponent) Name() string                { return c.name }
func (c *failingComponent) Start(context.Context) error { return c.err }
func (c *failingComponent) Stop(context.Context) error  { return nil }

func TestStartAllContinuesAfterFailure(t *testing.T) {
	log := &orderLog{}
	s := New(config.NewModel(), nil)
	s.Add(&failingComponent{name: "port", err: errors.New("no such device")}, nil)
	s.Add(&recordingComponent{name: "router", log: log}, []string{"port"})

	if err := s.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	if got := log.seq; len(got) != 1 || got[0] != "start:router" {
		t.Fatalf("continued start = %v, want [start:router]", got)
	}
	var portErr string
	for _, u := range s.Status() {
		if u.Name == "port" {
			portErr = u.Error
			if u.Running {
				t.Fatal("failed port reported Running")
			}
		}
	}
	if portErr == "" {
		t.Fatal("failed port Status.Error empty")
	}
}

func TestStartIdempotent(t *testing.T) {
	log := &orderLog{}
	s := New(config.NewModel(), nil)
	s.Add(&recordingComponent{name: "port", log: log}, nil)

	if err := s.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	if err := s.StartAll(context.Background()); err != nil {
		t.Fatalf("second StartAll: %v", err)
	}
	// Only one Start should have fired (idempotent).
	if got := len(log.seq); got != 1 {
		t.Fatalf("expected 1 start, got %d (%v)", got, log.seq)
	}
}

func TestPerNameStartBringsUpDeps(t *testing.T) {
	log := &orderLog{}
	s := New(config.NewModel(), nil)
	s.Add(&recordingComponent{name: "port", log: log}, nil)
	s.Add(&recordingComponent{name: "router", log: log}, []string{"port"})
	s.Add(&recordingComponent{name: "afp", log: log}, []string{"router"})

	// Starting afp alone must bring up port then router first.
	if err := s.Start(context.Background(), "afp"); err != nil {
		t.Fatalf("Start(afp): %v", err)
	}
	want := []string{"start:port", "start:router", "start:afp"}
	if !reflect.DeepEqual(log.seq, want) {
		t.Fatalf("per-name start order = %v, want %v", log.seq, want)
	}
}

func TestPerNameStopTakesDownDependents(t *testing.T) {
	log := &orderLog{}
	s := New(config.NewModel(), nil)
	s.Add(&recordingComponent{name: "port", log: log}, nil)
	s.Add(&recordingComponent{name: "router", log: log}, []string{"port"})
	s.Add(&recordingComponent{name: "afp", log: log}, []string{"router"})
	if err := s.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	log.seq = nil

	// Stopping port must take down its dependents (afp, router) first.
	if err := s.Stop(context.Background(), "port"); err != nil {
		t.Fatalf("Stop(port): %v", err)
	}
	want := []string{"stop:afp", "stop:router", "stop:port"}
	if !reflect.DeepEqual(log.seq, want) {
		t.Fatalf("per-name stop order = %v, want %v", log.seq, want)
	}
}

// stuckComponent's Stop ignores ctx and blocks until the test unblocks it — modeling
// the real components (afp, smb, ncp, macip, browser, ...) whose Stop implementations
// discard the passed context and rely purely on an internal close-channel/WaitGroup.
type stuckComponent struct {
	name    string
	release chan struct{}
}

func (c *stuckComponent) Name() string                { return c.name }
func (c *stuckComponent) Start(context.Context) error { return nil }
func (c *stuckComponent) Stop(context.Context) error {
	<-c.release
	return nil
}

// TestStopAllHonoursDeadlineDespiteStuckComponent guards against a regression of the
// Ctrl-C/SIGTERM "doesn't stop" bug: one component's Stop ignoring ctx must not hang
// StopAll (and everything queued behind it) past the caller's deadline.
func TestStopAllHonoursDeadlineDespiteStuckComponent(t *testing.T) {
	s := New(config.NewModel(), nil)
	log := &orderLog{}
	stuck := &stuckComponent{name: "stuck", release: make(chan struct{})}
	defer close(stuck.release) // let the leaked goroutine's Stop return so it doesn't leak past the test

	s.Add(stuck, nil)
	s.Add(&recordingComponent{name: "afterward", log: log}, []string{"stuck"})

	if err := s.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- s.StopAll(ctx) }()

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("StopAll error = %v, want context.DeadlineExceeded", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("StopAll did not return within its own deadline — a stuck component's Stop blocked it")
	}
}

// TestStuckComponentDoesNotFailTheRest guards the shutdown-budget cascade seen in a
// real log: TashTalk's Stop hung, ate the whole 5s budget, and every one of the 15
// components behind it in the teardown order was then handed an already-expired
// context and logged as "did not stop before deadline" — 30 error lines for one
// fault, with nothing to say which component was actually to blame. Each component
// gets its own share of the budget, so a healthy component after a stuck one still
// stops normally.
func TestStuckComponentDoesNotFailTheRest(t *testing.T) {
	s := New(config.NewModel(), nil)
	lg := &orderLog{}
	stuck := &stuckComponent{name: "stuck", release: make(chan struct{})}
	defer close(stuck.release)

	// stuck depends on healthy, so reverse-dependency teardown stops stuck FIRST and
	// healthy second — healthy is behind the component that overruns.
	s.Add(&recordingComponent{name: "healthy", log: lg}, nil)
	s.Add(stuck, []string{"healthy"})

	if err := s.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	// A budget the stuck component alone will exhaust.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- s.StopAll(ctx) }()

	select {
	case err := <-done:
		// The stuck component is still reported — the fault is not swallowed.
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("StopAll error = %v, want context.DeadlineExceeded from the stuck component", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("StopAll did not return")
	}

	lg.mu.Lock()
	seq := append([]string(nil), lg.seq...)
	lg.mu.Unlock()
	if !slices.Contains(seq, "stop:healthy") {
		t.Fatalf("component after the stuck one never stopped; log = %v", seq)
	}
}

// TestStopShare pins the budget split: an even share of the time left, clamped so a
// component is never given less than minStopGrace (the cascade guard) nor more than
// maxStopGrace (so a two-component order does not wait a long time on the first).
func TestStopShare(t *testing.T) {
	cases := []struct {
		name      string
		budget    time.Duration
		remaining int
		want      time.Duration
	}{
		{"even split", 4 * time.Second, 4, time.Second},
		{"clamped to floor when budget is spent", 0, 8, minStopGrace},
		{"clamped to floor when share is tiny", time.Second, 100, minStopGrace},
		{"clamped to ceiling", time.Minute, 2, maxStopGrace},
		{"last component takes what is left", 500 * time.Millisecond, 1, 500 * time.Millisecond},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), tc.budget)
			defer cancel()
			got := stopShare(ctx, tc.remaining)
			// time.Until loses a sliver between the WithTimeout and the call.
			if d := tc.want - got; d < 0 || d > 20*time.Millisecond {
				t.Fatalf("stopShare(budget=%v, remaining=%d) = %v, want ~%v", tc.budget, tc.remaining, got, tc.want)
			}
		})
	}

	// No deadline must still bound the wait: StopAll runs on the way out of the
	// process, and a component that will not stop cannot be allowed to hold it open.
	if got := stopShare(context.Background(), 3); got != maxStopGrace {
		t.Fatalf("stopShare(no deadline) = %v, want %v", got, maxStopGrace)
	}
}

func TestCycleDetected(t *testing.T) {
	s := New(config.NewModel(), nil)
	s.Add(&recordingComponent{name: "a", log: &orderLog{}}, []string{"b"})
	s.Add(&recordingComponent{name: "b", log: &orderLog{}}, []string{"a"})
	if err := s.StartAll(context.Background()); err == nil {
		t.Fatalf("expected cycle error, got nil")
	}
}
