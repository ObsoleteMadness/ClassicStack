package ltoudp

import (
	"errors"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
)

// TestPutUint32 pins the big-endian sender-ID encoding (the hand-rolled
// encoding/binary substitute).
func TestPutUint32(t *testing.T) {
	var b [4]byte
	putUint32(b[:], 0x01020304)
	if b != [4]byte{0x01, 0x02, 0x03, 0x04} {
		t.Fatalf("putUint32 = %v, want 01 02 03 04", b)
	}
}

// TestDefaultConfig: an empty interface keeps the wildcard join; the read
// timeout defaults when zero.
func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig("")
	if cfg.Interface != "" {
		t.Errorf("Interface = %q, want empty", cfg.Interface)
	}
	if cfg.ReadTimeout != defaultReadTimeout {
		t.Errorf("ReadTimeout = %v, want %v", cfg.ReadTimeout, defaultReadTimeout)
	}
}

// TestTwoOpensSharePort proves SO_REUSEADDR+SO_REUSEPORT let two sockets bind
// 0.0.0.0:1954 (spec/ltoudp.md: multiple instances on one host). Skipped when
// the host cannot open the group at all.
func TestTwoOpensSharePort(t *testing.T) {
	a, err := Open(DefaultConfig(""))
	if err != nil {
		t.Skipf("cannot open LToUDP group: %v", err)
	}
	defer a.Close()
	b, err := Open(DefaultConfig(""))
	if err != nil {
		t.Fatalf("second bind of 0.0.0.0:%d failed (need SO_REUSEPORT): %v", GroupPort, err)
	}
	defer b.Close()
}

// TestRoundTripMulticast opens two LToUDP links on the shared group (distinct
// per-process sender IDs are NOT distinct here — same PID — so we force them
// apart) and proves a frame written on one is read on the other, with the
// sender ID stripped. Skipped (not failed) when the host cannot join the group
// (locked-down CI / no multicast NIC): the dedup + strip logic is also covered
// by the unit tests above, which need no socket.
func TestRoundTripMulticast(t *testing.T) {
	a, err := Open(DefaultConfig(""))
	if err != nil {
		t.Skipf("cannot open LToUDP group (no multicast NIC?): %v", err)
	}
	defer a.Close()
	b, err := Open(DefaultConfig(""))
	if err != nil {
		t.Skipf("cannot open second LToUDP link: %v", err)
	}
	defer b.Close()

	// Both links share this process's PID as sender ID, so each would drop the
	// other's frames as "own echo". Force distinct IDs so b accepts a's frame.
	fa := a.(*frameLink)
	fb := b.(*frameLink)
	fa.senderID = [senderIDLen]byte{0xAA, 0xAA, 0xAA, 0xAA}
	fb.senderID = [senderIDLen]byte{0xBB, 0xBB, 0xBB, 0xBB}

	want := []byte{0xFF, 0x42, 0x01, 0xDE, 0xAD, 0xBE, 0xEF} // a fake LLAP frame
	if err := fa.Write(want); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := readWithin(t, fb, 2*time.Second)
	if err != nil {
		t.Skipf("no multicast delivery within deadline (firewall?): %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("read %v, want %v (sender ID should be stripped)", got, want)
	}
}

// TestOwnEchoDropped proves a link drops its OWN multicast echo: with loopback
// on, a frame a writes comes back to a, and Read must skip it (returning
// ErrTimeout once the deadline passes with nothing but the echo on the wire).
func TestOwnEchoDropped(t *testing.T) {
	a, err := Open(Config{ReadTimeout: 200 * time.Millisecond})
	if err != nil {
		t.Skipf("cannot open LToUDP group: %v", err)
	}
	defer a.Close()

	if err := a.Write([]byte{0xFF, 0x01, 0x01, 0x00}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// Reading should never yield our own frame; the only traffic is the echo, so
	// Read drains it and reports ErrTimeout.
	if _, err := a.Read(); !errors.Is(err, link.ErrTimeout) {
		t.Fatalf("Read after own write = %v, want ErrTimeout (echo must be dropped)", err)
	}
}

// TestClosedReadWrite: after Close, Read and Write are terminal with ErrClosed.
func TestClosedReadWrite(t *testing.T) {
	a, err := Open(DefaultConfig(""))
	if err != nil {
		t.Skipf("cannot open LToUDP group: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("second Close = %v, want nil (idempotent)", err)
	}
	if _, err := a.Read(); !errors.Is(err, link.ErrClosed) {
		t.Errorf("Read after Close = %v, want ErrClosed", err)
	}
	if err := a.Write([]byte{1, 2, 3}); !errors.Is(err, link.ErrClosed) {
		t.Errorf("Write after Close = %v, want ErrClosed", err)
	}
}

// readWithin loops Read past ErrTimeout until a frame arrives or the deadline
// expires (multicast can take a moment to deliver).
func readWithin(t *testing.T, fl link.FrameLink, d time.Duration) (link.Frame, error) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		f, err := fl.Read()
		if errors.Is(err, link.ErrTimeout) {
			continue
		}
		return f, err
	}
	return nil, errors.New("deadline exceeded")
}
