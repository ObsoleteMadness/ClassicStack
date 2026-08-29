package macresources

import (
	"bytes"
	"testing"
)

func typ(s string) [4]byte {
	var t [4]byte
	copy(t[:], s)
	return t
}

func sample() []Resource {
	return []Resource{
		{Type: typ("STR "), ID: 128, Name: "Greeting", HasName: true, Data: []byte("\x05Hello")},
		{Type: typ("STR "), ID: 129, Data: []byte("\x03Bye")},
		{Type: typ("CODE"), ID: 1, Attribs: AttrLocked | AttrPreload, Data: []byte{0x60, 0x00, 0x00, 0x10, 0xDE, 0xAD}},
	}
}

func eqResources(a, b []Resource) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Type != b[i].Type || a[i].ID != b[i].ID || a[i].Attribs != b[i].Attribs {
			return false
		}
		if a[i].HasName != b[i].HasName || a[i].Name != b[i].Name {
			return false
		}
		if !bytes.Equal(a[i].Data, b[i].Data) {
			return false
		}
	}
	return true
}

// TestBinaryRoundTrip proves resources survive BuildResourceFork → ParseResourceFork.
func TestBinaryRoundTrip(t *testing.T) {
	in := sample()
	bin := BuildResourceFork(in)
	out, err := ParseResourceFork(bin)
	if err != nil {
		t.Fatalf("ParseResourceFork: %v", err)
	}
	if !eqResources(in, out) {
		t.Fatalf("binary round-trip mismatch:\n in=%+v\nout=%+v", in, out)
	}
}

// TestRezRoundTrip proves resources survive FormatRez → ParseRez (the rdump text form),
// including names, attributes, and binary data.
func TestRezRoundTrip(t *testing.T) {
	in := sample()
	text := FormatRez(in)
	out, err := ParseRez(text)
	if err != nil {
		t.Fatalf("ParseRez: %v\ntext:\n%s", err, text)
	}
	if !eqResources(in, out) {
		t.Fatalf("rez round-trip mismatch:\ntext:\n%s\n in=%+v\nout=%+v", text, in, out)
	}
}

// TestFullRoundTrip proves the whole chain a derez engine uses: binary fork → rez text
// → binary fork is byte-identical resources.
func TestFullRoundTrip(t *testing.T) {
	in := sample()
	bin := BuildResourceFork(in)
	parsed, err := ParseResourceFork(bin)
	if err != nil {
		t.Fatalf("ParseResourceFork: %v", err)
	}
	text := FormatRez(parsed)
	back, err := ParseRez(text)
	if err != nil {
		t.Fatalf("ParseRez: %v", err)
	}
	rebin := BuildResourceFork(back)
	reparsed, err := ParseResourceFork(rebin)
	if err != nil {
		t.Fatalf("re-ParseResourceFork: %v", err)
	}
	if !eqResources(in, reparsed) {
		t.Fatalf("full round-trip mismatch")
	}
}

// TestEmptyFork proves an empty fork yields no resources and round-trips.
func TestEmptyFork(t *testing.T) {
	out, err := ParseResourceFork(nil)
	if err != nil || out != nil {
		t.Fatalf("ParseResourceFork(nil) = %v, %v; want nil,nil", out, err)
	}
	if rez := FormatRez(nil); len(rez) != 0 {
		t.Fatalf("FormatRez(nil) = %q, want empty", rez)
	}
	res, err := ParseRez(nil)
	if err != nil || res != nil {
		t.Fatalf("ParseRez(nil) = %v, %v; want nil,nil", res, err)
	}
}

// TestRezTextIsReadable spot-checks the human-readable shape (the point of the format).
func TestRezTextIsReadable(t *testing.T) {
	text := string(FormatRez([]Resource{
		{Type: typ("STR "), ID: 128, Name: "Hi", HasName: true, Data: []byte("AB")},
	}))
	for _, want := range []string{"data 'STR '", "(128, \"Hi\")", "$\"", "/*", "*/", "};"} {
		if !bytes.Contains([]byte(text), []byte(want)) {
			t.Fatalf("rez text missing %q:\n%s", want, text)
		}
	}
}
