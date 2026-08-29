package messenger

import "testing"

// TestSingleBlockRoundTrip proves a net-send message Marshals and Unmarshals with
// From/To/Text preserved verbatim.
func TestSingleBlockRoundTrip(t *testing.T) {
	m := Message{From: "ALICE", To: "BOB", Text: "Meet at 5pm"}
	got, err := Unmarshal(m.Marshal())
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.From != "ALICE" || got.To != "BOB" || got.Text != "Meet at 5pm" {
		t.Errorf("round-trip = %+v, want %+v", *got, m)
	}
}

// TestWireLayout pins the on-wire bytes: three NUL-terminated strings and NOTHING
// else — no leading message-type byte. A drift here is a protocol-compat break, not
// just a refactor.
func TestWireLayout(t *testing.T) {
	got := Message{From: "A", To: "B", Text: "hi"}.Marshal()
	want := []byte{'A', 0, 'B', 0, 'h', 'i', 0}
	if string(got) != string(want) {
		t.Errorf("wire = % x, want % x", got, want)
	}
}

// TestCaptureReplay_Win98NetSend decodes the exact mailslot body a real Win98 put on
// the wire for `net send` to the workgroup (spec/captures/nbipx-win98.pcap frame 229,
// SMB Trans Data Count 32 at Data Offset 88). The old codec required a leading 0x01
// type byte and rejected this outright, so no pop-up was ever logged or surfaced.
func TestCaptureReplay_Win98NetSend(t *testing.T) {
	raw := []byte{
		'W', 'I', 'N', '9', '8', 'U', 'S', 'E', 'R', 0,
		'W', 'O', 'R', 'K', 'G', 'R', 'O', 'U', 'P', 0,
		'H', 'E', 'L', 'L', 'O', ' ', 'W', 'O', 'R', 'L', 'D', 0,
	}
	if len(raw) != 32 {
		t.Fatalf("fixture is %d bytes, want the captured 32", len(raw))
	}
	got, err := Unmarshal(raw)
	if err != nil {
		t.Fatalf("Unmarshal of a real Win98 net send: %v", err)
	}
	if got.From != "WIN98USER" || got.To != "WORKGROUP" || got.Text != "HELLO WORLD" {
		t.Errorf("decoded = %+v, want WIN98USER/WORKGROUP/HELLO WORLD", *got)
	}
}

// TestRejectsEmpty proves an empty datagram is rejected rather than decoding to a
// blank pop-up.
func TestRejectsEmpty(t *testing.T) {
	if _, err := Unmarshal(nil); err == nil {
		t.Error("accepted an empty datagram")
	}
}

// TestRejectsUnterminatedNames proves a From/To without a NUL terminator is
// rejected (the names are mandatory and framed by their terminators).
func TestRejectsUnterminatedNames(t *testing.T) {
	// "ALICE" with no terminator at all.
	if _, err := Unmarshal([]byte("ALICE")); err == nil {
		t.Error("accepted a datagram with an unterminated From")
	}
}

// TestTolerantTrailingText proves a message whose Text field omits the trailing NUL
// still decodes, the remainder taken as the text (some senders skip it).
func TestTolerantTrailingText(t *testing.T) {
	raw := []byte{'A', 0, 'B', 0, 'h', 'e', 'l', 'l', 'o'} // no final NUL
	got, err := Unmarshal(raw)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Text != "hello" {
		t.Errorf("text = %q, want hello", got.Text)
	}
}
