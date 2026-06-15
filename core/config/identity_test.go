package config

import "testing"

// TestIdentityValidateBaseline: the always-on baseline accepts a normal hostname (and
// an empty one — a consumer defaults it) but rejects path/control characters.
func TestIdentityValidateBaseline(t *testing.T) {
	good := []string{"", "CLASSICSTACK", "my-server", "host.local"}
	for _, h := range good {
		if err := (Identity{Hostname: h}).Validate(); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", h, err)
		}
	}
	bad := []string{"bad/name", "bad\\name", "ctrl\x01", "tab\there"}
	for _, h := range bad {
		if err := (Identity{Hostname: h}).Validate(); err != ErrHostnameInvalid {
			t.Errorf("Validate(%q) = %v, want ErrHostnameInvalid", h, err)
		}
	}
}

// TestIdentityNetBIOSConstraintWhenEnabled: the 15-byte NetBIOS limit is a consumer
// constraint — baseline Validate accepts a 20-char name, but ValidateForNetBIOS (run
// only when NetBIOS is enabled) rejects it. A name within the limit passes both.
func TestIdentityNetBIOSConstraintWhenEnabled(t *testing.T) {
	long := Identity{Hostname: "THIS-NAME-IS-WAY-TOO-LONG"} // > 15 bytes
	if err := long.Validate(); err != nil {
		t.Fatalf("baseline Validate should accept a long hostname (SMB :445 / AFP-only): %v", err)
	}
	if err := long.ValidateForNetBIOS(); err != ErrHostnameTooLongForNetBIOS {
		t.Fatalf("ValidateForNetBIOS(long) = %v, want ErrHostnameTooLongForNetBIOS", err)
	}

	ok := Identity{Hostname: "classicstack"} // 12 bytes
	if err := ok.ValidateForNetBIOS(); err != nil {
		t.Fatalf("ValidateForNetBIOS(short) = %v, want nil", err)
	}
}

// TestIdentityNetBIOSNameUppercases: the NetBIOS consumer claims the name upper-cased
// and trimmed (NetBIOS names are case-insensitive upper-case).
func TestIdentityNetBIOSNameUppercases(t *testing.T) {
	if got := (Identity{Hostname: "  My-Host  "}).NetBIOSName(); got != "MY-HOST" {
		t.Fatalf("NetBIOSName = %q, want MY-HOST", got)
	}
}

// TestIdentityClonedWithModel: Identity rides Model.Clone as a value (no aliasing).
func TestIdentityClonedWithModel(t *testing.T) {
	m := NewModel()
	m.Identity = Identity{Hostname: "ORIG", Workgroup: "WG", Description: "d"}
	cp := m.Clone()
	cp.Identity.Hostname = "CHANGED"
	if m.Identity.Hostname != "ORIG" {
		t.Fatal("Clone aliased Identity")
	}
}
