package config

import "testing"

// TestLoad_ExampleFile loads the canonical server.toml.example from the
// repo root to make sure the schema and the example stay in sync.
func TestLoad_ExampleFile(t *testing.T) {
	cfg, err := Load("../server.toml.example")
	if err != nil {
		t.Fatalf("Load(server.toml.example): %v", err)
	}
	if cfg.AFPServerName != "OmniTalk" {
		t.Fatalf("AFPServerName = %q, want %q", cfg.AFPServerName, "OmniTalk")
	}
	if len(cfg.AFPVolumes) != 2 {
		t.Fatalf("AFPVolumes = %d, want 2", len(cfg.AFPVolumes))
	}
}
