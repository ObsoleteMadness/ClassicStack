package protolog

import (
	"bytes"
	"strings"
	"testing"
)

func TestFilterGatesEvents(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	sink := &ConsoleSink{W: &buf, MaxBytes: 0}

	// Only AFP inbound allowed.
	l := New(FilterConfig{"AFP": "in"}.Compile(), sink)

	l.In("AFP", "peer", []byte{1, 2, 3}, nil, nil)
	l.Out("AFP", "peer", []byte{1, 2, 3}, nil)
	l.In("DDP", "peer", []byte{1, 2, 3}, nil, nil)

	got := buf.String()
	if !strings.Contains(got, "AFP") {
		t.Fatalf("expected AFP inbound: %q", got)
	}
	// Only one record should have been written.
	if n := strings.Count(got, "PROTO"); n != 1 {
		t.Fatalf("expected 1 event past filter, got %d: %q", n, got)
	}
}

func TestAllowAllAndDenyAll(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	sink := &ConsoleSink{W: &buf}

	l := New(AllowAll(), sink)
	l.In("X", "p", []byte{0xAB}, nil, nil)
	if buf.Len() == 0 {
		t.Fatal("AllowAll should have written")
	}
	buf.Reset()

	l.SetFilter(DenyAll())
	l.In("X", "p", []byte{0xAB}, nil, nil)
	if buf.Len() != 0 {
		t.Fatalf("DenyAll should have blocked: %q", buf.String())
	}
}

func TestFilterConfigWildcard(t *testing.T) {
	t.Parallel()
	f := FilterConfig{"*": "in+out", "DDP": "off"}.Compile()
	if !f("AFP", DirIn) {
		t.Error("wildcard should admit AFP in")
	}
	if f("DDP", DirIn) {
		t.Error("DDP off should deny")
	}
}

func TestJSONSinkEmitsHex(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	l := New(AllowAll(), &JSONSink{W: &buf})
	l.In("AFP", "peer", []byte{0xDE, 0xAD}, nil, nil)
	out := buf.String()
	if !strings.Contains(out, `"raw_hex":"dead"`) {
		t.Fatalf("expected hex-encoded raw: %q", out)
	}
	if !strings.Contains(out, `"source":"AFP"`) {
		t.Fatalf("expected source field: %q", out)
	}
}
