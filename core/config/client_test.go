package config

import (
	"testing"
	"time"
)

func TestClientIdleDurationDefault(t *testing.T) {
	if got := (ClientSection{}).IdleDuration(); got != 10*time.Minute {
		t.Fatalf("zero Client IdleDuration = %s, want 10m", got)
	}
	if got := (ClientSection{MaxIdleMinutes: 3}).IdleDuration(); got != 3*time.Minute {
		t.Fatalf("IdleDuration = %s, want 3m", got)
	}
}

func TestClientAllowsService(t *testing.T) {
	empty := ClientSection{}
	for _, svc := range []string{"afp", "smb", "ncp", "etherdfs"} {
		if !empty.AllowsService(svc) {
			t.Errorf("empty Services should allow %s", svc)
		}
	}
	if empty.AllowsService("ftp") {
		t.Fatal("empty Services must not allow unknown schemes")
	}
	only := ClientSection{Services: []string{"AFP", " smb "}}
	if !only.AllowsService("afp") || !only.AllowsService("smb") {
		t.Fatal("listed services should be allowed")
	}
	if only.AllowsService("ncp") {
		t.Fatal("unlisted service should be denied")
	}
}

func TestClientEnabledServices(t *testing.T) {
	got := (ClientSection{}).EnabledServices()
	want := []string{ClientServiceAFP, ClientServiceSMB, ClientServiceNCP, ClientServiceEtherDFS}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	got = (ClientSection{Services: []string{"SMB", "afp", "afp", "ftp"}}).EnabledServices()
	if len(got) != 2 || got[0] != "smb" || got[1] != "afp" {
		t.Fatalf("normalized services = %v", got)
	}
}

func TestClientValidate(t *testing.T) {
	if err := (ClientSection{MaxIdleMinutes: -1}).Validate(); err == nil {
		t.Fatal("negative max_idle_minutes should fail")
	}
	if err := (ClientSection{Services: []string{"ftp"}}).Validate(); err == nil {
		t.Fatal("unknown service should fail")
	}
	if err := (ClientSection{Enabled: true, Services: []string{"afp", "smb"}}).Validate(); err != nil {
		t.Fatalf("valid section: %v", err)
	}
}

func TestClientCloneIndependent(t *testing.T) {
	s := ClientSection{Enabled: true, Services: []string{"afp"}}
	c := s.Clone()
	c.Services[0] = "smb"
	if s.Services[0] != "afp" {
		t.Fatal("clone mutated original Services")
	}
}

func TestModelValidateClient(t *testing.T) {
	m := NewModel()
	m.Client = ClientSection{Services: []string{"nope"}}
	if err := m.Validate(ValidateOptions{}); err == nil {
		t.Fatal("bad [Client] should fail Model.Validate")
	}
}
