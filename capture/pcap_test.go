package capture

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/gopacket/pcapgo"
	"os"
)

func TestPcapSinkRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.pcap")

	sink, err := NewPcapSink(path, LinkTypeLocalTalk, 0)
	if err != nil {
		t.Fatalf("NewPcapSink: %v", err)
	}

	frames := [][]byte{
		{0x01, 0x02, 0x01, 0xDE, 0xAD},
		{0x03, 0x04, 0x02, 0xBE, 0xEF, 0xCA, 0xFE},
	}
	now := time.Unix(1700000000, 0)
	for i, f := range frames {
		sink.WriteFrame(now.Add(time.Duration(i)*time.Millisecond), f)
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
		t.Fatalf("NewReader: %v", err)
	}
	if got := r.LinkType(); got != LinkTypeLocalTalk {
		t.Fatalf("link type = %v, want %v", got, LinkTypeLocalTalk)
	}
	for i, want := range frames {
		data, _, err := r.ReadPacketData()
		if err != nil {
			t.Fatalf("ReadPacketData[%d]: %v", i, err)
		}
		if !bytes.Equal(data, want) {
			t.Fatalf("frame %d = %x, want %x", i, data, want)
		}
	}
	if _, _, err := r.ReadPacketData(); err == nil {
		t.Fatalf("expected EOF after %d frames", len(frames))
	}
}
