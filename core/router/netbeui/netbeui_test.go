package netbeui

import (
	"testing"

	portnetbeui "github.com/ObsoleteMadness/ClassicStack/core/port/netbeui"
	nbf "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbeui"
)

// fakePort implements the mini-router's Port: it captures the delivery callback and records
// sends. SetDeliveryCallback takes the port package's named type so it satisfies Port.
type fakePort struct {
	cb         portnetbeui.DeliveryCallback
	sent       []*nbf.Frame
	broadcasts []*nbf.Frame
}

func (p *fakePort) SetDeliveryCallback(cb portnetbeui.DeliveryCallback) { p.cb = cb }
func (p *fakePort) Send(_ [6]byte, frame *nbf.Frame) error {
	p.sent = append(p.sent, frame)
	return nil
}
func (p *fakePort) SendBroadcast(frame *nbf.Frame) error {
	p.broadcasts = append(p.broadcasts, frame)
	return nil
}

type recordingName struct{ got []*nbf.Frame }

func (h *recordingName) HandleFrame(_, _ [6]byte, frame *nbf.Frame) { h.got = append(h.got, frame) }

type recordingSession struct{ got []*nbf.Frame }

func (h *recordingSession) HandleSessionFrame(_, _ [6]byte, frame *nbf.Frame) {
	h.got = append(h.got, frame)
}

func nameOf(s string) [16]byte {
	var n [16]byte
	copy(n[:], s)
	return n
}

func newWiredRouter() (*Router, *fakePort) {
	r := NewRouter(nil)
	p := &fakePort{}
	r.AddPort(p)
	return r, p
}

func TestNameDispatch(t *testing.T) {
	r, p := newWiredRouter()
	h := &recordingName{}
	name := nameOf("FILESERVER")
	if err := r.RegisterName(name, h); err != nil {
		t.Fatalf("RegisterName: %v", err)
	}
	// A non-session UI frame addressed to the registered name.
	p.cb([6]byte{1, 2, 3, 4, 5, 6}, [6]byte{}, &nbf.Frame{Command: 0x08, DestinationName: name})
	if len(h.got) != 1 {
		t.Fatalf("name handler got %d, want 1", len(h.got))
	}
}

func TestUnregisteredNameGoesToBroadcast(t *testing.T) {
	r, p := newWiredRouter()
	bcast := &recordingName{}
	if err := r.RegisterBroadcast(bcast); err != nil {
		t.Fatalf("RegisterBroadcast: %v", err)
	}
	// No name handler registered for this destination → broadcast handler catches it.
	p.cb([6]byte{1, 2, 3, 4, 5, 6}, [6]byte{}, &nbf.Frame{Command: 0x00, DestinationName: nameOf("UNKNOWN")})
	if len(bcast.got) != 1 {
		t.Errorf("broadcast handler got %d, want 1", len(bcast.got))
	}
}

func TestSessionFrameGoesToSessionHandler(t *testing.T) {
	r, p := newWiredRouter()
	sess := &recordingSession{}
	name := &recordingName{}
	_ = r.RegisterName(nameOf("X"), name)
	if err := r.RegisterSession(sess); err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}
	// A session command (>=0x14) goes to the session handler, never the name handler.
	p.cb([6]byte{1, 2, 3, 4, 5, 6}, [6]byte{}, &nbf.Frame{Command: 0x15, DestNumber: 1, SourceNumber: 2})
	if len(sess.got) != 1 {
		t.Errorf("session handler got %d, want 1", len(sess.got))
	}
	if len(name.got) != 0 {
		t.Errorf("name handler got a session frame (%d)", len(name.got))
	}
}

func TestSessionFrameDroppedWithoutHandler(t *testing.T) {
	_, p := newWiredRouter()
	// No session handler registered: must not panic, just drop.
	p.cb([6]byte{1, 2, 3, 4, 5, 6}, [6]byte{}, &nbf.Frame{Command: 0x16})
}
