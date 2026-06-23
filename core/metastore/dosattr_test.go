package metastore

import (
	"testing"
	"time"
)

func TestDOSInfoRoundTrip(t *testing.T) {
	ct := time.Date(1999, 12, 31, 23, 59, 58, 0, time.UTC)
	cases := []DOSAttr{
		{Attrs: DOSReadOnly | DOSHidden},
		{Attrs: DOSArchive, CreateTime: ct},
		{Attrs: DOSSystem | DOSReadOnly | DOSArchive, CreateTime: ct},
		{}, // empty
	}
	for _, in := range cases {
		got, err := DecodeDOSInfo(EncodeDOSInfo(in))
		if err != nil {
			t.Fatalf("decode(encode(%+v)): %v", in, err)
		}
		if got.Attrs != in.Attrs {
			t.Errorf("attrs round-trip: got %#x want %#x", got.Attrs, in.Attrs)
		}
		if !got.CreateTime.Equal(in.CreateTime) {
			t.Errorf("create-time round-trip: got %v want %v", got.CreateTime, in.CreateTime)
		}
	}
}

func TestDOSInfoRejectsGarbage(t *testing.T) {
	for _, b := range [][]byte{nil, {1}, {0, 0, 0, 0, 0, 0}, {99, 0, 1, 0, 0, 0}} {
		if _, err := DecodeDOSInfo(b); err == nil {
			t.Errorf("DecodeDOSInfo(%v) accepted garbage", b)
		}
	}
}

func TestDOSAttrStoreCRUD(t *testing.T) {
	st, _ := NewMem("")
	s := NewDOSAttrStore(st)

	if _, ok := s.Get("foo.txt"); ok {
		t.Fatal("unstored path should report ok=false")
	}

	want := DOSAttr{Attrs: DOSHidden | DOSReadOnly | DOSDirectory, CreateTime: time.Unix(1000000, 0).UTC()}
	if err := s.Set("foo.txt", want); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Get("foo.txt")
	if !ok {
		t.Fatal("stored path should report ok=true")
	}
	// Structural bits (Directory) are NOT persisted.
	if got.Has(DOSDirectory) {
		t.Error("Directory bit must not be persisted")
	}
	if !got.Has(DOSHidden) || !got.Has(DOSReadOnly) {
		t.Errorf("stored attrs lost bits: %#x", got.Attrs)
	}
	if !got.CreateTime.Equal(want.CreateTime) {
		t.Errorf("create-time: got %v want %v", got.CreateTime, want.CreateTime)
	}

	// Rename carries attributes; the old path is cleared.
	if err := s.Rename("foo.txt", "bar.txt"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("foo.txt"); ok {
		t.Error("old path should be cleared after rename")
	}
	if _, ok := s.Get("bar.txt"); !ok {
		t.Error("new path should carry attributes after rename")
	}

	// Delete drops them.
	if err := s.Delete("bar.txt"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("bar.txt"); ok {
		t.Error("deleted path should report ok=false")
	}
}
