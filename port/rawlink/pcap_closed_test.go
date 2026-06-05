package rawlink

import (
	"errors"
	"testing"
)

// TestPcapLinkClosedGuards verifies that a closed pcapLink returns ErrClosed
// from ReadFrame, WriteFrame, and SetFilter instead of touching the freed
// libpcap handle. Reusing a port across a UI stop/start cycle previously drove
// SetFilter into pcap_compile on a closed handle, a use-after-free that
// surfaced as a 0xC0000005 access violation on Windows. The closed flag is
// checked before handle is dereferenced, so a nil handle here is intentional:
// if any guard is removed, the nil deref panics and fails the test.
func TestPcapLinkClosedGuards(t *testing.T) {
	l := &pcapLink{closed: true}

	if _, err := l.ReadFrame(); !errors.Is(err, ErrClosed) {
		t.Errorf("ReadFrame after close = %v, want ErrClosed", err)
	}
	if err := l.WriteFrame([]byte{0x00}); !errors.Is(err, ErrClosed) {
		t.Errorf("WriteFrame after close = %v, want ErrClosed", err)
	}
	if err := l.SetFilter("ip"); !errors.Is(err, ErrClosed) {
		t.Errorf("SetFilter after close = %v, want ErrClosed", err)
	}
}

// TestPcapLinkCloseIdempotent verifies Close can be called repeatedly without
// double-freeing the underlying handle. The first Close sets closed, so the
// second returns early before reaching handle.Close (which is nil here).
func TestPcapLinkCloseIdempotent(t *testing.T) {
	l := &pcapLink{closed: true} // simulate already-closed; handle never touched.
	if err := l.Close(); err != nil {
		t.Errorf("second Close = %v, want nil", err)
	}
}
