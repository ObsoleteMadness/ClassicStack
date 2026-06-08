package link

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
)

type loopbackFrameLink struct {
	ch     chan Frame
	closed bool
}

func newLoopbackFrameLink(depth int) *loopbackFrameLink {
	if depth <= 0 {
		depth = 1
	}
	return &loopbackFrameLink{ch: make(chan Frame, depth)}
}

func (l *loopbackFrameLink) Read() (Frame, error) {
	select {
	case f, ok := <-l.ch:
		if !ok {
			return nil, ErrClosed
		}
		return f, nil
	default:
		if l.closed {
			return nil, ErrClosed
		}
		return nil, ErrTimeout
	}
}

func (l *loopbackFrameLink) Write(f Frame) error {
	if l.closed {
		return ErrClosed
	}
	cpy := append(Frame(nil), f...)
	l.ch <- cpy
	return nil
}

func (l *loopbackFrameLink) Close() error {
	if l.closed {
		return ErrClosed
	}
	l.closed = true
	close(l.ch)
	return nil
}

type identityDatagramLink struct{ inner FrameLink }

func (l *identityDatagramLink) ReadDatagram() (ddp.Datagram, error) {
	f, err := l.inner.Read()
	if err != nil {
		return ddp.Datagram{}, err
	}
	return ddp.Decode(f)
}

func (l *identityDatagramLink) WriteDatagram(d ddp.Datagram) error {
	f, err := d.Encode(nil)
	if err != nil {
		return err
	}
	return l.inner.Write(f)
}

func (l *identityDatagramLink) Close() error { return l.inner.Close() }

type identityFramer struct{}

func (identityFramer) Framing(fl FrameLink) (DatagramLink, error) {
	return &identityDatagramLink{inner: fl}, nil
}

func TestLoopbackFrameLink_ReadWrite(t *testing.T) {
	l := newLoopbackFrameLink(1)
	in := Frame{1, 2, 3}
	if err := l.Write(in); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	out, err := l.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(out) != len(in) || out[0] != in[0] || out[1] != in[1] || out[2] != in[2] {
		t.Fatalf("Read() = %v, want %v", out, in)
	}
}

func TestDecorators_NoOpPlaceholders(t *testing.T) {
	inner := newLoopbackFrameLink(1)
	pass := func(Frame) bool { return true }
	if got := Filter(inner, pass); got != inner {
		t.Fatalf("Filter() did not return inner")
	}
	if got := Dedup(inner, 1000); got != inner {
		t.Fatalf("Dedup() did not return inner")
	}
	if got := Capture(inner, nil); got != inner {
		t.Fatalf("Capture() did not return inner")
	}
	if got := Bridge(inner, "bridge"); got != inner {
		t.Fatalf("Bridge() did not return inner")
	}
}

func TestIdentityFramer_RoundTripDatagram(t *testing.T) {
	fl := newLoopbackFrameLink(1)
	dl, err := identityFramer{}.Framing(fl)
	if err != nil {
		t.Fatalf("Framing() error = %v", err)
	}
	in := ddp.Datagram{
		Hops:        1,
		DestNetwork: 1,
		SrcNetwork:  2,
		DestNode:    3,
		SrcNode:     4,
		DestSocket:  5,
		SrcSocket:   6,
		DDPType:     7,
		Data:        []byte{0xAA, 0xBB},
	}
	if err := dl.WriteDatagram(in); err != nil {
		t.Fatalf("WriteDatagram() error = %v", err)
	}
	out, err := dl.ReadDatagram()
	if err != nil {
		t.Fatalf("ReadDatagram() error = %v", err)
	}
	if out.Hops != in.Hops ||
		out.DestNetwork != in.DestNetwork ||
		out.SrcNetwork != in.SrcNetwork ||
		out.DestNode != in.DestNode ||
		out.SrcNode != in.SrcNode ||
		out.DestSocket != in.DestSocket ||
		out.SrcSocket != in.SrcSocket ||
		out.DDPType != in.DDPType ||
		len(out.Data) != len(in.Data) ||
		out.Data[0] != in.Data[0] ||
		out.Data[1] != in.Data[1] {
		t.Fatalf("ReadDatagram() = %#v, want %#v", out, in)
	}
}
