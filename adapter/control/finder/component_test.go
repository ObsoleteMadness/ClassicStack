package finder

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

func TestStopUnmountsLiveMounts(t *testing.T) {
	svc := New(nil, nil)
	var unmounts atomic.Int32
	svc.mu.Lock()
	svc.mounts["m1"] = &liveMount{
		info: MountInfo{ID: "m1", Mountpoint: "/Volumes/Test"},
		unmount: func() {
			unmounts.Add(1)
		},
	}
	svc.mu.Unlock()

	if err := svc.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if unmounts.Load() != 1 {
		t.Fatalf("unmount calls = %d, want 1", unmounts.Load())
	}
	svc.mu.Lock()
	n := len(svc.mounts)
	svc.mu.Unlock()
	if n != 0 {
		t.Fatalf("mounts left = %d, want 0", n)
	}
}

func TestStopClosesRemoteSessions(t *testing.T) {
	svc := New(nil, nil)
	ffs, err := fs.BuildShare(fs.ShareSpec{Name: "HD", FSType: "memfs"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	svc.put(&Session{
		ID:         "remote",
		Kind:       KindAFP,
		ServerName: "Mac",
		FS:         ffs,
		touched:    time.Now(),
	})

	if err := svc.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	svc.mu.Lock()
	n := len(svc.sess)
	svc.mu.Unlock()
	if n != 0 {
		t.Fatalf("sessions left = %d, want 0", n)
	}
}

func TestStopIsIdempotent(t *testing.T) {
	svc := New(nil, nil)
	if err := svc.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := svc.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestStartAfterStop(t *testing.T) {
	svc := New(nil, nil)
	if err := svc.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := svc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := svc.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}
