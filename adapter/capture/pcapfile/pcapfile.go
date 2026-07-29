package pcapfile

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
)

// LinkType identifies the data-link layer of captured frames (libpcap DLT_*).
// Only the values ClassicStack emits are named; any uint32 is accepted.
type LinkType uint32

const (
	// LinkTypeEthernet is DLT_EN10MB (1): standard 802.3 / Ethernet II frames.
	LinkTypeEthernet LinkType = 1
	// LinkTypeLocalTalk is DLT_LTALK (114): AppleTalk LocalTalk frames.
	LinkTypeLocalTalk LinkType = 114
)

// magic is the classic libpcap magic for microsecond-resolution, host-order
// timestamps. We always write little-endian, so we emit this value LE; readers
// detect endianness from how they read the magic back.
const magicMicros uint32 = 0xA1B2C3D4

const defaultSnapLen uint32 = 65535

// Sink writes captured frames to a libpcap-format .pcap file. It satisfies
// core/link.CaptureSink. Safe for concurrent WriteFrame/Close.
type Sink struct {
	mu      sync.Mutex
	f       *os.File
	bw      *bufio.Writer
	snapLen uint32
}

// Compile-time assertion that *Sink is a CaptureSink.
var _ link.CaptureSink = (*Sink)(nil)

// New creates and opens a .pcap file at path with the given link-layer type and
// snap length (0 -> 65535), writing the global header immediately. Parent
// directories are created as needed.
func New(path string, lt LinkType, snaplen uint32) (*Sink, error) {
	if snaplen == 0 {
		snaplen = defaultSnapLen
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		// 0750: a capture directory may hold packet dumps with sensitive
		// payloads, so it should not be world-readable.
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("pcapfile: mkdir %s: %w", dir, err)
		}
	}
	// The capture path is an operator-configured destination (server.toml / UI),
	// i.e. trusted input, not an attacker-controlled request parameter.
	f, err := os.Create(path) // #nosec G304 -- operator-configured capture path
	if err != nil {
		return nil, fmt.Errorf("pcapfile: open %s: %w", path, err)
	}
	bw := bufio.NewWriter(f)
	if err := writeGlobalHeader(bw, snaplen, uint32(lt)); err != nil {
		_ = bw.Flush()
		_ = f.Close()
		return nil, fmt.Errorf("pcapfile: write header: %w", err)
	}
	return &Sink{f: f, bw: bw, snapLen: snaplen}, nil
}

// WriteFrame appends one captured frame stamped at tsUnixNano. Errors are
// swallowed by design: a broken capture file must never take down the data path.
// An empty frame or a closed sink is a no-op.
func (s *Sink) WriteFrame(tsUnixNano int64, f link.Frame) {
	if s == nil || len(f) == 0 {
		return
	}
	data := f
	if uint32(len(data)) > s.snapLen {
		data = data[:s.snapLen]
	}
	secs := uint32(tsUnixNano / 1_000_000_000)
	usecs := uint32((tsUnixNano % 1_000_000_000) / 1_000)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return
	}
	var hdr [16]byte
	bp.PutLE32(hdr[0:4], secs)
	bp.PutLE32(hdr[4:8], usecs)
	bp.PutLE32(hdr[8:12], uint32(len(data))) // incl_len (captured)
	bp.PutLE32(hdr[12:16], uint32(len(f)))   // orig_len (on the wire)
	if _, err := s.bw.Write(hdr[:]); err != nil {
		return
	}
	_, _ = s.bw.Write(data)
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

// writeGlobalHeader emits the 24-byte libpcap global header (little-endian,
// microsecond magic).
func writeGlobalHeader(w *bufio.Writer, snaplen, linktype uint32) error {
	var h [24]byte
	bp.PutLE32(h[0:4], magicMicros)
	bp.PutLE16(h[4:6], 2)          // version major
	bp.PutLE16(h[6:8], 4)          // version minor
	bp.PutLE32(h[8:12], 0)         // thiszone (GMT)
	bp.PutLE32(h[12:16], 0)        // sigfigs
	bp.PutLE32(h[16:20], snaplen)  // snaplen
	bp.PutLE32(h[20:24], linktype) // network (DLT)
	_, err := w.Write(h[:])
	return err
}
