package pcapfile_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/gopacket/pcapgo"

	"github.com/ObsoleteMadness/ClassicStack/adapter/capture/pcapfile"
	"github.com/ObsoleteMadness/ClassicStack/adapter/link/inmem"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
)

// TestCaptureDecorator_NonPcapLink_WritesValidPcap is the M1 acceptance: a
// NON-pcap FrameLink (here in-memory) wrapped by the core link.Capture decorator
// tees frames into the pure-Go pcapfile sink, producing a Wireshark-openable
// .pcap with no libpcap linked. We read it back with gopacket (the same format
// tshark -r consumes).
func TestCaptureDecorator_NonPcapLink_WritesValidPcap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tee.pcap")
	sink, err := pcapfile.New(path, pcapfile.LinkTypeEthernet, 0)
	if err != nil {
		t.Fatalf("New sink: %v", err)
	}

	a, b := inmem.Pair(4)
	defer a.Close()
	defer b.Close()

	// Capture tees both Read and Write frames on side a into the sink.
	capLink := link.Capture(a, sink)

	want := []link.Frame{
		{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x80, 0x9B, 0x10},
		{0x09, 0x00, 0x07, 0xFF, 0xFF, 0xFF, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x14},
	}
	// Write frame 0 from a (captured on the write path).
	if err := capLink.Write(want[0]); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// Drain it off b so the channel doesn't fill.
	if _, err := b.Read(); err != nil {
		t.Fatalf("peer Read: %v", err)
	}
	// Send frame 1 from b -> a and read it through the decorator (captured on read).
	if err := b.Write(want[1]); err != nil {
		t.Fatalf("peer Write: %v", err)
	}
	if _, err := capLink.Read(); err != nil {
		t.Fatalf("Read: %v", err)
	}

	// Closing the decorator closes the sink (flushing the file).
	if err := capLink.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	r, err := pcapgo.NewReader(f)
	if err != nil {
		t.Fatalf("pcapgo.NewReader: %v", err)
	}
	for i := range want {
		data, _, err := r.ReadPacketData()
		if err != nil {
			t.Fatalf("ReadPacketData #%d: %v", i, err)
		}
		if string(data) != string(want[i]) {
			t.Fatalf("captured frame %d = % x, want % x", i, data, want[i])
		}
	}
}
