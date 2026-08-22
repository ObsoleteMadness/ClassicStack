package runport

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
)

// blockingCloseLink is a DatagramLink whose Close blocks until released — the
// failure mode a stuck serial driver exhibits during TashTalk shutdown.
type blockingCloseLink struct {
	block   chan struct{}
	closed  bool
	closeMu sync.Mutex
}

func newBlockingCloseLink() *blockingCloseLink {
	return &blockingCloseLink{block: make(chan struct{})}
}

func (b *blockingCloseLink) ReadDatagram() (ddp.Datagram, error) {
	select {
	case <-b.block:
		return ddp.Datagram{}, link.ErrClosed
	default:
	}
	return ddp.Datagram{}, link.ErrTimeout
}

func (b *blockingCloseLink) WriteDatagram(ddp.Datagram) error { return nil }

func (b *blockingCloseLink) Close() error {
	b.closeMu.Lock()
	defer b.closeMu.Unlock()
	if b.closed {
		return nil
	}
	<-b.block // simulate a driver that never returns from Close
	b.closed = true
	return nil
}

func (b *blockingCloseLink) releaseClose() { close(b.block) }

// TestStopHonoursCloseDeadline verifies Stop returns when ctx expires even if
// DatagramLink.Close is blocked (serial shutdown path).
func TestStopHonoursCloseDeadline(t *testing.T) {
	dl := newBlockingCloseLink()
	p := New(&port.Section{SKey: "TashTalk"}, func() (link.DatagramLink, error) { return dl, nil }, nil, nil)
	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := p.Stop(ctx)
	if err == nil {
		t.Fatal("Stop = nil, want context deadline error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop err = %v, want context.DeadlineExceeded", err)
	}

	dl.releaseClose()
}
