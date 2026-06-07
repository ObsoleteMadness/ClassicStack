package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestModelTOMLRoundTrip(t *testing.T) {
	m := Defaults()
	m.LToUDP.SeedZone = "Custom Zone"
	m.AFP.Volumes = map[string]VolumeModel{
		"TestVol": {Name: "Test Vol", Path: `C:\Mac\Test`, FSType: "local_fs"},
	}
	m.WebUI.Enabled = true
	m.WebUI.Bind = "127.0.0.1:9000"

	data, err := m.ToTOML()
	if err != nil {
		t.Fatalf("ToTOML: %v", err)
	}

	// Reload through the koanf source path and confirm key fields survive.
	dir := t.TempDir()
	path := filepath.Join(dir, "server.toml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	src, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := FromSource(src)

	if got.LToUDP.SeedZone != "Custom Zone" {
		t.Errorf("LToUDP.SeedZone = %q, want %q", got.LToUDP.SeedZone, "Custom Zone")
	}
	if !got.WebUI.Enabled || got.WebUI.Bind != "127.0.0.1:9000" {
		t.Errorf("WebUI round-trip lost data: %+v", got.WebUI)
	}
	if v, ok := got.AFP.Volumes["TestVol"]; !ok || v.Path != `C:\Mac\Test` {
		t.Errorf("AFP volume round-trip lost data: %+v", got.AFP.Volumes)
	}
}

func TestCloneIsDeep(t *testing.T) {
	m := Defaults()
	m.AFP.Volumes = map[string]VolumeModel{"A": {Path: "/a"}}
	m.NetBIOS.Transports = []string{"tcp"}

	cp := m.Clone()
	cp.AFP.Volumes["A"] = VolumeModel{Path: "/changed"}
	cp.NetBIOS.Transports[0] = "ipx"

	if m.AFP.Volumes["A"].Path != "/a" {
		t.Errorf("Clone shared volume map: original mutated to %q", m.AFP.Volumes["A"].Path)
	}
	if m.NetBIOS.Transports[0] != "tcp" {
		t.Errorf("Clone shared slice: original mutated to %q", m.NetBIOS.Transports[0])
	}
}

func TestSaveCreatesNumberedBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.toml")
	if err := os.WriteFile(path, []byte("# original\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	m := Defaults()
	backup, err := Save(path, m)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	want := path + ".0001"
	if backup != want {
		t.Errorf("backup path = %q, want %q", backup, want)
	}
	if b, _ := os.ReadFile(backup); string(b) != "# original\n" {
		t.Errorf("backup content = %q, want original", string(b))
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("new config not written: %v", err)
	}

	// A second save bumps to .0002.
	backup2, err := Save(path, m)
	if err != nil {
		t.Fatalf("Save 2: %v", err)
	}
	if backup2 != path+".0002" {
		t.Errorf("second backup = %q, want .0002", backup2)
	}
}

func TestSaveNoBackupWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.toml")
	backup, err := Save(path, Defaults())
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if backup != "" {
		t.Errorf("backup = %q, want empty when no prior file", backup)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("config not written: %v", err)
	}
}

func TestRouterBindsPort(t *testing.T) {
	// Empty list binds everything (the default).
	empty := RouterModel{}
	for _, name := range []string{RouterPortLToUDP, RouterPortTashTalk, RouterPortEtherTalk} {
		if !empty.BindsPort(name) {
			t.Errorf("empty Ports: BindsPort(%q) = false, want true", name)
		}
	}

	// A non-empty list binds only the named transports; matching is
	// case-insensitive and whitespace-tolerant.
	r := RouterModel{Ports: []string{"LToUdp", " ethertalk "}}
	if !r.BindsPort(RouterPortLToUDP) {
		t.Errorf("BindsPort(LToUdp) = false, want true")
	}
	if !r.BindsPort(RouterPortEtherTalk) {
		t.Errorf("BindsPort(EtherTalk) = false, want true (case-insensitive)")
	}
	if r.BindsPort(RouterPortTashTalk) {
		t.Errorf("BindsPort(TashTalk) = true, want false (not listed)")
	}
}

func TestRouterPortsTOMLRoundTrip(t *testing.T) {
	m := Defaults()
	m.Router.Ports = []string{RouterPortLToUDP, RouterPortEtherTalk}

	data, err := m.ToTOML()
	if err != nil {
		t.Fatalf("ToTOML: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "server.toml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	src, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := FromSource(src)
	if got.Router.BindsPort(RouterPortTashTalk) {
		t.Errorf("after round-trip TashTalk still bound; Ports=%v", got.Router.Ports)
	}
	if !got.Router.BindsPort(RouterPortLToUDP) || !got.Router.BindsPort(RouterPortEtherTalk) {
		t.Errorf("after round-trip lost a bound port; Ports=%v", got.Router.Ports)
	}
}
