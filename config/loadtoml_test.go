package config

import "testing"

// TestLoad_ExampleFile loads the canonical server.toml.example from the
// repo root to make sure the parser still accepts the shipped example.
// Schema-level checks live with the consumers (e.g. service/afp).
func TestLoad_ExampleFile(t *testing.T) {
	src, err := Load("../server.toml.example")
	if err != nil {
		t.Fatalf("Load(server.toml.example): %v", err)
	}
	if got := src.K.String("AFP.name"); got != "ClassicStack" {
		t.Fatalf("AFP.name = %q, want %q", got, "ClassicStack")
	}
	if vols := src.K.MapKeys("AFP.Volumes"); len(vols) != 2 {
		t.Fatalf("AFP.Volumes = %d, want 2", len(vols))
	}
}
