package toml

import (
	"path/filepath"
	"testing"

	filestore "github.com/ObsoleteMadness/ClassicStack/adapter/store/file"
	"github.com/ObsoleteMadness/ClassicStack/core/auth/authsection"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

// TestAuthSectionRoundTrip proves the M8a Auth config section survives a TOML
// round-trip through the schema registry: the backend/path a user sets in
// server.toml is what the supervisor reads back via authsection.SectionFromModel. The
// section carries no secrets (those live in the users file), so nothing
// sensitive rides the codec — this only checks the selector fields.
func TestAuthSectionRoundTrip(t *testing.T) {
	authsection.Register() // installs the "Auth" schema codecs iterate

	m := config.NewModel()
	m.Set(&authsection.Section{SKey: authsection.Key, Backend: authsection.BackendLocal, Path: "/etc/classicstack/users.db"})

	c := New()
	data, err := c.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got := config.NewModel()
	if err := c.Unmarshal(data, got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	sec := authsection.SectionFromModel(got)
	if sec.EffectiveBackend() != authsection.BackendLocal {
		t.Errorf("backend: got %q want %q", sec.EffectiveBackend(), authsection.BackendLocal)
	}
	if sec.EffectivePath() != "/etc/classicstack/users.db" {
		t.Errorf("path: got %q want %q", sec.EffectivePath(), "/etc/classicstack/users.db")
	}
}

// TestAuthSectionRoundTripDefaults proves an empty Auth section round-trips and
// still resolves to the built-in defaults (local backend, users.db) — a config
// that omits [Auth] entirely behaves identically.
func TestAuthSectionRoundTripDefaults(t *testing.T) {
	authsection.Register()

	m := config.NewModel()
	m.Set(&authsection.Section{SKey: authsection.Key})

	c := New()
	data, err := c.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got := config.NewModel()
	if err := c.Unmarshal(data, got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	sec := authsection.SectionFromModel(got)
	if sec.EffectiveBackend() != authsection.BackendLocal {
		t.Errorf("default backend: got %q want %q", sec.EffectiveBackend(), authsection.BackendLocal)
	}
	if sec.EffectivePath() != "users.db" {
		t.Errorf("default path: got %q want %q", sec.EffectivePath(), "users.db")
	}
}

// TestConfigPersistsThroughStore exercises the whole M8 config path end to end:
// codec.Marshal → file.Store.Save (with backup rotation) → Store.Load →
// codec.Unmarshal, and asserts the Auth selector survives. This is the path the
// control plane's config-apply drives — proving the codec and store adapters
// compose, not just that each works alone.
func TestConfigPersistsThroughStore(t *testing.T) {
	authsection.Register()

	c := New()
	store := filestore.New(filepath.Join(t.TempDir(), "server.toml"))

	m := config.NewModel()
	m.Logging = config.LoggingSection{Level: "warn"}
	m.Set(&authsection.Section{SKey: authsection.Key, Backend: authsection.BackendLocal, Path: "users.db"})

	data, err := c.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if _, err := store.Save(data); err != nil {
		t.Fatalf("Save: %v", err)
	}

	raw, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := config.NewModel()
	if err := c.Unmarshal(raw, got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.Logging.Level != "warn" {
		t.Errorf("logging level: got %q want warn", got.Logging.Level)
	}
	if sec := authsection.SectionFromModel(got); sec.EffectivePath() != "users.db" {
		t.Errorf("auth path after persist: got %q want users.db", sec.EffectivePath())
	}
}
