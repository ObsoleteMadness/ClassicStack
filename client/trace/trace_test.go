package trace_test

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/client/trace"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

// recordingSink is a log.Sink that records every record it receives, for
// assertions, with a settable threshold.
type recordingSink struct {
	min    log.Level
	recs   []log.Record
	closed bool
}

func (s *recordingSink) Write(rec log.Record) { s.recs = append(s.recs, rec) }
func (s *recordingSink) Min() log.Level       { return s.min }
func (s *recordingSink) Close() error         { s.closed = true; return nil }

// resetVerbose restores the package-wide verbose state to its quiet default
// after a test, since trace's state (sharedLevel/muted/extraSinks) is a
// process-wide singleton shared across every test in this package.
func resetVerbose(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		trace.SetVerbose(false)
		trace.CloseExtraSinks()
	})
}

// TestSetVerbose_TogglesVerbose checks Verbose() reports SetVerbose's most
// recent setting.
func TestSetVerbose_TogglesVerbose(t *testing.T) {
	resetVerbose(t)
	trace.SetVerbose(true)
	if !trace.Verbose() {
		t.Error("Verbose() = false after SetVerbose(true)")
	}
	trace.SetVerbose(false)
	if trace.Verbose() {
		t.Error("Verbose() = true after SetVerbose(false)")
	}
}

// TestLogger_GatedByVerbose checks a Logger's Trace level is only enabled
// while verbose is on, with no extra sink registered to override it.
func TestLogger_GatedByVerbose(t *testing.T) {
	resetVerbose(t)
	trace.SetVerbose(false)
	l := trace.Logger("test-gated-by-verbose")
	if l.Enabled(log.Trace) {
		t.Error("Trace enabled with verbose off and no extra sink")
	}
	trace.SetVerbose(true)
	if !l.Enabled(log.Trace) {
		t.Error("Trace not enabled with verbose on")
	}
}

// TestSetScope_MutesIndependentlyOfVerbose checks a muted scope stays quiet
// even while verbose is globally on, and an unmuted scope is unaffected.
func TestSetScope_MutesIndependentlyOfVerbose(t *testing.T) {
	resetVerbose(t)
	trace.SetVerbose(true)
	t.Cleanup(func() { trace.SetScope("test-muted-scope", true) })

	trace.SetScope("test-muted-scope", false)
	muted := trace.Logger("test-muted-scope")
	loud := trace.Logger("test-unmuted-scope")

	if muted.Enabled(log.Trace) {
		t.Error("muted scope reports Trace enabled")
	}
	if !loud.Enabled(log.Trace) {
		t.Error("unmuted scope reports Trace disabled while verbose is on")
	}

	trace.SetScope("test-muted-scope", true)
	if !muted.Enabled(log.Trace) {
		t.Error("scope still muted after SetScope(scope, true)")
	}
}

// TestAddSink_ReceivesRecordsIndependentOfVerbose checks the documented
// AddSink contract: an extra sink's own (lower) threshold can capture
// records even while the shared verbose toggle is off.
func TestAddSink_ReceivesRecordsIndependentOfVerbose(t *testing.T) {
	resetVerbose(t)
	trace.SetVerbose(false)

	sink := &recordingSink{min: log.Trace}
	trace.AddSink(sink)

	l := trace.Logger("test-addsink-scope")
	if !l.Enabled(log.Trace) {
		t.Fatal("Trace not enabled with an extra Trace-level sink registered, even though verbose is off")
	}
	l.Log0(log.Trace, "hello")

	if len(sink.recs) != 1 || sink.recs[0].Msg != "hello" {
		t.Fatalf("recordingSink got %+v, want one record with Msg %q", sink.recs, "hello")
	}
}

// TestAddSink_NilIsNoop checks AddSink(nil) doesn't register anything (a nil
// sink would panic on Write otherwise).
func TestAddSink_NilIsNoop(t *testing.T) {
	resetVerbose(t)
	trace.AddSink(nil)
	trace.SetVerbose(true)
	l := trace.Logger("test-addsink-nil-scope")
	l.Log0(log.Trace, "must not panic") // would panic if a nil sink got fanned into
}

// TestCloseExtraSinks_ClosesAndForgets checks CloseExtraSinks closes every
// registered sink and stops fanning future records to it.
func TestCloseExtraSinks_ClosesAndForgets(t *testing.T) {
	resetVerbose(t)
	trace.SetVerbose(false)

	sink := &recordingSink{min: log.Trace}
	trace.AddSink(sink)
	trace.CloseExtraSinks()

	if !sink.closed {
		t.Error("sink not closed by CloseExtraSinks")
	}

	l := trace.Logger("test-close-extra-sinks-scope")
	l.Log0(log.Trace, "should not reach the closed sink")
	if len(sink.recs) != 0 {
		t.Errorf("closed sink received %d records, want 0", len(sink.recs))
	}
}

// TestSetLevel_SetsThreshold checks SetLevel drives the same shared
// threshold SetVerbose does, at an arbitrary level (not just Trace/off).
func TestSetLevel_SetsThreshold(t *testing.T) {
	resetVerbose(t)
	trace.SetLevel(log.Info)
	l := trace.Logger("test-setlevel-scope")
	if l.Enabled(log.Debug) {
		t.Error("Debug enabled after SetLevel(Info)")
	}
	if !l.Enabled(log.Info) {
		t.Error("Info not enabled after SetLevel(Info)")
	}
	if !l.Enabled(log.Warn) {
		t.Error("Warn not enabled after SetLevel(Info)")
	}
}
