package finder

import (
	"context"
	"errors"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

func TestSanitizeMountName(t *testing.T) {
	if got := sanitizeMountName("Mac HD"); got != "Mac HD" {
		t.Fatalf("got %q", got)
	}
	if got := sanitizeMountName("OpenRetroSCSI 7.5.3"); got != "OpenRetroSCSI 7.5.3" {
		t.Fatalf("got %q", got)
	}
	if got := sanitizeMountName(`foo/bar:baz`); got != "foo_bar_baz" {
		t.Fatalf("got %q", got)
	}
	if got := sanitizeMountName("   "); got != "ClassicStack" {
		t.Fatalf("got %q", got)
	}
}

func TestIsDarwinVolumesLeaf(t *testing.T) {
	if !isDarwinVolumesLeaf("/Volumes/OpenRetroSCSI 7.5.3") {
		t.Fatal("spaced /Volumes leaf should be created by macFUSE")
	}
	if !isDarwinVolumesLeaf("/Volumes/Classic") {
		t.Fatal("/Volumes/Classic should be created by macFUSE")
	}
	for _, p := range []string{"/Volumes", "/Volumes/", "/Volumes/foo/bar", "/mnt/vol", "Volumes/Classic"} {
		if isDarwinVolumesLeaf(p) {
			t.Fatalf("%q must not be treated as a macFUSE /Volumes leaf", p)
		}
	}
}

func TestPrepareMountpointSkipsVolumesOnDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macFUSE /Volumes auto-create is Darwin-only")
	}
	point := "/Volumes/ClassicStack-prepare-test-do-not-create"
	if err := prepareMountpoint(point); err != nil {
		t.Fatalf("prepareMountpoint: %v", err)
	}
	if _, err := os.Stat(point); err == nil {
		t.Fatalf("prepareMountpoint must not mkdir %s", point)
	}
}

func TestPrepareMountpointCreatesElsewhere(t *testing.T) {
	dir := t.TempDir() + "/mnt/vol"
	if err := prepareMountpoint(dir); err != nil {
		t.Fatalf("prepareMountpoint: %v", err)
	}
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		t.Fatalf("expected directory at %s: %v", dir, err)
	}
}

func TestMountRejectsLocal(t *testing.T) {
	svc := New(nil, nil)
	_, err := svc.Mount(t.Context(), MountRequest{Kind: KindLocal, ID: "local:afp:HD", Volume: "HD", Mountpoint: t.TempDir()})
	if !errors.Is(err, ErrLocalMount) && !errors.Is(err, ErrMountUnavailable) {
		t.Fatalf("err = %v", err)
	}
	_, err = svc.Mount(t.Context(), MountRequest{Kind: KindAFP, ID: "local:afp:HD", Volume: "HD", Mountpoint: t.TempDir()})
	if !errors.Is(err, ErrLocalMount) && !errors.Is(err, ErrMountUnavailable) {
		t.Fatalf("local id err = %v", err)
	}
}

func TestDefaultMountDir(t *testing.T) {
	if DefaultMountDir() == "" {
		t.Fatal("empty default mount dir")
	}
	if runtime.GOOS == "darwin" && DefaultMountDir() != DarwinVolumesDir {
		t.Fatalf("DefaultMountDir = %q, want %q", DefaultMountDir(), DarwinVolumesDir)
	}
}

func TestMountFSReusesOpenBrowseSession(t *testing.T) {
	ffs, err := fs.BuildShare(fs.ShareSpec{Name: "HD", FSType: "memfs"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	svc := New(nil, nil)
	svc.put(&Session{
		ID: "browse", Kind: KindAFP, ServerName: "Mac", Volume: "HD", FS: ffs,
		remoteURI: "afp://Mac,ltoudp/", Volumes: []string{"HD"}, touched: time.Now(),
	})
	_, vol, _, got, reused, err := svc.mountFS(context.Background(), MountRequest{
		SessionID: "browse", Volume: "HD", Kind: KindAFP,
	})
	if err != nil {
		t.Fatalf("mountFS: %v", err)
	}
	if !reused {
		t.Fatal("want reused=true")
	}
	if got != ffs {
		t.Fatal("want same ForkFS pointer")
	}
	if vol != "HD" {
		t.Fatalf("volume = %q", vol)
	}
	sess, err := svc.get("browse")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !sess.hostMount {
		t.Fatal("browse session should be marked hostMount")
	}
}

func TestMountFSSkipsReuseWhenVolumeDiffers(t *testing.T) {
	ffs, err := fs.BuildShare(fs.ShareSpec{Name: "HD", FSType: "memfs"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	svc := New(nil, nil)
	svc.put(&Session{
		ID: "browse", Kind: KindAFP, ServerName: "Mac", Volume: "HD", FS: ffs,
		remoteURI: "afp://Mac,ltoudp/", touched: time.Now(),
	})
	_, _, _, _, reused, err := svc.mountFS(context.Background(), MountRequest{
		SessionID: "browse", Volume: "Public", Kind: KindAFP,
	})
	if err == nil {
		t.Fatal("expected dial error for unmatched volume")
	}
	if reused {
		t.Fatal("must not reuse FS for a different volume")
	}
}
