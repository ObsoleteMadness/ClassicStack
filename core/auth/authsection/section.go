// Package authsection holds the user-store config section ("Auth") that selects and
// locates the file-service user store. It lives in its own package — split out of
// core/auth — because it imports core/config, while core/config now imports core/auth
// (for the pure PBKDF2 helpers behind config.AdminAuth.Verify). Keeping the section
// here breaks what would otherwise be an import cycle (config → auth → config) and
// preserves the rule that core/auth's contract + crypto stay config-free and
// TinyGo-clean.
package authsection

import "github.com/ObsoleteMadness/ClassicStack/core/config"

// Key is the config-section / registry name for the authentication store.
const Key = "Auth"

// Section is the typed config that selects and locates the user store. It carries
// NO password fields — the built-in store keeps secrets in its own file (Path),
// separate from the main config and its backups — so nothing secret rides the
// TOML/UCI codec. It satisfies config.Section (§4) so the model can stage/
// round-trip it.
type Section struct {
	// SKey is the section key; always "Auth". (Stored so Key() is a plain getter.)
	SKey string `toml:"-"`
	// Backend names the store implementation. Only "local" (the built-in
	// file-backed store) ships today; future PAM/Windows/sqlite backends are
	// tagged adapters that register additional names. Empty defaults to "local".
	Backend string `toml:"backend"`
	// Path is the users file the local backend reads/writes (smbpasswd-style).
	// Empty defaults to "users.db" beside the server config. Ignored by backends
	// that do not use a file.
	Path string `toml:"path"`
}

// BackendLocal is the built-in file-backed store name.
const BackendLocal = "local"

// Key returns the section key.
func (s *Section) Key() string { return Key }

// Clone returns a deep copy (all fields are value types).
func (s *Section) Clone() config.Section {
	cp := *s
	return &cp
}

// Validate checks the section in isolation. An unknown backend is not rejected
// here (the compose layer logs once and falls back, mirroring the registry's
// "unregistered component" handling), so a config naming a backend a given build
// lacks does not hard-fail the whole model.
func (s *Section) Validate() error { return nil }

// EffectiveBackend returns the configured backend, defaulting to local.
func (s *Section) EffectiveBackend() string {
	if s.Backend == "" {
		return BackendLocal
	}
	return s.Backend
}

// EffectivePath returns the configured users-file path, defaulting to "users.db".
func (s *Section) EffectivePath() string {
	if s.Path == "" {
		return "users.db"
	}
	return s.Path
}

// compile-time assertion: *Section satisfies config.Section.
var _ config.Section = (*Section)(nil)

// SectionFromModel resolves the Auth Section from the model, falling back to a
// fresh default (local backend, default path) when the model carries none.
func SectionFromModel(m *config.Model) *Section {
	if m != nil {
		if s, ok := m.Get(Key); ok {
			if as, ok := s.(*Section); ok {
				return as
			}
		}
	}
	return &Section{SKey: Key}
}

// Register installs the Auth section schema so codecs can round-trip it without
// knowing the concrete type. Called from the compose registry wiring (kept out of
// an init() so a build that excludes the file services excludes the section too).
func Register() {
	config.Register(config.SectionSchema{
		Key: Key,
		New: func() config.Section { return &Section{SKey: Key} },
		Validate: func(s config.Section) error {
			if as, ok := s.(*Section); ok {
				return as.Validate()
			}
			return nil
		},
	})
}
