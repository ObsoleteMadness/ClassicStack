//go:build sqlite || all

package sqlite_test

import (
	"testing"

	_ "github.com/ObsoleteMadness/ClassicStack/adapter/metastore/sqlite"
	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
)

// TestSQLiteKindRegistered confirms the adapter registers the "sqlite" kind and
// that it behaves as a keyed store, including prefix Range and a CNID round-trip.
func TestSQLiteKindRegistered(t *testing.T) {
	s, err := metastore.Open("sqlite", "")
	if err != nil {
		t.Fatalf("Open(sqlite): %v", err)
	}
	defer s.Close()

	if err := s.Put([]byte("a/1"), []byte("one")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	s.Put([]byte("a/2"), []byte("two"))
	s.Put([]byte("b/1"), []byte("three"))

	if v, ok := s.Get([]byte("a/1")); !ok || string(v) != "one" {
		t.Fatalf("Get a/1 = %q ok=%v", v, ok)
	}

	var got []string
	s.Range([]byte("a/"), func(k, v []byte) bool {
		got = append(got, string(k)+"="+string(v))
		return true
	})
	if len(got) != 2 || got[0] != "a/1=one" || got[1] != "a/2=two" {
		t.Fatalf("Range(a/) = %v", got)
	}

	if err := s.Delete([]byte("a/1")); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := s.Get([]byte("a/1")); ok {
		t.Fatal("a/1 present after delete")
	}
}

func TestSQLiteBacksCNID(t *testing.T) {
	s, err := metastore.Open("sqlite", "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	c := metastore.NewCNIDStore(s)
	id := c.Ensure("dir/file")
	if got, ok := c.CNID("dir/file"); !ok || got != id {
		t.Fatalf("CNID = %d ok=%v want %d", got, ok, id)
	}
	c.Rebind("dir", "moved")
	if p, _ := c.Path(id); p != "moved/file" {
		t.Fatalf("rebind path = %q", p)
	}
}
