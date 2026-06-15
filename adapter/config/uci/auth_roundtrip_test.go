package uci

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/auth"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

// TestAuthSectionRoundTrip proves the M8a Auth config section survives a UCI
// round-trip via the schema registry, so an OpenWRT deployment reads back the
// same store selector it wrote. UCI lower-cases the section type ("config auth"),
// which the codec matches case-insensitively against the "Auth" schema key.
func TestAuthSectionRoundTrip(t *testing.T) {
	auth.Register()

	m := config.NewModel()
	m.Set(&auth.Section{SKey: auth.Key, Backend: auth.BackendLocal, Path: "/etc/config/users.db"})

	c := New()
	data, err := c.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got := config.NewModel()
	if err := c.Unmarshal(data, got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	sec := auth.SectionFromModel(got)
	if sec.EffectiveBackend() != auth.BackendLocal {
		t.Errorf("backend: got %q want %q", sec.EffectiveBackend(), auth.BackendLocal)
	}
	if sec.EffectivePath() != "/etc/config/users.db" {
		t.Errorf("path: got %q want %q", sec.EffectivePath(), "/etc/config/users.db")
	}
}

// TestEmptyStringOptionRoundTrips guards a tokenizer bug: an unset string field
// marshals to `option key ”`, and an empty quoted value must parse back to an
// empty string, not be dropped (dropping it left the option line with too few
// tokens and failed the whole Unmarshal). A default model — whose well-known
// Logging.Level is "" — must survive a UCI save/load.
func TestEmptyStringOptionRoundTrips(t *testing.T) {
	m := config.NewModel() // Logging.Level == "", Router.DefaultZone == "", etc.

	c := New()
	data, err := c.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got := config.NewModel()
	if err := c.Unmarshal(data, got); err != nil {
		t.Fatalf("Unmarshal default model: %v", err)
	}
	if got.Logging.Level != "" {
		t.Errorf("logging level: got %q want empty", got.Logging.Level)
	}
}
