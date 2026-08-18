package fuse

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveMountpointExpandsTildeAndKeepsSpaces(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}

	got, err := ResolveMountpoint("~/Volumes/OpenRetroSCSI 7.5.3")
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(filepath.Join(home, "Volumes", "OpenRetroSCSI 7.5.3"))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if filepath.Base(got) != "OpenRetroSCSI 7.5.3" {
		t.Fatalf("base = %q (space was split)", filepath.Base(got))
	}
}

func TestResolveMountpointMkdirAllIsOneLeaf(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
	got, err := ResolveMountpoint("~/Volumes/OpenRetroSCSI 7.5.3")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(got, 0o755); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Dir(got)
	if _, err := os.Stat(filepath.Join(parent, "7.5.3")); err == nil {
		t.Fatal("space split: sibling 7.5.3 exists")
	}
	st, err := os.Stat(got)
	if err != nil || !st.IsDir() {
		t.Fatalf("expected %q: %v", got, err)
	}
}

func TestResolveMountpointRejectsEmpty(t *testing.T) {
	if _, err := ResolveMountpoint("  "); err == nil {
		t.Fatal("expected error")
	}
}

func TestExpandHomeLeavesAbsolute(t *testing.T) {
	const in = "/Volumes/OpenRetroSCSI 7.5.3"
	got, err := expandHome(in)
	if err != nil {
		t.Fatal(err)
	}
	if got != in {
		t.Fatalf("got %q", got)
	}
}
