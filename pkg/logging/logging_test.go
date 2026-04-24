package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestConsoleHandlerRendersSourcePrefix(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	l := New("AFP", Options{Sinks: []Sink{{Writer: &buf, Format: FormatConsole, Level: slog.LevelInfo}}})
	l.Info("OpenFork", "refnum", 12)

	got := buf.String()
	if !strings.Contains(got, "[AFP]") {
		t.Fatalf("missing source prefix in console output: %q", got)
	}
	if !strings.Contains(got, "OpenFork") {
		t.Fatalf("missing message: %q", got)
	}
	if !strings.Contains(got, "refnum=12") {
		t.Fatalf("missing attr: %q", got)
	}
}

func TestJSONHandlerEmitsSourceAttr(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	l := New("ASP", Options{Sinks: []Sink{{Writer: &buf, Format: FormatJSON, Level: slog.LevelInfo}}})
	l.Info("OpenSess", "sess", "01HF")

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json unmarshal: %v (raw: %q)", err, buf.String())
	}
	if got["source"] != "ASP" {
		t.Fatalf("source: want ASP, got %v", got["source"])
	}
	if got["msg"] != "OpenSess" {
		t.Fatalf("msg: want OpenSess, got %v", got["msg"])
	}
	if got["sess"] != "01HF" {
		t.Fatalf("sess attr missing: %v", got)
	}
}

func TestDualSinkFanout(t *testing.T) {
	t.Parallel()
	var console, jsonBuf bytes.Buffer
	l := New("ZIP", Options{Sinks: []Sink{
		{Writer: &console, Format: FormatConsole, Level: slog.LevelInfo},
		{Writer: &jsonBuf, Format: FormatJSON, Level: slog.LevelInfo},
	}})
	l.Info("hello")

	if !strings.Contains(console.String(), "[ZIP]") {
		t.Errorf("console missing prefix: %q", console.String())
	}
	if !strings.Contains(jsonBuf.String(), `"source":"ZIP"`) {
		t.Errorf("json missing source: %q", jsonBuf.String())
	}
}

func TestLevelFilter(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	l := New("X", Options{Sinks: []Sink{{Writer: &buf, Format: FormatConsole, Level: slog.LevelWarn}}})
	l.Info("quiet")
	l.Warn("loud")
	got := buf.String()
	if strings.Contains(got, "quiet") {
		t.Errorf("info should have been filtered: %q", got)
	}
	if !strings.Contains(got, "loud") {
		t.Errorf("warn should have emitted: %q", got)
	}
}

func TestContextLogger(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	l := New("Router", Options{Sinks: []Sink{{Writer: &buf, Format: FormatConsole, Level: slog.LevelInfo}}})
	ctx := WithContext(context.Background(), l.With("session", "abc"))

	FromContext(ctx).Info("tick")
	got := buf.String()
	if !strings.Contains(got, "session=abc") {
		t.Fatalf("context logger did not carry session: %q", got)
	}
}

func TestChildReplacesSource(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	root := New("AFP", Options{Sinks: []Sink{{Writer: &buf, Format: FormatConsole, Level: slog.LevelInfo}}})
	sub := Child(root, "AFP.Fork")
	sub.Info("open")

	out := buf.String()
	if !strings.Contains(out, "[AFP.Fork]") {
		t.Fatalf("child source missing: %q", out)
	}
}

func TestParseLevel(t *testing.T) {
	t.Parallel()
	cases := map[string]slog.Level{
		"debug":   slog.LevelDebug,
		"info":    slog.LevelInfo,
		"warn":    slog.LevelWarn,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
	}
	for in, want := range cases {
		got, ok := ParseLevel(in)
		if !ok || got != want {
			t.Errorf("ParseLevel(%q) = (%v, %v); want (%v, true)", in, got, ok, want)
		}
	}
	if _, ok := ParseLevel("bogus"); ok {
		t.Errorf("ParseLevel(bogus) should return ok=false")
	}
}
