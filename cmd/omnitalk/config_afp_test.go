//go:build afp || all

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pgodw/omnitalk/config"
	"github.com/pgodw/omnitalk/service/afp"
)

// loadAFPForTest is a small helper that mirrors what wireAFP does on
// the config-file path: load the TOML source and unmarshal [AFP] into
// an afp.Config, applying the same path resolution.
func loadAFPForTest(t *testing.T, path string) afp.Config {
	t.Helper()
	src, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	cfg := afp.DefaultConfig()
	if err := loadAFPSection(src, &cfg); err != nil {
		t.Fatalf("loadAFPSection: %v", err)
	}
	return cfg
}

func TestLoadAFPConfig_VolumesAndExtensionMap(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "server.toml")
	content := `[AFP]
enabled = true
name = "OmniTalk"
zone = "EtherTalk Network"
protocols = "ddp,tcp"
binding = ":548"
extension_map = "extmap.conf"
cnid_backend = "memory"
use_decomposed_names = true

[AFP.Volumes.Main]
name = "Main"
path = 'C:\Mac'
appledouble_mode = "legacy"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg := loadAFPForTest(t, cfgPath)
	if cfg.ExtensionMap != filepath.Join(dir, "extmap.conf") {
		t.Fatalf("ExtensionMap = %q, want %q", cfg.ExtensionMap, filepath.Join(dir, "extmap.conf"))
	}
	if cfg.CNIDBackend != "memory" {
		t.Fatalf("CNIDBackend = %q", cfg.CNIDBackend)
	}
	if !cfg.UseDecomposedNames {
		t.Fatal("UseDecomposedNames = false")
	}
	vols, err := cfg.ResolvedVolumes()
	if err != nil {
		t.Fatalf("ResolvedVolumes: %v", err)
	}
	if len(vols) != 1 || vols[0].Path != `C:\Mac` {
		t.Fatalf("unexpected volumes: %#v", vols)
	}
	if vols[0].AppleDoubleMode != afp.AppleDoubleModeLegacy {
		t.Fatalf("AppleDoubleMode = %q", vols[0].AppleDoubleMode)
	}
}

func TestLoadAFPConfig_PerVolumeAppleDoubleMode(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "server.toml")
	content := `[AFP.Volumes.Modern]
name = "Modern"
path = "/tmp/modern"
appledouble_mode = "modern"

[AFP.Volumes.Legacy]
name = "Legacy"
path = "/tmp/legacy"
appledouble_mode = "legacy"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg := loadAFPForTest(t, cfgPath)
	vols, err := cfg.ResolvedVolumes()
	if err != nil {
		t.Fatalf("ResolvedVolumes: %v", err)
	}
	if len(vols) != 2 {
		t.Fatalf("want 2 vols, got %d", len(vols))
	}
	byName := map[string]afp.VolumeConfig{}
	for _, v := range vols {
		byName[v.Name] = v
	}
	if byName["Modern"].AppleDoubleMode != afp.AppleDoubleModeModern {
		t.Fatalf("Modern AppleDoubleMode = %q", byName["Modern"].AppleDoubleMode)
	}
	if byName["Legacy"].AppleDoubleMode != afp.AppleDoubleModeLegacy {
		t.Fatalf("Legacy AppleDoubleMode = %q", byName["Legacy"].AppleDoubleMode)
	}
}

func TestLoadAFPConfig_PerVolumeFSType(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "server.toml")
	content := `[AFP.Volumes.Local]
name = "Local"
path = 'C:\Mac\Local'
fs_type = "local_fs"

[AFP.Volumes.Garden]
name = "Garden"
path = 'C:\Mac\Garden'
fs_type = "macgarden"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg := loadAFPForTest(t, cfgPath)
	vols, err := cfg.ResolvedVolumes()
	if err != nil {
		t.Fatalf("ResolvedVolumes: %v", err)
	}
	if len(vols) != 2 {
		t.Fatalf("want 2 vols, got %d", len(vols))
	}
	byName := map[string]afp.VolumeConfig{}
	for _, v := range vols {
		byName[v.Name] = v
	}
	if byName["Local"].FSType != afp.FSTypeLocalFS {
		t.Fatalf("Local fs_type = %q", byName["Local"].FSType)
	}
	if byName["Garden"].FSType != afp.FSTypeMacGarden {
		t.Fatalf("Garden fs_type = %q", byName["Garden"].FSType)
	}
}

func TestLoadAFPConfig_InvalidFSType(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "server.toml")
	content := `[AFP.Volumes.Bad]
name = "Bad"
path = 'C:\Mac\Bad'
fs_type = "bananas"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	src, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	cfg := afp.DefaultConfig()
	if err := loadAFPSection(src, &cfg); err == nil {
		t.Fatal("expected invalid fs_type error")
	}
}

func TestLoadAFPConfig_MacGardenWithoutPath(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "server.toml")
	content := `[AFP.Volumes.MacGarden]
name = "Mac Garden"
fs_type = "macgarden"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg := loadAFPForTest(t, cfgPath)
	vols, err := cfg.ResolvedVolumes()
	if err != nil {
		t.Fatalf("ResolvedVolumes: %v", err)
	}
	if len(vols) != 1 {
		t.Fatalf("want 1 vol, got %d", len(vols))
	}
	if vols[0].FSType != afp.FSTypeMacGarden {
		t.Fatalf("fs_type = %q", vols[0].FSType)
	}
	if got, want := filepath.ToSlash(vols[0].Path), ".macgarden/Mac_Garden"; got != want {
		t.Fatalf("generated path = %q, want %q", got, want)
	}
}

func TestLoadAFPConfig_LocalFSWithoutPathStillFails(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "server.toml")
	content := `[AFP.Volumes.Local]
name = "Local"
fs_type = "local_fs"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	src, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	cfg := afp.DefaultConfig()
	if err := loadAFPSection(src, &cfg); err == nil {
		t.Fatal("expected path required error for local_fs")
	}
}
