package libpcap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/gopacket/pcapgo"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
)

// TestSink_WritesReadablePcap writes frames through the gopacket-backed sink and
// reads them back, confirming the file is a valid .pcap. This mirrors the
// pure-Go pcapfile sink's test so both writers are proven equivalent.
func TestSink_WritesReadablePcap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.pcap")
	sink, err := New(path, LinkTypeEthernet, 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	frames := []link.Frame{
		{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
		{0xDE, 0xAD, 0xBE, 0xEF},
	}
	for i, f := range frames {
		sink.WriteFrame(int64(i+1)*1_000_000_000, f)
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
		t.Fatalf("pcapgo.NewReader: %v", err)
	}
	for i := range frames {
		data, _, err := r.ReadPacketData()
		if err != nil {
			t.Fatalf("ReadPacketData #%d: %v", i, err)
		}
		if string(data) != string(frames[i]) {
			t.Fatalf("frame %d = % x, want % x", i, data, frames[i])
		}
	}
}
