package finder

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"

	smbclient "github.com/ObsoleteMadness/ClassicStack/client/smb"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

func TestIsConnectionLost(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"not found", ErrNotFound, false},
		{"benign fork EOF", io.EOF, false},
		{"wrapped fork EOF", errors.New("read fork: " + io.EOF.Error()), false},
		{"transport closed", smbclient.ErrTransportClosed, true},
		{"wrapped transport closed", errors.Join(errors.New("smb: read"), smbclient.ErrTransportClosed), true},
		{"net closed", net.ErrClosed, true},
		{"raw net error", &net.OpError{Op: "read", Err: errors.New("connection reset by peer")}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isConnectionLost(c.err); got != c.want {
				t.Fatalf("isConnectionLost(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}

func TestInvalidateOnErrorDropsDeadSession(t *testing.T) {
	ffs, err := fs.BuildShare(fs.ShareSpec{Name: "HD", FSType: "memfs"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	svc := New(nil, nil)
	svc.put(&Session{
		ID: "abc", Kind: KindAFP, ServerName: "Mac", Volume: "HD", FS: ffs,
		remoteURI: "afp://Mac,ltoudp/", touched: time.Now(),
	})

	if got := svc.InvalidateOnError("abc", errors.New("file not found")); got {
		t.Fatal("a benign error must not invalidate the session")
	}
	if _, err := svc.get("abc"); err != nil {
		t.Fatalf("benign error dropped session: %v", err)
	}

	if got := svc.InvalidateOnError("abc", smbclient.ErrTransportClosed); !got {
		t.Fatal("a transport-closed error must invalidate the session")
	}
	if _, err := svc.get("abc"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("session still present after invalidation: err=%v", err)
	}
	if got := svc.MountedVolumes(); len(got) != 0 {
		t.Fatalf("invalidated session still reported as mounted: %+v", got)
	}
}

func TestInvalidateOnErrorUnmountsHostMount(t *testing.T) {
	ffs, err := fs.BuildShare(fs.ShareSpec{Name: "HD", FSType: "memfs"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	svc := New(nil, nil)
	svc.mounts["m1"] = &liveMount{
		info: MountInfo{ID: "m1", Mountpoint: "/Volumes/HD", Volume: "HD", Kind: KindAFP, Server: "Mac"},
		fsys: ffs,
	}
	// MountedVolumes() lazily creates the backing browse session for a host mount.
	if got := svc.MountedVolumes(); len(got) != 1 {
		t.Fatalf("got %+v, want 1", got)
	}

	if got := svc.InvalidateOnError("m1", smbclient.ErrTransportClosed); !got {
		t.Fatal("expected invalidation")
	}
	if got := svc.MountedVolumes(); len(got) != 0 {
		t.Fatalf("host mount still reported after invalidation: %+v", got)
	}
	if _, ok := svc.mounts["m1"]; ok {
		t.Fatal("host mount entry not removed")
	}
}

func TestInvalidateOnErrorNoSession(t *testing.T) {
	svc := New(nil, nil)
	if got := svc.InvalidateOnError("", smbclient.ErrTransportClosed); got {
		t.Fatal("empty sessionID must be a no-op")
	}
	if got := svc.InvalidateOnError("missing", smbclient.ErrTransportClosed); got {
		t.Fatal("unknown sessionID must be a no-op")
	}
}
