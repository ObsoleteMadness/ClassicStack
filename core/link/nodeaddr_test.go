package link

import (
	"errors"
	"testing"
)

// nodeFilterLink is a FrameLink WITH a hardware node filter, recording the last
// node it was armed for.
type nodeFilterLink struct {
	*loopbackFrameLink
	armed    uint8
	calls    int
	failWith error
}

func newNodeFilterLink() *nodeFilterLink {
	return &nodeFilterLink{loopbackFrameLink: newLoopbackFrameLink(1)}
}

func (n *nodeFilterLink) SetNodeAddress(node uint8) error {
	n.calls++
	if n.failWith != nil {
		return n.failWith
	}
	n.armed = node
	return nil
}

// TestDecoratorsForwardSetNodeAddress is the regression guard for a bug that made a
// TashTalk port receive NOTHING: every decorator embeds FrameLink as an INTERFACE,
// which does not promote extra methods, so wrapping a link hid SetNodeAddress and
// the node-claim's type assertion silently failed. A port with capture enabled (the
// common case) therefore never armed its hardware filter.
func TestDecoratorsForwardSetNodeAddress(t *testing.T) {
	cases := []struct {
		name string
		wrap func(FrameLink) FrameLink
	}{
		{"Capture", func(fl FrameLink) FrameLink { return Capture(fl, &nopSink{}) }},
		{"Pace", func(fl FrameLink) FrameLink { return Pace(fl, int64(1)) }},
		{"Filter", func(fl FrameLink) FrameLink { return Filter(fl, func(Frame) bool { return true }) }},
		{"Dedup", func(fl FrameLink) FrameLink { return Dedup(fl, int64(1)) }},
		{"Capture+Pace stacked", func(fl FrameLink) FrameLink {
			return Capture(Pace(fl, int64(1)), &nopSink{})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inner := newNodeFilterLink()
			wrapped := tc.wrap(inner)

			s, ok := wrapped.(NodeAddressSetter)
			if !ok {
				t.Fatalf("%s does not expose SetNodeAddress; a wrapped TashTalk port would never arm its filter", tc.name)
			}
			if err := s.SetNodeAddress(0xFE); err != nil {
				t.Fatalf("SetNodeAddress through %s: %v", tc.name, err)
			}
			if inner.armed != 0xFE {
				t.Fatalf("inner armed = %d, want 254 (the call did not reach the hardware link)", inner.armed)
			}
		})
	}
}

// TestDecoratorsReportUnsupportedForPlainLink proves a transport with NO hardware
// filter (LToUDP, virtual) reports ErrUnsupported through a decorator rather than
// pretending success — the caller treats that as "nothing to arm".
func TestDecoratorsReportUnsupportedForPlainLink(t *testing.T) {
	plain := newLoopbackFrameLink(1)
	wrapped := Capture(plain, &nopSink{})

	s, ok := wrapped.(NodeAddressSetter)
	if !ok {
		t.Fatal("captureLink should always expose SetNodeAddress (it forwards)")
	}
	if err := s.SetNodeAddress(0xFE); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("SetNodeAddress on a filterless link = %v, want ErrUnsupported", err)
	}
}

// TestForwardedErrorPropagates proves a real hardware failure is not swallowed by
// the passthrough (it must not be mistaken for ErrUnsupported).
func TestForwardedErrorPropagates(t *testing.T) {
	boom := errors.New("device write failed")
	inner := newNodeFilterLink()
	inner.failWith = boom

	s, ok := Capture(inner, &nopSink{}).(NodeAddressSetter)
	if !ok {
		t.Fatal("captureLink does not expose SetNodeAddress")
	}
	if err := s.SetNodeAddress(0xFE); !errors.Is(err, boom) {
		t.Fatalf("SetNodeAddress = %v, want the device error", err)
	}
}

type nopSink struct{}

func (nopSink) WriteFrame(int64, Frame) {}
func (nopSink) Close() error            { return nil }
