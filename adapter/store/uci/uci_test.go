package uci

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestUCIStore_LoadSaveFileFallback(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "uci-store-test")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	path := filepath.Join(tmpDir, "classicstack")
	s := New(path)

	// uciCmd to non-existent so we force fallback
	s.uciCmd = "non-existent-command-name-here"

	// Initial load should be empty (file doesn't exist)
	data, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if data != nil {
		t.Fatalf("expected nil data for non-existent file, got %s", data)
	}

	// Save
	input := []byte("config logging\n\toption level 'debug'\n")
	rev, err := s.Save(input)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if rev != path {
		t.Errorf("Save returned revision %q, want %q", rev, path)
	}

	// Reload
	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load after save: %v", err)
	}
	if !bytes.Equal(got, input) {
		t.Errorf("Load got %q, want %q", got, input)
	}
}
