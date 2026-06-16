package uci

import (
	"strings"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/auth"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

func configuredAdmin(user, password string) config.AdminAuth {
	salt := make([]byte, auth.SaltLen)
	for i := range salt {
		salt[i] = byte(i + 7)
	}
	cred := auth.DeriveCredential(password, salt)
	return config.AdminAuth{User: user, SaltHex: cred.SaltHex(), HashHex: cred.HashHex()}
}

// TestAdminAuthRoundTrip proves the §4-ter web-admin credential survives a UCI
// round-trip (the OpenWRT config path), reading back the same hash and still
// verifying the original password.
func TestAdminAuthRoundTrip(t *testing.T) {
	m := config.NewModel()
	m.AdminAuth = configuredAdmin("admin", "hunter2")

	c := New()
	data, err := c.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got := config.NewModel()
	if err := c.Unmarshal(data, got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.AdminAuth != m.AdminAuth {
		t.Fatalf("AdminAuth round-trip mismatch:\n got %+v\nwant %+v", got.AdminAuth, m.AdminAuth)
	}
	if !got.AdminAuth.Verify("admin", "hunter2") {
		t.Error("reloaded admin credential should still verify the password")
	}
}

// TestAdminAuthUnconfiguredOmitted proves an unconfigured AdminAuth writes no
// adminauth block, so a fresh config stays first-run on reload.
func TestAdminAuthUnconfiguredOmitted(t *testing.T) {
	m := config.NewModel()

	c := New()
	data, err := c.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(data), "adminauth") {
		t.Errorf("unconfigured model should emit no adminauth block, got:\n%s", data)
	}

	got := config.NewModel()
	if err := c.Unmarshal(data, got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.AdminAuth.Configured() {
		t.Error("reloaded model should be unconfigured (first-run)")
	}
}
