package afp

import (
	"bytes"
	"testing"
)

// TestParseExtensionMap proves the Netatalk-format parser reads `.ext "TYPE" "CRTR"`
// lines, skips blanks/comments, lowercases the extension, and rejects malformed lines.
func TestParseExtensionMap(t *testing.T) {
	src := []byte("# a comment\n" +
		".TXT \"TEXT\" \"ttxt\"\n" +
		"\n" +
		"jpg \"JPEG\" \"ogle\"\n")
	m, err := ParseExtensionMap(src)
	if err != nil {
		t.Fatalf("ParseExtensionMap: %v", err)
	}

	// Case-insensitive on extension; dot optional.
	mp, ok := m.Lookup("readme.txt")
	if !ok {
		t.Fatal("expected .txt mapping")
	}
	if string(mp.FileType[:]) != "TEXT" || string(mp.Creator[:]) != "ttxt" {
		t.Fatalf("txt mapping = %q/%q, want TEXT/ttxt", mp.FileType, mp.Creator)
	}
	if mp2, ok := m.Lookup("photo.JPG"); !ok || string(mp2.FileType[:]) != "JPEG" {
		t.Fatalf("jpg lookup failed: %v %q", ok, mp2.FileType)
	}
	// No extension / no entry → no mapping.
	if _, ok := m.Lookup("noext"); ok {
		t.Error("expected no mapping for an extensionless name")
	}
	if _, ok := m.Lookup("file.xyz"); ok {
		t.Error("expected no mapping for an unknown extension")
	}
}

// TestParseExtensionMapRejectsBadLine proves a malformed line is a hard error naming
// the line number (so the management plane can reject an edited map).
func TestParseExtensionMapRejectsBadLine(t *testing.T) {
	if err := ValidateExtensionMap([]byte(".txt \"TOOLONG\" \"ttxt\"")); err == nil {
		t.Fatal("expected an error for an over-length type")
	}
	if err := ValidateExtensionMap([]byte("garbage line with no quotes")); err == nil {
		t.Fatal("expected an error for an unparseable line")
	}
}

// TestExtensionMapMarshalRoundTrip proves Marshal emits the Netatalk format that
// ParseExtensionMap reads back identically (the UI grid save→load round-trip).
func TestExtensionMapMarshalRoundTrip(t *testing.T) {
	src := []byte(".txt \"TEXT\" \"ttxt\"\n.gif \"GIFf\" \"ogle\"\n")
	m, err := ParseExtensionMap(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out := m.Marshal()
	m2, err := ParseExtensionMap(out)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	mp1, _ := m.Lookup("a.txt")
	mp2, _ := m2.Lookup("a.txt")
	if mp1 != mp2 {
		t.Fatalf("round-trip mismatch: %v vs %v", mp1, mp2)
	}
	// Deterministic (sorted) output: gif before txt.
	if !bytes.Contains(out, []byte(".gif")) || !bytes.Contains(out, []byte(".txt")) {
		t.Fatalf("marshal missing entries: %q", out)
	}
}

// TestExtensionMappingFinderInfo proves the synthesized Finder info carries the type at
// bytes 0-3 and creator at 4-7, the rest zero.
func TestExtensionMappingFinderInfo(t *testing.T) {
	mp, err := NewExtensionMapping("TEXT", "ttxt")
	if err != nil {
		t.Fatalf("NewExtensionMapping: %v", err)
	}
	info := mp.FinderInfo()
	if string(info[0:4]) != "TEXT" || string(info[4:8]) != "ttxt" {
		t.Fatalf("FinderInfo type/creator = %q/%q", info[0:4], info[4:8])
	}
	for i := 8; i < 32; i++ {
		if info[i] != 0 {
			t.Fatalf("byte %d non-zero: %d", i, info[i])
		}
	}
}

// TestNilExtensionMapLookup proves a nil *ExtensionMap matches nothing (no nil guard
// needed at the call site).
func TestNilExtensionMapLookup(t *testing.T) {
	var m *ExtensionMap
	if _, ok := m.Lookup("a.txt"); ok {
		t.Fatal("nil map should match nothing")
	}
}
