package pcapfile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/gopacket/pcapgo"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
)

// TestSink_WritesReadablePcap writes a few frames through the pure-Go sink and
// reads them back with gopacket's reader, proving the file is a valid,
// Wireshark-openable .pcap (the §6f / M1 "non-pcap link writes a valid pcap that
// tshark -r reads back" acceptance, asserted in-process via the same parser
// tshark uses). The test may use gopacket; the writer itself does not.
func TestSink_WritesReadablePcap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.pcap")
	sink, err := New(path, LinkTypeEthernet, 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	frames := []link.Frame{
		{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x80, 0x9B},
		{0xDE, 0xAD, 0xBE, 0xEF},
		{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
	}
	// Distinct nanosecond timestamps to confirm the sec/usec split.
	ts := []int64{1_000_000_000, 1_500_000_500_000, 2_123_456_789}
	for i, f := range frames {
		sink.WriteFrame(ts[i], f)
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	r, err := pcapgo.NewReader(f)
	if err != nil {
		t.Fatalf("pcapgo.NewReader (invalid pcap header): %v", err)
	}
	if lt := r.LinkType(); uint32(lt) != uint32(LinkTypeEthernet) {
		t.Fatalf("link type = %d, want %d", lt, LinkTypeEthernet)
	}

	for i := range frames {
		data, ci, err := r.ReadPacketData()
		if err != nil {
			t.Fatalf("ReadPacketData #%d: %v", i, err)
		}
		if string(data) != string(frames[i]) {
			t.Fatalf("frame %d = % x, want % x", i, data, frames[i])
		}
		if ci.Length != len(frames[i]) {
			t.Fatalf("frame %d orig_len = %d, want %d", i, ci.Length, len(frames[i]))
		}
		wantNanos := ts[i] - (ts[i] % 1000) // we keep microsecond resolution
		if got := ci.Timestamp.UnixNano(); got != wantNanos {
			t.Fatalf("frame %d ts = %d ns, want %d ns", i, got, wantNanos)
		}
	}

	// A fourth read should be EOF.
	if _, _, err := r.ReadPacketData(); err == nil {
		t.Fatalf("expected EOF after %d frames", len(frames))
	}
}

// TestSink_NilAndEmpty exercises the no-op guards.
func TestSink_NilAndEmpty(t *testing.T) {
	var s *Sink
	s.WriteFrame(0, link.Frame{1, 2, 3}) // nil receiver: must not panic
	if err := s.Close(); err != nil {
		t.Fatalf("nil Close: %v", err)
	}

	path := filepath.Join(t.TempDir(), "empty.pcap")
	real, err := New(path, LinkTypeLocalTalk, 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	real.WriteFrame(1, nil)          // empty frame: skipped
	real.WriteFrame(1, link.Frame{}) // zero-len: skipped
	if err := real.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := real.Close(); err != nil { // idempotent
		t.Fatalf("double Close: %v", err)
	}
}
