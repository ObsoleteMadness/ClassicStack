package capture

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcapgo"
)

// LinkType is a thin alias for layers.LinkType so callers don't have
// to import gopacket directly.
type LinkType = layers.LinkType

const (
	LinkTypeLocalTalk LinkType = layers.LinkTypeLTalk   // DLT_LTALK = 114
	LinkTypeEthernet  LinkType = layers.LinkTypeEthernet // DLT_EN10MB = 1
)

// PcapSink writes captured frames as a libpcap-format file.
type PcapSink struct {
	mu  sync.Mutex
	f   *os.File
	bw  *bufio.Writer
	w   *pcapgo.Writer
	cap uint32
}

// NewPcapSink creates and opens a pcap file at path with the given
// link-layer type and snap length. If snaplen is zero, 65535 is used.
func NewPcapSink(path string, lt LinkType, snaplen uint32) (*PcapSink, error) {
	if snaplen == 0 {
		snaplen = 65535
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("capture: mkdir %s: %w", dir, err)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("capture: open %s: %w", path, err)
	}
	bw := bufio.NewWriter(f)
	w := pcapgo.NewWriter(bw)
	if err := w.WriteFileHeader(snaplen, lt); err != nil {
		_ = bw.Flush()
		_ = f.Close()
		return nil, fmt.Errorf("capture: write header: %w", err)
	}
	return &PcapSink{f: f, bw: bw, w: w, cap: snaplen}, nil
}

// WriteFrame appends one captured frame. Errors are swallowed (logged
// nowhere) on purpose: a broken capture file should never take down
// the data path.
func (p *PcapSink) WriteFrame(ts time.Time, frame []byte) {
	if p == nil || len(frame) == 0 {
		return
	}
	data := frame
	if uint32(len(data)) > p.cap {
		data = data[:p.cap]
	}
	ci := gopacket.CaptureInfo{
		Timestamp:     ts,
		CaptureLength: len(data),
		Length:        len(frame),
	}
	p.mu.Lock()
	_ = p.w.WritePacket(ci, data)
	p.mu.Unlock()
}

// Close flushes and closes the underlying file.
func (p *PcapSink) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.f == nil {
		return nil
	}
	flushErr := p.bw.Flush()
	closeErr := p.f.Close()
	p.f = nil
	if flushErr != nil {
		return flushErr
	}
	return closeErr
}
