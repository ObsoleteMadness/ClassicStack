package config

import (
	"errors"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/auth"
)

// AdminAuthKey is the well-known section key for the web-management-interface
// credential. AdminAuth is a typed field on Model (like Identity/Logging/Router/
// Bridge), not a registered component section, so this key is the codec/UI handle,
// not a Sections map entry.
const AdminAuthKey = "AdminAuth"

// AdminAuth is the single web-management-interface admin credential (§4-ter): a
// username plus a salted PBKDF2-SHA256 hash. It NEVER carries a plaintext password —
// the cleartext exists only transiently in a /setup request body, where it is
// immediately derived into Salt/Hash by the HTTP adapter (which owns crypto/rand).
//
// This is the management-plane credential, distinct from the file-service user store
// (auth.UserStore, the AFP/SMB share users): there is exactly one admin, it lives in
// the config model (so it round-trips through server.toml), and it gates the web UI
// via HTTP Basic auth. An empty value (no User) means "not yet configured" — the web
// server then enters first-run setup mode and prompts the operator to create it.
//
// A salted hash at rest in config is acceptable: it is not a recoverable secret, so
// unlike a backend password it is NOT redacted by config.SecretMasker — masking it
// would also break Verify on a Config() round-trip (the verifier reads the model the
// plane returns). The hash is the value we want persisted and reloaded verbatim.
type AdminAuth struct {
	// User is the admin username. Empty = unconfigured (first-run). Matched
	// case-insensitively at login (mirrors the user-store convention).
	User string `toml:"user"`
	// SaltHex is the hex-encoded PBKDF2 salt (auth.SaltLen bytes). The adapter ring
	// generates it with crypto/rand; core never does.
	SaltHex string `toml:"salt"`
	// HashHex is the hex-encoded PBKDF2-SHA256 derivation of the password under Salt.
	HashHex string `toml:"hash"`
}

// ErrAdminUserInvalid is returned by Validate when the admin username carries a
// control character.
var ErrAdminUserInvalid = errors.New("config: admin username contains an illegal character")

// ErrAdminCredentialInvalid is returned by Validate when a present salt/hash pair
// cannot be decoded (a corrupt [adminauth] block in server.toml).
var ErrAdminCredentialInvalid = errors.New("config: admin credential is malformed")

// Key returns the well-known section key.
func (AdminAuth) Key() string { return AdminAuthKey }

// Clone returns a copy. AdminAuth is all value-typed fields, so a shallow copy is a
// deep copy.
func (a AdminAuth) Clone() AdminAuth { return a }

// Configured reports whether an admin credential is fully set. The web server uses
// this to decide first-run (false → show setup) vs. enforce-Basic-auth (true).
func (a AdminAuth) Configured() bool {
	return a.User != "" && a.SaltHex != "" && a.HashHex != ""
}

// Validate checks the credential in isolation (run from Model.Validate on the commit
// path): the username must not carry a control character (it appears in a WWW-
// Authenticate realm / log line), and a present salt/hash pair must decode. An
// unconfigured (empty) AdminAuth validates clean — first-run is a legitimate state.
func (a AdminAuth) Validate() error {
	for _, r := range a.User {
		if r < 0x20 || r == 0x7f {
			return ErrAdminUserInvalid
		}
	}
	if a.SaltHex != "" || a.HashHex != "" {
		if _, err := auth.ParseCredential(a.SaltHex, a.HashHex); err != nil {
			return ErrAdminCredentialInvalid
		}
	}
	return nil
}

// Verify reports whether (user, password) matches the stored credential, in constant
// time. It uses only the pure core/auth helpers (no crypto/rand), so it is safe in
// core. A zero/unconfigured AdminAuth, an unknown user, or a corrupt record never
// verifies. Username match is case-insensitive.
func (a AdminAuth) Verify(user, password string) bool {
	if !a.Configured() || !strings.EqualFold(user, a.User) {
		return false
	}
	cred, err := auth.ParseCredential(a.SaltHex, a.HashHex)
	if err != nil {
		return false
	}
	return cred.Verify(password)
}
