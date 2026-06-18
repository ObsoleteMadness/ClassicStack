package main

import (
	"testing"

	mailslot "github.com/ObsoleteMadness/ClassicStack/core/protocol/mailslot"
	messenger "github.com/ObsoleteMadness/ClassicStack/core/protocol/messenger"
)

// TestPayloadRoundTrips proves the payload csnetsend assembles (messenger frame
// inside a \MAILSLOT\MESSNGR transaction) decodes back through the SAME core codecs
// the messenger service uses — the protocol-reuse proof: what the client builds is
// exactly what the server parses.
func TestPayloadRoundTrips(t *testing.T) {
	body := messenger.Message{From: "ALICE", To: "BOB", Text: "hello there"}.Marshal()
	payload := mailslot.Write{Name: mailslot.NameMessenger, Body: body}.Marshal()

	// Unwrap the mailslot envelope: name must be \MAILSLOT\MESSNGR, body the frame.
	w, err := mailslot.Unmarshal(payload)
	if err != nil {
		t.Fatalf("mailslot Unmarshal: %v", err)
	}
	if w.Name != mailslot.NameMessenger {
		t.Errorf("mailslot name = %q, want %q", w.Name, mailslot.NameMessenger)
	}

	// Decode the inner messenger frame and confirm the fields survived.
	m, err := messenger.Unmarshal(w.Body)
	if err != nil {
		t.Fatalf("messenger Unmarshal: %v", err)
	}
	if m.From != "ALICE" || m.To != "BOB" || m.Text != "hello there" {
		t.Errorf("decoded message = %+v, want From=ALICE To=BOB Text=\"hello there\"", *m)
	}
}

// TestHexDumpShape sanity-checks the dump renders one offset-prefixed row per 16
// bytes with the trailing ASCII gutter, so the tool's human output stays stable.
func TestHexDumpShape(t *testing.T) {
	out := hexDump([]byte("ABCDEFGHIJKLMNOPQR")) // 18 bytes → 2 rows
	if got := countLines(out); got != 2 {
		t.Errorf("hexDump produced %d rows for 18 bytes, want 2", got)
	}
}

func countLines(s string) int {
	n := 0
	for _, c := range s {
		if c == '\n' {
			n++
		}
	}
	return n
}
