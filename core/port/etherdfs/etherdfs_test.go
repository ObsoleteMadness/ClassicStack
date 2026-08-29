package etherdfs

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/etherdfs"
)

// fakeFrameLink is an in-test link.FrameLink: queued frames are returned by Read
// (then ErrTimeout to idle the loop); written frames are captured.
type fakeFrameLink struct {
	mu     sync.Mutex
	inbox  [][]byte
	sent   [][]byte
	closed bool
}

func (f *fakeFrameLink) push(frame []byte) {
	f.mu.Lock()
	f.inbox = append(f.inbox, frame)
	f.mu.Unlock()
}

func (f *fakeFrameLink) Read() (link.Frame, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil, link.ErrClosed
	}
	if len(f.inbox) > 0 {
		frame := f.inbox[0]
		f.inbox = f.inbox[1:]
		return frame, nil
	}
	return nil, link.ErrTimeout
}

func (f *fakeFrameLink) Write(frame link.Frame) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return link.ErrClosed
	}
	cp := make([]byte, len(frame))
	copy(cp, frame)
	f.sent = append(f.sent, cp)
	return nil
}

func (f *fakeFrameLink) Close() error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	return nil
}

func (f *fakeFrameLink) sent0() ([]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		return nil, false
	}
	return f.sent[0], true
}

func newTestPort(t *testing.T, srcMAC [6]byte, fl link.FrameLink) *Port {
	t.Helper()
	sec := &port.Section{SKey: Name, IsEnabled: true}
	open := func() (link.FrameLink, error) { return fl, nil }
	p, err := NewInstanceFromOpener(sec, open, srcMAC, log.New(Name))
	if err != nil {
		t.Fatalf("NewInstanceFromOpener: %v", err)
	}
	return p
}

// buildReq encodes a request frame addressed to dst from src.
func buildReq(dst, src [6]byte, op uint8, payload []byte) []byte {
	f := proto.Frame{DstMAC: dst, SrcMAC: src, Sequence: 1, Opcode: op, Payload: payload}
	return f.Encode(nil)
}

func waitFor(cond func() bool) bool {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return false
}

func TestPortDispatchesAddressedFrame(t *testing.T) {
	srcMAC := [6]byte{0x02, 0, 0, 0, 0, 0x01}
	client := [6]byte{0x02, 0, 0, 0, 0, 0x99}
	fl := &fakeFrameLink{}
	p := newTestPort(t, srcMAC, fl)

	var gotOpcode uint8
	p.SetHandler(func(req proto.Frame) (uint16, []byte, bool) {
		gotOpcode = req.Opcode
		return proto.ErrNone, nil, true
	})

	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer p.Stop(context.Background())

	fl.push(buildReq(srcMAC, client, proto.OpDiskspace, nil))

	if !waitFor(func() bool { _, ok := fl.sent0(); return ok }) {
		t.Fatal("no reply sent for addressed frame")
	}
	if gotOpcode != proto.OpDiskspace {
		t.Errorf("handler saw opcode %#x, want OpDiskspace", gotOpcode)
	}
	// The reply must come from our MAC, back to the client.
	out, _ := fl.sent0()
	rep, err := proto.ParseFrame(out)
	if err != nil {
		t.Fatalf("reply not a valid frame: %v", err)
	}
	if rep.DstMAC != client || rep.SrcMAC != srcMAC {
		t.Errorf("reply MACs wrong: dst=%v src=%v", rep.DstMAC, rep.SrcMAC)
	}
}

func TestPortAcceptsBroadcast(t *testing.T) {
	srcMAC := [6]byte{0x02, 0, 0, 0, 0, 0x01}
	client := [6]byte{0x02, 0, 0, 0, 0, 0x99}
	fl := &fakeFrameLink{}
	p := newTestPort(t, srcMAC, fl)
	p.SetHandler(func(req proto.Frame) (uint16, []byte, bool) { return proto.ErrNone, nil, true })
	_ = p.Start(context.Background())
	defer p.Stop(context.Background())

	// The reference client's auto-discovery broadcasts an ordinary AL_DISKSPACE
	// query (no dedicated discovery opcode exists on the wire) and learns the
	// server's MAC from whichever reply arrives.
	fl.push(buildReq(broadcastMAC, client, proto.OpDiskspace, nil))
	if !waitFor(func() bool { _, ok := fl.sent0(); return ok }) {
		t.Fatal("broadcast (auto-discovery) frame was not answered")
	}
}

func TestPortIgnoresForeignMAC(t *testing.T) {
	srcMAC := [6]byte{0x02, 0, 0, 0, 0, 0x01}
	other := [6]byte{0x02, 0, 0, 0, 0, 0x55}
	client := [6]byte{0x02, 0, 0, 0, 0, 0x99}
	fl := &fakeFrameLink{}
	p := newTestPort(t, srcMAC, fl)

	called := false
	p.SetHandler(func(req proto.Frame) (uint16, []byte, bool) { called = true; return 0, nil, true })
	_ = p.Start(context.Background())
	defer p.Stop(context.Background())

	// A frame addressed to a different unicast MAC must be ignored.
	fl.push(buildReq(other, client, proto.OpDiskspace, nil))
	time.Sleep(50 * time.Millisecond)
	if called {
		t.Error("handler ran for a frame addressed to a foreign MAC")
	}
}

func TestPortIgnoresWrongEtherType(t *testing.T) {
	srcMAC := [6]byte{0x02, 0, 0, 0, 0, 0x01}
	client := [6]byte{0x02, 0, 0, 0, 0, 0x99}
	fl := &fakeFrameLink{}
	p := newTestPort(t, srcMAC, fl)
	called := false
	p.SetHandler(func(req proto.Frame) (uint16, []byte, bool) { called = true; return 0, nil, true })
	_ = p.Start(context.Background())
	defer p.Stop(context.Background())

	frame := buildReq(srcMAC, client, proto.OpDiskspace, nil)
	frame[12], frame[13] = 0x08, 0x00 // rewrite EtherType to IPv4
	fl.push(frame)
	time.Sleep(50 * time.Millisecond)
	if called {
		t.Error("handler ran for a non-EtherDFS EtherType")
	}
}

func TestDisabledSectionBuildsInertPort(t *testing.T) {
	// A disabled section still builds the port (the MacIP pattern): the component
	// exists so the dashboard can show it Disabled and the operator can enable it
	// live. It must report Enabled()==false and Start inert (nil opener → no link).
	sec := &port.Section{SKey: Name, IsEnabled: false}
	p, err := NewInstanceFromOpener(sec, nil, [6]byte{}, log.New(Name))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if p == nil {
		t.Fatal("disabled section should still build the port")
	}
	if p.Enabled() {
		t.Error("disabled section: Enabled() = true, want false")
	}
	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("inert Start: %v", err)
	}
	if err := p.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}
