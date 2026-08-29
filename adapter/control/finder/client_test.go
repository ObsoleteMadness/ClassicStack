package finder

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

func TestDiscoverDisabledWhenClientOff(t *testing.T) {
	m := config.NewModel()
	m.Client.Enabled = false
	svc := New(modelStub{m: m}, nil)
	_, err := svc.Discover(DiscoverRequest{Scheme: KindAFP})
	if !errors.Is(err, ErrClientDisabled) {
		t.Fatalf("err = %v, want ErrClientDisabled", err)
	}
}

func TestDiscoverRejectsUnlistedService(t *testing.T) {
	m := config.NewModel()
	m.Client = config.ClientSection{Enabled: true, Services: []string{"afp"}}
	svc := New(modelStub{m: m}, nil)
	_, err := svc.Discover(DiscoverRequest{Scheme: KindSMB})
	if !errors.Is(err, ErrServiceDisabled) {
		t.Fatalf("err = %v, want ErrServiceDisabled", err)
	}
}

func TestConnectDisabledWhenClientOff(t *testing.T) {
	m := config.NewModel()
	svc := New(modelStub{m: m}, nil)
	_, err := svc.Connect(context.Background(), ConnectRequest{Kind: KindAFP, Target: "afp://x/"})
	if !errors.Is(err, ErrClientDisabled) {
		t.Fatalf("err = %v, want ErrClientDisabled", err)
	}
}

func TestMountDisabledWhenClientMountOff(t *testing.T) {
	m := config.NewModel()
	m.Client = config.ClientSection{Enabled: true, Mount: false}
	svc := New(modelStub{m: m}, nil)
	if svc.MountStatus().Available {
		t.Fatal("mount should be unavailable when [Client].mount is false")
	}
	_, err := svc.Mount(context.Background(), MountRequest{Kind: KindAFP, Target: "afp://x/", Volume: "HD"})
	if err == nil {
		t.Fatal("want mount error")
	}
	if !errors.Is(err, ErrMountDisabled) && !errors.Is(err, ErrMountUnavailable) {
		t.Fatalf("err = %v, want ErrMountDisabled or ErrMountUnavailable", err)
	}
}

func TestStateReadsConnectionsAndVolumes(t *testing.T) {
	m := config.NewModel()
	m.Client = config.ClientSection{Enabled: true, Iface: "br-lan", Mount: true, Services: []string{"afp", "smb"}}
	svc := New(modelStub{m: m}, nil)
	ffs, err := fs.BuildShare(fs.ShareSpec{Name: "HD", FSType: "memfs"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	svc.put(&Session{
		ID:         "abc",
		Kind:       KindAFP,
		ServerName: "Mac HD",
		Volumes:    []string{"HD"},
		Volume:     "HD",
		FS:         ffs,
		remoteURI:  "afp://Mac HD,ltoudp/",
		touched:    time.Now(),
	})
	svc.remember(KindAFP, []VolumeInfo{{ID: "afp://Mac HD,ltoudp/", Kind: KindAFP, Title: "Mac HD"}})

	st := svc.State()
	if !st.Enabled || st.Iface != "br-lan" || len(st.Services) != 2 {
		t.Fatalf("state config = %+v", st)
	}
	if len(st.Networks) != 1 || st.Networks[0].Title != "Mac HD" {
		t.Fatalf("networks = %+v", st.Networks)
	}
	if len(st.Connections) != 1 || st.Connections[0].SessionID != "abc" {
		t.Fatalf("connections = %+v", st.Connections)
	}
	if len(st.Volumes) != 1 || st.Volumes[0].Volume != "HD" {
		t.Fatalf("volumes = %+v", st.Volumes)
	}
}

func TestStartDisabledIsNoop(t *testing.T) {
	m := config.NewModel()
	svc := New(modelStub{m: m}, nil)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := svc.State()
	if st.Enabled {
		t.Fatal("disabled client should report enabled=false")
	}
	if st.Scanning {
		t.Fatal("disabled client should not be scanning")
	}
}

func TestScanLoopExitsOnCancel(t *testing.T) {
	svc := New(modelStub{m: config.NewModel()}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		svc.scanLoop(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scanLoop did not exit after context cancellation (goroutine leak on Stop)")
	}
}

func TestNewBindsClientIface(t *testing.T) {
	m := config.NewModel()
	m.SetInterface(config.InterfaceSection{Name: "br-lan", Kind: config.IfaceKindBridge, Device: "en0", Default: true})
	m.SetInterface(config.InterfaceSection{Name: "other", Kind: config.IfaceKindBridge, Device: "eth1"})
	m.Client = config.ClientSection{Enabled: true, Iface: "other"}
	svc := New(modelStub{m: m}, nil)
	got := svc.configuredInterface()
	if got.Name != "other" || got.Device != "eth1" {
		t.Fatalf("client iface = %+v, want other/eth1", got)
	}
}

func TestSessionIdleFromClient(t *testing.T) {
	m := config.NewModel()
	m.Client = config.ClientSection{Enabled: true, MaxIdleMinutes: 3}
	svc := New(modelStub{m: m}, nil)
	if got := svc.sessionIdle(); got != 3*time.Minute {
		t.Fatalf("idle = %s, want 3m", got)
	}
}
