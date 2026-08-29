package config

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/auth"
)

// makeAdmin derives a fully-configured AdminAuth for a fixed salt/password, the way
// the HTTP /setup handler would (salt generation aside — a fixed salt keeps the test
// deterministic).
func makeAdmin(t *testing.T, user, password string) AdminAuth {
	t.Helper()
	salt := make([]byte, auth.SaltLen)
	for i := range salt {
		salt[i] = byte(i + 1)
	}
	cred := auth.DeriveCredential(password, salt)
	return AdminAuth{User: user, SaltHex: cred.SaltHex(), HashHex: cred.HashHex()}
}

func TestAdminAuthConfigured(t *testing.T) {
	if (AdminAuth{}).Configured() {
		t.Error("zero AdminAuth should be unconfigured")
	}
	if (AdminAuth{User: "admin"}).Configured() {
		t.Error("user without salt/hash should be unconfigured")
	}
	if !makeAdmin(t, "admin", "pw").Configured() {
		t.Error("fully-set AdminAuth should be configured")
	}
}

func TestAdminAuthVerify(t *testing.T) {
	a := makeAdmin(t, "admin", "hunter2")

	if !a.Verify("admin", "hunter2") {
		t.Error("correct credential should verify")
	}
	if a.Verify("admin", "wrong") {
		t.Error("wrong password must not verify")
	}
	if a.Verify("root", "hunter2") {
		t.Error("wrong user must not verify")
	}
	// Username match is case-insensitive (mirrors the user-store convention).
	if !a.Verify("ADMIN", "hunter2") {
		t.Error("username match should be case-insensitive")
	}
	// An unconfigured credential never verifies, even with empty inputs.
	if (AdminAuth{}).Verify("", "") {
		t.Error("unconfigured AdminAuth must never verify")
	}
}

func TestAdminAuthValidate(t *testing.T) {
	// Unconfigured (empty) is a legitimate first-run state.
	if err := (AdminAuth{}).Validate(); err != nil {
		t.Errorf("empty AdminAuth should validate: %v", err)
	}
	// A good credential validates.
	if err := makeAdmin(t, "admin", "pw").Validate(); err != nil {
		t.Errorf("valid AdminAuth rejected: %v", err)
	}
	// Control character in the username is rejected.
	if err := (AdminAuth{User: "ad\x00min"}).Validate(); err == nil {
		t.Error("username with a control char should fail validation")
	}
	// A present-but-corrupt salt/hash pair is rejected.
	if err := (AdminAuth{User: "admin", SaltHex: "zz", HashHex: "zz"}).Validate(); err == nil {
		t.Error("malformed credential should fail validation")
	}
}

func TestAdminAuthCloneIndependent(t *testing.T) {
	a := makeAdmin(t, "admin", "pw")
	b := a.Clone()
	b.User = "other"
	if a.User != "admin" {
		t.Error("Clone aliased the original")
	}
}

// TestModelValidateChecksAdminAuth confirms Model.Validate surfaces a bad AdminAuth.
func TestModelValidateChecksAdminAuth(t *testing.T) {
	m := NewModel()
	m.AdminAuth = AdminAuth{User: "bad\x01name"}
	if err := m.Validate(ValidateOptions{}); err == nil {
		t.Error("Model.Validate should reject an invalid AdminAuth username")
	}
}
