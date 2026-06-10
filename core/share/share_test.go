package share

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

func TestBuild_ExposesFSAndConfig(t *testing.T) {
	spec := fs.ShareSpec{
		Name:          "Media",
		FSType:        "memfs",
		ForkBackend:   "appledouble",
		FilenameCodec: "macroman-utf8",
		ReadOnly:      true,
	}
	sh, err := Build(spec, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if sh.Name() != "Media" {
		t.Fatalf("Name = %q, want Media", sh.Name())
	}
	if sh.FS() == nil {
		t.Fatal("FS() is nil")
	}
	if !sh.ReadOnly() {
		t.Fatal("ReadOnly() = false, want true")
	}
	if sh.Config().FSType != "memfs" {
		t.Fatalf("Config().FSType = %q, want memfs", sh.Config().FSType)
	}
	if sh.Codec() == nil {
		t.Fatal("Codec() is nil")
	}
	// Permissions is a stub: world-accessible until enforcement lands.
	if !sh.Permissions().AllowsGuest() {
		t.Fatal("stub Permissions should allow guest")
	}
}

// TestBuild_InvalidSpecFailsLoudly asserts Build surfaces the fs.BuildShare
// validation (here, an unknown fs_type) rather than returning a broken share.
func TestBuild_InvalidSpecFailsLoudly(t *testing.T) {
	if _, err := Build(fs.ShareSpec{Name: "x", FSType: "no-such-fs"}, nil); err == nil {
		t.Fatal("expected unknown fs_type to fail Build")
	}
}

func TestFS_ExposesCatalogOps(t *testing.T) {
	sh, err := Build(fs.ShareSpec{Name: "W", FSType: "memfs"}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// The catalog surface is the FS, reached via FS() — the Share does not mirror it.
	if _, err := sh.FS().CreateFile("note"); err != nil {
		t.Fatalf("FS().CreateFile: %v", err)
	}
	if _, err := sh.FS().Stat("note"); err != nil {
		t.Fatalf("FS().Stat: %v", err)
	}
}

func TestInfoOf(t *testing.T) {
	sh, _ := Build(fs.ShareSpec{Name: "Users", FSType: "memfs", ReadOnly: true}, nil)
	sh.SetDescription("home dirs")
	got := InfoOf(sh)
	want := Info{Name: "Users", FSType: "memfs", Description: "home dirs", ReadOnly: true}
	if got != want {
		t.Fatalf("InfoOf = %+v, want %+v", got, want)
	}
}
