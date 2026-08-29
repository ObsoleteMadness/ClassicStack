package appledouble

import (
	"bytes"
	"testing"
)

func TestRoundTripFinderAndResource(t *testing.T) {
	t.Parallel()
	var fi [32]byte
	copy(fi[:], "APPLMACS")
	in := Parsed{
		FinderInfo:  fi,
		Resource:    []byte("hello-rsrc"),
		HasFinder:   true,
		HasResource: true,
	}
	raw := Build(in, false, 0)
	out, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !out.HasFinder || out.FinderInfo != fi {
		t.Fatalf("FinderInfo round-trip mismatch: got %v", out.FinderInfo)
	}
	if !out.HasResource || !bytes.Equal(out.Resource, in.Resource) {
		t.Fatalf("Resource round-trip mismatch")
	}
}

func TestRoundTripWithComment(t *testing.T) {
	t.Parallel()
	in := Parsed{
		Comment:     []byte("hi"),
		Resource:    []byte("r"),
		HasComment:  true,
		HasResource: true,
	}
	raw := Build(in, true, uint32(len(in.Comment)))
	out, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !out.HasComment || !bytes.Equal(out.Comment, in.Comment) {
		t.Fatalf("Comment round-trip mismatch: got %q", out.Comment)
	}
}

func TestParseRejectsBadMagic(t *testing.T) {
	t.Parallel()
	b := make([]byte, HeaderSize)
	if _, err := Parse(b); err == nil {
		t.Fatal("expected error on bad magic")
	}
}

func TestSidecarPath(t *testing.T) {
	t.Parallel()
	got := SidecarPath("/Volumes/X/foo.txt")
	want := "/Volumes/X/._foo.txt"
	// On Windows, filepath.Join uses backslash; compare the basename.
	if !bytes.HasSuffix([]byte(got), []byte("._foo.txt")) {
		t.Fatalf("SidecarPath = %q, want suffix %q (full want=%q)", got, "._foo.txt", want)
	}
}
