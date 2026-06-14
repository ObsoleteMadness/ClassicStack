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

// TestWireLayout pins the on-wire bytes: type 0x01, then three NUL-terminated
// strings. A drift here is a protocol-compat break, not just a refactor.
func TestWireLayout(t *testing.T) {
	got := Message{From: "A", To: "B", Text: "hi"}.Marshal()
	want := []byte{TypeSingleBlock, 'A', 0, 'B', 0, 'h', 'i', 0}
	if string(got) != string(want) {
		t.Errorf("wire = % x, want % x", got, want)
	}
}

// TestRejectsWrongType proves a buffer whose first byte is not the single-block
// type is rejected, not mis-decoded as a multi-block/foreign frame.
func TestRejectsWrongType(t *testing.T) {
	if _, err := Unmarshal([]byte{0xD0, 'X', 0, 'Y', 0}); err == nil {
		t.Error("accepted a non-single-block type byte")
	}
	if _, err := Unmarshal(nil); err == nil {
		t.Error("accepted an empty datagram")
	}
}

// TestRejectsUnterminatedNames proves a From/To without a NUL terminator is
// rejected (the names are mandatory and framed by their terminators).
func TestRejectsUnterminatedNames(t *testing.T) {
	// Type + "ALICE" with no terminator at all.
	if _, err := Unmarshal(append([]byte{TypeSingleBlock}, []byte("ALICE")...)); err == nil {
		t.Error("accepted a datagram with an unterminated From")
	}
}

// TestTolerantTrailingText proves a message whose Text field omits the trailing NUL
// still decodes, the remainder taken as the text (some senders skip it).
func TestTolerantTrailingText(t *testing.T) {
	raw := []byte{TypeSingleBlock, 'A', 0, 'B', 0, 'h', 'e', 'l', 'l', 'o'} // no final NUL
	got, err := Unmarshal(raw)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Text != "hello" {
		t.Errorf("text = %q, want hello", got.Text)
	}
}
