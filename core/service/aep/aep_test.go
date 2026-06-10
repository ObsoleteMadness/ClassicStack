package aep

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

// fakeRouter is a minimal router.ServiceRouter that records replies. AEP only needs Reply.
type fakeRouter struct {
	mu      sync.Mutex
	replies []reply
}

type reply struct {
	ddpType uint8
	data    []byte
}

func (f *fakeRouter) Reply(_ ddp.Datagram, _ router.RoutedPort, ddpType uint8, data []byte) {
	f.mu.Lock()
	f.replies = append(f.replies, reply{ddpType: ddpType, data: append([]byte(nil), data...)})
	f.mu.Unlock()
}
func (f *fakeRouter) Route(ddp.Datagram, bool) error      { return nil }
func (f *fakeRouter) RoutingTable() *router.RoutingTable  { return nil }
func (f *fakeRouter) Zones() *router.ZoneInformationTable { return nil }
func (f *fakeRouter) Ports() []router.RoutedPort          { return nil }

func (f *fakeRouter) waitReplies(n int) []reply {
	for range 2000 {
		f.mu.Lock()
		got := len(f.replies)
		f.mu.Unlock()
		if got >= n {
			break
		}
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]reply(nil), f.replies...)
}

func TestEchoRequestReflected(t *testing.T) {
	fr := &fakeRouter{}
	s := New(fr, nil)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop(context.Background())

	s.Inbound(ddp.Datagram{DDPType: DDPType, Data: []byte{CmdRequest, 0xDE, 0xAD}}, nil)

	got := fr.waitReplies(1)
	if len(got) != 1 {
		t.Fatalf("got %d replies, want 1", len(got))
	}
	if got[0].ddpType != DDPType {
		t.Errorf("reply ddpType = %d, want %d", got[0].ddpType, DDPType)
	}
	want := []byte{CmdReply, 0xDE, 0xAD}
	if string(got[0].data) != string(want) {
		t.Errorf("reply data = %v, want %v", got[0].data, want)
	}
}

func TestNonRequestIgnored(t *testing.T) {
	fr := &fakeRouter{}
	s := New(fr, nil)
	_ = s.Start(context.Background())
	defer s.Stop(context.Background())

	// A reply (not a request) and a wrong DDP type are both ignored.
	s.Inbound(ddp.Datagram{DDPType: DDPType, Data: []byte{CmdReply}}, nil)
	s.Inbound(ddp.Datagram{DDPType: 7, Data: []byte{CmdRequest}}, nil)

	time.Sleep(10 * time.Millisecond)
	if got := fr.waitReplies(0); len(got) != 0 {
		t.Errorf("non-request traffic produced %d replies, want 0", len(got))
	}
}

func TestInboundAfterStopDoesNotPanic(t *testing.T) {
	fr := &fakeRouter{}
	s := New(fr, nil)
	_ = s.Start(context.Background())
	_ = s.Stop(context.Background())
	// Must be a safe no-op, not a send-on-closed-channel panic.
	s.Inbound(ddp.Datagram{DDPType: DDPType, Data: []byte{CmdRequest}}, nil)
}
