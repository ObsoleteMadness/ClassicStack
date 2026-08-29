package metastore

import "testing"

func TestEAListRoundTrip(t *testing.T) {
	cases := [][]EA{
		nil,
		{{Name: ".LONGNAME", Value: []byte("A really long file name.txt")}},
		{
			{Name: ".TYPE", Value: []byte("EAT_ASCII"), NeedEA: true},
			{Name: ".CLASSINFO", Value: []byte{0x01, 0x00, 0xFF, 0x7F}},
		},
		{{Name: "", Value: nil}}, // empty name/value is still a record
	}
	for _, in := range cases {
		got, ok := decodeEAList(encodeEAList(in))
		if !ok {
			t.Fatalf("decode(encode(%+v)) rejected", in)
		}
		if len(got) != len(in) {
			t.Fatalf("round-trip count: got %d want %d", len(got), len(in))
		}
		for i := range in {
			if got[i].Name != in[i].Name || string(got[i].Value) != string(in[i].Value) || got[i].NeedEA != in[i].NeedEA {
				t.Errorf("entry %d round-trip: got %+v want %+v", i, got[i], in[i])
			}
		}
	}
}

func TestEAListRejectsGarbage(t *testing.T) {
	for _, b := range [][]byte{nil, {1}, {0, 0, 0, 0}, {99, 0, 1, 0}} {
		if _, ok := decodeEAList(b); ok {
			t.Errorf("decodeEAList(%v) accepted garbage", b)
		}
	}
}

func TestEAStoreCRUD(t *testing.T) {
	st, _ := NewMem("")
	s := NewEAStore(st, nil)

	if _, ok := s.Get("foo.txt"); ok {
		t.Fatal("unstored path should report ok=false")
	}

	want := []EA{
		{Name: ".LONGNAME", Value: []byte("Foo Document.txt")},
		{Name: ".TYPE", Value: []byte("EAT_ASCII")},
	}
	if err := s.Set("foo.txt", want); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Get("foo.txt")
	if !ok {
		t.Fatal("stored path should report ok=true")
	}
	if len(got) != len(want) {
		t.Fatalf("stored EAs: got %d want %d", len(got), len(want))
	}

	// Rename carries EAs; the old path is cleared.
	if err := s.Rename("foo.txt", "bar.txt"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("foo.txt"); ok {
		t.Error("old path should be cleared after rename")
	}
	if _, ok := s.Get("bar.txt"); !ok {
		t.Error("new path should carry EAs after rename")
	}

	// Delete drops them.
	if err := s.Delete("bar.txt"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("bar.txt"); ok {
		t.Error("deleted path should report ok=false")
	}
}
