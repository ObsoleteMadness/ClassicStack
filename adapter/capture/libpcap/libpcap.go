package libpcap

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

	"github.com/ObsoleteMadness/ClassicStack/core/link"
)

// LinkType is a thin alias for gopacket's layers.LinkType so callers don't have
// to import gopacket directly.
type LinkType = layers.LinkType

const (
	// LinkTypeLocalTalk is DLT_LTALK (114).
	LinkTypeLocalTalk LinkType = layers.LinkTypeLTalk
	// LinkTypeEthernet is DLT_EN10MB (1).
	LinkTypeEthernet LinkType = layers.LinkTypeEthernet
)

// Sink writes captured frames as a libpcap-format file via gopacket/pcapgo. It
// satisfies core/link.CaptureSink. Safe for concurrent WriteFrame/Close.
type Sink struct {
	mu  sync.Mutex
	f   *os.File
	bw  *bufio.Writer
	w   *pcapgo.Writer
	cap uint32
}

// Compile-time assertion that *Sink is a CaptureSink.
var _ link.CaptureSink = (*Sink)(nil)

// New creates and opens a pcap file at path with the given link-layer type and
// snap length (0 -> 65535). Parent directories are created as needed.
func New(path string, lt LinkType, snaplen uint32) (*Sink, error) {
	if snaplen == 0 {
		snaplen = 65535
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		// 0750: a capture directory may hold packet dumps with sensitive
		// payloads, so it should not be world-readable.
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("libpcap: mkdir %s: %w", dir, err)
		}
	}
	// The capture path is an operator-configured destination (server.toml / UI),
	// i.e. trusted input, not an attacker-controlled request parameter.
	f, err := os.Create(path) // #nosec G304 -- operator-configured capture path
	if err != nil {
		return nil, fmt.Errorf("libpcap: open %s: %w", path, err)
	}
	bw := bufio.NewWriter(f)
	w := pcapgo.NewWriter(bw)
	if err := w.WriteFileHeader(snaplen, lt); err != nil {
		_ = bw.Flush()
		_ = f.Close()
		return nil, fmt.Errorf("libpcap: write header: %w", err)
	}
	return &Sink{f: f, bw: bw, w: w, cap: snaplen}, nil
}

// WriteFrame appends one captured frame stamped at tsUnixNano. Errors are
// swallowed by design: a broken capture file must never take down the data path.
func (s *Sink) WriteFrame(tsUnixNano int64, frame link.Frame) {
	if s == nil || len(frame) == 0 {
		return
	}
	data := frame
	if uint32(len(data)) > s.cap {
		data = data[:s.cap]
	}
	ci := gopacket.CaptureInfo{
		Timestamp:     time.Unix(0, tsUnixNano),
		CaptureLength: len(data),
		Length:        len(frame),
	}
	s.mu.Lock()
	_ = s.w.WritePacket(ci, data)
	s.mu.Unlock()
}

// Close flushes and closes the underlying file. Idempotent.
func (s *Sink) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return nil
	}
	flushErr := s.bw.Flush()
	closeErr := s.f.Close()
	s.f = nil
	if flushErr != nil {
		return flushErr
	}
	return closeErr
}
