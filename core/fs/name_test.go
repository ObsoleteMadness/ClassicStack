package fs

import (
	"strings"
	"testing"
)

func TestDerivedNameEngine_ShortNameRoundTrip(t *testing.T) {
	e := NewDerivedNameEngine(nil)

	short := e.Bind("dir", "LongFileName.txt", ShortName)
	if short == "" || len(short) > 12 { // 8 + '.' + 3
		t.Fatalf("short name = %q (len too large)", short)
	}
	// Stable: same long → same short.
	if again := e.Bind("dir", "LongFileName.txt", ShortName); again != short {
		t.Fatalf("Bind not stable: %q vs %q", short, again)
	}
	// Reverse maps back to the long name.
	long, ok := e.ToLong("dir", short, ShortName)
	if !ok || long != "LongFileName.txt" {
		t.Fatalf("ToLong(%q) = %q ok=%v", short, long, ok)
	}
}

func TestDerivedNameEngine_ShortNameAlreadyValidUnchanged(t *testing.T) {
	e := NewDerivedNameEngine(nil)
	for _, long := range []string{"rp9.exe", "README.TXT", "A.B", "FILENAME.EXE"} {
		if short := e.Bind("dir", long, ShortName); short != strings.ToUpper(long) {
			t.Fatalf("Bind(%q) = %q, want unchanged %q", long, short, strings.ToUpper(long))
		}
	}
	// A later long name that WOULD derive to the same base still gets a suffix
	// instead of colliding with the as-is binding.
	other := e.Bind("dir", "RP9x.exe", ShortName)
	if other == "RP9.EXE" {
		t.Fatalf("collision: second name reused the as-is short name %q", other)
	}
}

func TestDerivedNameEngine_ShortNameCollision(t *testing.T) {
	e := NewDerivedNameEngine(nil)
	a := e.Bind("d", "ReportFinal2024.xlsx", ShortName)
	b := e.Bind("d", "ReportFinalDraft.xlsx", ShortName)
	if a == b {
		t.Fatalf("colliding long names produced same short name %q", a)
	}
	// Each still reverses to its own long name.
	if l, _ := e.ToLong("d", a, ShortName); l != "ReportFinal2024.xlsx" {
		t.Fatalf("a reverses to %q", l)
	}
	if l, _ := e.ToLong("d", b, ShortName); l != "ReportFinalDraft.xlsx" {
		t.Fatalf("b reverses to %q", l)
	}
}

func TestDerivedNameEngine_Medium31Limit(t *testing.T) {
	e := NewDerivedNameEngine(nil)
	long := "this-name-is-definitely-longer-than-thirty-one-characters.txt"
	med := e.Bind("d", long, MediumName)
	if len(med) > 31 {
		t.Fatalf("medium name = %q (len %d > 31)", med, len(med))
	}
}

func TestDerivedNameEngine_CaseInsensitiveLookup(t *testing.T) {
	e := NewDerivedNameEngine(nil)
	// First sight establishes the binding with the ORIGINAL case.
	first := e.Bind("d", "ReadMe.TXT", ShortName)
	// A differently-cased request for the SAME name resolves to the SAME derived
	// name (case-insensitive lookup, Windows-FS semantics) — not a collision.
	again := e.Bind("d", "README.txt", ShortName)
	if again != first {
		t.Fatalf("case-insensitive lookup produced different short names: %q vs %q", first, again)
	}
	// The directory's casing must not matter either.
	if d := e.Bind("D", "ReadMe.TXT", ShortName); d != first {
		t.Fatalf("dir casing changed the binding: %q vs %q", d, first)
	}
}

func TestDerivedNameEngine_MediumPreservesCase(t *testing.T) {
	e := NewDerivedNameEngine(nil)
	med := e.Bind("d", "MyMixedCaseName", MediumName)
	if med != "MyMixedCaseName" {
		t.Fatalf("medium name lost its stored case: %q", med)
	}
	// Looked up case-insensitively, it still reverses to the stored-case long name.
	if l, ok := e.ToLong("d", "MYMIXEDCASENAME", MediumName); !ok || l != "MyMixedCaseName" {
		t.Fatalf("case-insensitive medium reverse: %q ok=%v", l, ok)
	}
}

func TestDerivedNameEngine_DifferentKindsIndependent(t *testing.T) {
	e := NewDerivedNameEngine(nil)
	s := e.Bind("d", "MyDocument.txt", ShortName)
	m := e.Bind("d", "MyDocument.txt", MediumName)
	if ls, _ := e.ToLong("d", s, ShortName); ls != "MyDocument.txt" {
		t.Fatalf("short reverse = %q", ls)
	}
	if lm, _ := e.ToLong("d", m, MediumName); lm != "MyDocument.txt" {
		t.Fatalf("medium reverse = %q", lm)
	}
}
