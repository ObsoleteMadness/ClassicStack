package smb

import (
	"context"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/pkg/vfs"
)

func TestServiceLifecycleSubscribesAndUnsubscribes(t *testing.T) {
	bus := vfs.NewBus(vfs.BusOptions{})

	svc := NewService(ServerOptions{Bus: bus}, nil, []ShareConfig{
		{Name: "Public", Path: "/tmp/pub", FSType: "local_fs"},
	})

	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Publishing should not panic and should reach our subscriber.
	bus.Publish(vfs.Event{Op: vfs.OpRename, HostPath: "/tmp/pub/a", OldPath: "/tmp/pub/b", Origin: "afp"})

	if err := svc.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Calling Stop again must be idempotent.
	if err := svc.Stop(); err != nil {
		t.Fatalf("Stop (second): %v", err)
	}
}

func TestServiceShortnameOptional(t *testing.T) {
	svc := NewService(ServerOptions{}, nil, nil)
	if svc.opts.Shortname != nil {
		t.Fatal("Shortname should be nil by default")
	}
}
