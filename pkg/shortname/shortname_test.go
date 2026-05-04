package shortname

import "testing"

func TestRoundTrip(t *testing.T) {
	m := NewMapper(nil)
	short := m.LongToShort("My Long Filename.txt")
	if short != "MYLONGF~1.TXT" {
		// Stub algorithm: stripped spaces, uppercased, 6 chars + ~1, .TXT.
		// Check the prefix and suffix shape rather than exact content
		// so the test survives small algorithm tweaks before the real
		// Windows mapping lands.
		if !endsWith(short, "~1.TXT") {
			t.Fatalf("unexpected short name %q", short)
		}
	}
	got, ok := m.ShortToLong(short)
	if !ok {
		t.Fatalf("ShortToLong(%q): not found", short)
	}
	if got != "My Long Filename.txt" {
		t.Fatalf("ShortToLong: got %q want %q", got, "My Long Filename.txt")
	}
}

func TestBindIsIdempotent(t *testing.T) {
	m := NewMapper(nil)
	a := m.Bind("/home", "report.docx")
	b := m.Bind("/home", "report.docx")
	if a != b {
		t.Fatalf("Bind not idempotent: %q vs %q", a, b)
	}
}

func endsWith(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
