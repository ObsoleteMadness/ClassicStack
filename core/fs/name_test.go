package fs

import "testing"

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
