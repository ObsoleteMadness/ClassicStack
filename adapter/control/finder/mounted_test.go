package finder

import (
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

func TestMountedVolumesEmpty(t *testing.T) {
	svc := New(nil, nil)
	if got := svc.MountedVolumes(); len(got) != 0 {
		t.Fatalf("got %+v, want empty", got)
	}
}

func TestMountedVolumesListsOpenRemoteVolume(t *testing.T) {
	ffs, err := fs.BuildShare(fs.ShareSpec{Name: "HD", FSType: "memfs"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	svc := New(nil, nil)
	root := ffs.Meta().EnsureCNID("")
	svc.put(&Session{
		ID:         "abc",
		Kind:       KindAFP,
		ServerName: "Mac HD",
		Volumes:    []string{"HD"},
		Volume:     "HD",
		FS:         ffs,
		remoteURI:  "afp://Mac HD,ltoudp/",
		transport:  "ltoudp",
		touched:    time.Now(),
	})
	got := svc.MountedVolumes()
	if len(got) != 1 {
		t.Fatalf("got %+v, want 1", got)
	}
	m := got[0]
	if m.SessionID != "abc" || m.Volume != "HD" || m.ServerName != "Mac HD" || m.Kind != KindAFP {
		t.Fatalf("mounted = %+v", m)
	}
	if m.RootID != root {
		t.Fatalf("rootId = %d, want %d", m.RootID, root)
	}
	if m.Target != "afp://Mac HD,ltoudp/" || m.Transport != "ltoudp" {
		t.Fatalf("target/transport = %+v", m)
	}
}

func TestMountedVolumesOmitsLocalAndUnopened(t *testing.T) {
	ffs, err := fs.BuildShare(fs.ShareSpec{Name: "Mem", FSType: "memfs"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	svc := New(nil, nil)
	svc.put(&Session{ID: "local", Kind: KindLocal, Volume: "Mem", FS: ffs, local: true, touched: time.Now()})
	svc.put(&Session{ID: "login", Kind: KindAFP, ServerName: "X", Volumes: []string{"HD"}, remoteURI: "afp://X/", touched: time.Now()})
	if got := svc.MountedVolumes(); len(got) != 0 {
		t.Fatalf("got %+v, want empty (local + not yet opened)", got)
	}
}

func TestReapIdleKeepsMountedVolume(t *testing.T) {
	ffs, err := fs.BuildShare(fs.ShareSpec{Name: "HD", FSType: "memfs"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	svc := New(nil, nil)
	svc.put(&Session{
		ID: "keep", Kind: KindAFP, ServerName: "X", Volume: "HD", FS: ffs,
		touched: time.Now().Add(-time.Hour),
	})
	svc.put(&Session{
		ID: "drop", Kind: KindAFP, ServerName: "Y", Volumes: []string{"Z"},
		touched: time.Now().Add(-time.Hour),
	})
	svc.reapIdle()
	if _, err := svc.get("keep"); err != nil {
		t.Fatalf("mounted session reaped: %v", err)
	}
	if _, err := svc.get("drop"); err != ErrNotFound {
		t.Fatalf("idle login err = %v, want ErrNotFound", err)
	}
}

func TestConnectReusesMountedSession(t *testing.T) {
	ffs, err := fs.BuildShare(fs.ShareSpec{Name: "HD", FSType: "memfs"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	svc := New(nil, nil)
	svc.put(&Session{
		ID: "abc", Kind: KindAFP, ServerName: "Mac", Volume: "HD", FS: ffs,
		remoteURI: "afp://Mac,ltoudp/", Volumes: []string{"HD", "Public"}, touched: time.Now(),
	})
	info, err := svc.Connect(t.Context(), ConnectRequest{Kind: KindAFP, Target: "afp://Mac,ltoudp/"})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if info.SessionID != "abc" {
		t.Fatalf("session %q, want reused abc", info.SessionID)
	}
	if info.Volume != "" || info.RootID != 0 {
		t.Fatalf("connect reused volume catalog: %+v", info)
	}
	if len(info.Volumes) != 2 || info.Volumes[0] != "HD" || info.Volumes[1] != "Public" {
		t.Fatalf("volumes = %v, want HD and Public", info.Volumes)
	}
	if !info.AllowGuest {
		t.Fatal("reused login should skip the Finder password prompt")
	}
}

func TestConnectPrefersLoginSession(t *testing.T) {
	ffs, err := fs.BuildShare(fs.ShareSpec{Name: "HD", FSType: "memfs"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	svc := New(nil, nil)
	svc.put(&Session{
		ID: "vol", Kind: KindAFP, ServerName: "Mac", Volume: "HD", FS: ffs,
		remoteURI: "afp://Mac,ltoudp/", Volumes: []string{"HD", "Public"}, touched: time.Now(),
	})
	svc.put(&Session{
		ID: "login", Kind: KindAFP, ServerName: "Mac",
		remoteURI: "afp://Mac,ltoudp/", Volumes: []string{"HD", "Public"}, touched: time.Now(),
	})
	info, err := svc.Connect(t.Context(), ConnectRequest{Kind: KindAFP, Target: "afp://Mac,ltoudp/"})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if info.SessionID != "login" {
		t.Fatalf("session %q, want login (not the open volume)", info.SessionID)
	}
}

func TestConnectDoesNotReuseDifferentServerURI(t *testing.T) {
	ffs, err := fs.BuildShare(fs.ShareSpec{Name: "HD", FSType: "memfs"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	svc := New(nil, nil)
	svc.put(&Session{
		ID: "abc", Kind: KindAFP, ServerName: "Macintosh HD", Volume: "HD", FS: ffs,
		remoteURI: "afp://Macintosh HD:ZoneA,pcap/", Volumes: []string{"HD"}, touched: time.Now(),
	})
	if got := svc.existingMounted(KindAFP, "afp://Macintosh HD:ZoneB,pcap/"); got != nil {
		t.Fatalf("matched other zone: %+v", got.info())
	}
}

func TestOpenVolumeReusesExistingVolumeSession(t *testing.T) {
	hd, err := fs.BuildShare(fs.ShareSpec{Name: "HD", FSType: "memfs"}, nil)
	if err != nil {
		t.Fatalf("HD: %v", err)
	}
	pub, err := fs.BuildShare(fs.ShareSpec{Name: "Public", FSType: "memfs"}, nil)
	if err != nil {
		t.Fatalf("Public: %v", err)
	}
	svc := New(nil, nil)
	svc.put(&Session{
		ID: "s1", Kind: KindAFP, ServerName: "Mac", Volume: "HD", FS: hd,
		remoteURI: "afp://Mac,ltoudp/", Volumes: []string{"HD", "Public"}, touched: time.Now(),
	})
	svc.put(&Session{
		ID: "s2", Kind: KindAFP, ServerName: "Mac", Volume: "Public", FS: pub,
		remoteURI: "afp://Mac,ltoudp/", Volumes: []string{"HD", "Public"}, touched: time.Now(),
	})
	info, err := svc.OpenVolume("s1", "Public")
	if err != nil {
		t.Fatalf("OpenVolume: %v", err)
	}
	if info.SessionID != "s2" || info.Volume != "Public" {
		t.Fatalf("opened %+v, want existing Public session s2", info)
	}
	s1, err := svc.get("s1")
	if err != nil {
		t.Fatalf("s1: %v", err)
	}
	if s1.Volume != "HD" || s1.FS != hd {
		t.Fatalf("s1 clobbered: volume=%q fs=%v", s1.Volume, s1.FS != hd)
	}
}

func TestSameMountTargetURIIsNotServerName(t *testing.T) {
	sess := &Session{ID: "abc", ServerName: "Macintosh HD", remoteURI: "afp://Macintosh HD:ZoneA,pcap/"}
	if !sameMountTarget(sess, "afp://Macintosh HD:ZoneA,pcap/") {
		t.Fatal("URI should match remoteURI")
	}
	if sameMountTarget(sess, "afp://Macintosh HD:ZoneB,pcap/") {
		t.Fatal("other zone URI should not match")
	}
	if sameMountTarget(sess, "afp://Macintosh HD,pcap/") {
		t.Fatal("URI must not match on display name alone")
	}
	if !sameMountTarget(sess, "Macintosh HD") {
		t.Fatal("bare server name should match ServerName")
	}
	if !sameMountTarget(sess, "mounted:abc") {
		t.Fatal("mounted: id should match session id")
	}
}

func TestCloseVolumeKeepsLoginSession(t *testing.T) {
	ffs, err := fs.BuildShare(fs.ShareSpec{Name: "HD", FSType: "memfs"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	svc := New(nil, nil)
	svc.put(&Session{
		ID: "abc", Kind: KindAFP, ServerName: "Mac", Volume: "HD", FS: ffs,
		remoteURI: "afp://Mac,ltoudp/", Volumes: []string{"HD", "Public"}, touched: time.Now(),
	})
	if err := svc.CloseVolume("abc", "HD"); err != nil {
		t.Fatalf("CloseVolume: %v", err)
	}
	sess, err := svc.get("abc")
	if err != nil {
		t.Fatalf("login session dropped: %v", err)
	}
	if sess.FS != nil || sess.Volume != "" {
		t.Fatalf("volume still open: fs=%v volume=%q", sess.FS != nil, sess.Volume)
	}
	if len(sess.Volumes) != 2 {
		t.Fatalf("volumes = %v, want login list kept", sess.Volumes)
	}
	if got := svc.MountedVolumes(); len(got) != 0 {
		t.Fatalf("mounted after eject = %+v", got)
	}
}

func TestMountedVolumesIncludesHostMount(t *testing.T) {
	ffs, err := fs.BuildShare(fs.ShareSpec{Name: "HD", FSType: "memfs"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	svc := New(nil, nil)
	svc.mounts["m1"] = &liveMount{
		info: MountInfo{ID: "m1", Mountpoint: "/Volumes/HD", Volume: "HD", Kind: KindAFP, Server: "Mac"},
		fsys: ffs,
	}
	got := svc.MountedVolumes()
	if len(got) != 1 {
		t.Fatalf("got %+v, want 1", got)
	}
	if got[0].SessionID != "m1" || got[0].Volume != "HD" || got[0].Mountpoint != "/Volumes/HD" {
		t.Fatalf("mounted = %+v", got[0])
	}
	if _, err := svc.get("m1"); err != nil {
		t.Fatalf("browse session for host mount: %v", err)
	}
}
