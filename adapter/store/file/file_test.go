package file

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingReturnsNil(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "nope.toml"))
	data, err := s.Load()
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if data != nil {
		t.Fatalf("Load missing = %q, want nil", data)
	}
}

func TestSaveRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "server.toml")
	s := New(p)

	rev, err := s.Save([]byte("v1"))
	if err != nil {
		t.Fatalf("Save v1: %v", err)
	}
	if rev != "" {
		t.Errorf("first Save revision = %q, want empty (nothing to back up)", rev)
	}
	got, _ := s.Load()
	if string(got) != "v1" {
		t.Fatalf("Load = %q, want v1", got)
	}
}

func TestSaveRotatesBackups(t *testing.T) {
	p := filepath.Join(t.TempDir(), "server.toml")
	s := New(p)

	if _, err := s.Save([]byte("v1")); err != nil {
		t.Fatal(err)
	}
	rev, err := s.Save([]byte("v2"))
	if err != nil {
		t.Fatalf("Save v2: %v", err)
	}
	if rev != p+".1" {
		t.Errorf("revision = %q, want %q", rev, p+".1")
	}
	// .1 holds the prior contents; the live file holds the new ones.
	b1, err := os.ReadFile(p + ".1")
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(b1) != "v1" {
		t.Errorf("backup .1 = %q, want v1", b1)
	}
	got, _ := s.Load()
	if string(got) != "v2" {
		t.Errorf("live = %q, want v2", got)
	}
}

func TestSaveDropsOldestBeyondMax(t *testing.T) {
	p := filepath.Join(t.TempDir(), "server.toml")
	s := New(p)
	s.MaxBackups = 2

	for i := range 5 {
		if _, err := s.Save([]byte{byte('a' + i)}); err != nil {
			t.Fatal(err)
		}
	}
	// With MaxBackups=2 only .1 and .2 may exist; .3 must not.
	if _, err := os.Stat(p + ".3"); !os.IsNotExist(err) {
		t.Errorf(".3 should have been dropped (err=%v)", err)
	}
	if _, err := os.Stat(p + ".2"); err != nil {
		t.Errorf(".2 should exist: %v", err)
	}
}
