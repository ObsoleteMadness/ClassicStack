package finder

import (
	"errors"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

func TestMountRequestFromVolume(t *testing.T) {
	req, err := mountRequestFromVolume(&config.FUSEVolumeSection{
		Remote:     "smb://foo:pass@foohost,smb/share",
		Mountpoint: "/Volumes/share",
		ReadOnly:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if req.Kind != KindSMB || req.Volume != "share" || req.User != "foo" || req.Password != "pass" {
		t.Fatalf("creds/kind: %+v", req)
	}
	if req.Guest || !req.ReadOnly || req.Mountpoint != "/Volumes/share" {
		t.Fatalf("flags: %+v", req)
	}
	if req.IfaceType != "smb" {
		t.Fatalf("transport = %q", req.IfaceType)
	}

	_, err = mountRequestFromVolume(&config.FUSEVolumeSection{Remote: "smb://host/", Mountpoint: "/mnt/x"})
	if err == nil {
		t.Fatal("empty volume should fail")
	}
	_, err = mountRequestFromVolume(&config.FUSEVolumeSection{Remote: "not-a-uri", Mountpoint: "/mnt/x"})
	if err == nil {
		t.Fatal("bad URI should fail")
	}
}

func TestRetryableAutoMount(t *testing.T) {
	if retryableAutoMount(ErrMountUnavailable) || retryableAutoMount(ErrMountDisabled) ||
		retryableAutoMount(ErrClientDisabled) || retryableAutoMount(ErrLocalMount) {
		t.Fatal("config/platform errors must not retry")
	}
	if !retryableAutoMount(errors.New("connection refused")) {
		t.Fatal("connect errors should retry")
	}
}

func TestAutoMountSkippedWhenClientOff(t *testing.T) {
	m := config.NewModel()
	m.Client.Enabled = false
	m.AddInstance(&config.FUSEVolumeSection{Remote: "smb://h/s", Mountpoint: "/mnt/s"})
	svc := New(modelStub{m: m}, nil)
	svc.autoMountAll(t.Context())
	if n := len(svc.MountStatus().Mounts); n != 0 {
		t.Fatalf("disabled client mounted %d volumes", n)
	}
}

func TestFuseConfigTimeout(t *testing.T) {
	m := config.NewModel()
	m.FUSE.MountTimeoutSeconds = 7
	svc := New(modelStub{m: m}, nil)
	if got := svc.fuseConfig().MountTimeout().Seconds(); got != 7 {
		t.Fatalf("timeout = %v, want 7s", got)
	}
}
