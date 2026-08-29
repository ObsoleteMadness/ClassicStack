package registry

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/gopacket/pcapgo"

	"github.com/ObsoleteMadness/ClassicStack/adapter/capture/pcapfile"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
)

// countRecords opens path with the same reader tshark uses and returns how many packet
// records it can read back (i.e. records durably on disk). An empty file (no bytes flushed
// yet — not even the global header) counts as 0 records: pcapgo returns EOF constructing a
// reader over it, which is the "nothing durable yet" state the flush path is meant to fix.
func countRecords(t *testing.T, path string) int {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	r, err := pcapgo.NewReader(f)
	if err != nil {
		return 0 // empty/headerless file: nothing durable
	}
	n := 0
	for {
		if _, _, err := r.ReadPacketData(); err != nil {
			return n
		}
		n++
	}
}

// resetCaptureSinks clears the process-wide sink registry so a test starts clean and does
// not leak an open file to later tests.
func resetCaptureSinks() {
	captureSinks.mu.Lock()
	captureSinks.byKey = map[string]*pcapfile.Sink{}
	captureSinks.mu.Unlock()
}

// TestFlushAndCloseCaptureSinks proves the shutdown-durability path: records written to a
// memoised sink are NOT on disk until flushed, FlushCaptureSinks makes them durable while
// leaving the sink writable, and CloseCaptureSinks finalises and forgets the sink so a
// re-open truncates fresh.
func TestFlushAndCloseCaptureSinks(t *testing.T) {
	resetCaptureSinks()
	t.Cleanup(func() { CloseCaptureSinks(); resetCaptureSinks() })

	path := filepath.Join(t.TempDir(), "cap.pcap")
	s := captureSink(path, pcapfile.LinkTypeEthernet, 0)
	if s == nil {
		t.Fatal("captureSink returned nil")
	}
	// A second request for the same path reuses the same sink (memoised).
	if s2 := captureSink(path, pcapfile.LinkTypeEthernet, 0); s2 != s {
		t.Fatal("captureSink did not memoise by path")
	}

	s.WriteFrame(1_000_000_000, link.Frame{0xDE, 0xAD, 0xBE, 0xEF})
	s.WriteFrame(2_000_000_000, link.Frame{0x01, 0x02, 0x03})

	// Before flush the records sit in the bufio buffer — the global header is on disk (a
	// valid empty capture) but no packet records yet.
	if n := countRecords(t, path); n != 0 {
		t.Fatalf("before flush: got %d records, want 0", n)
	}

	FlushCaptureSinks()
	if n := countRecords(t, path); n != 2 {
		t.Fatalf("after flush: got %d records, want 2", n)
	}

	// The sink is still writable after a flush.
	s.WriteFrame(3_000_000_000, link.Frame{0x99})
	CloseCaptureSinks()
	if n := countRecords(t, path); n != 3 {
		t.Fatalf("after close: got %d records, want 3", n)
	}

	// After CloseCaptureSinks the path is forgotten, so a new request opens a FRESH sink
	// (truncating the file) rather than handing back the closed one.
	s3 := captureSink(path, pcapfile.LinkTypeEthernet, 0)
	if s3 == nil || s3 == s {
		t.Fatal("captureSink after Close should open a fresh sink")
	}
	if n := countRecords(t, path); n != 0 {
		t.Fatalf("fresh sink truncated: got %d records, want 0", n)
	}
	// Close the fresh sink now so Windows can unlink the TempDir file (an open handle
	// blocks RemoveAll). CloseCaptureSinks also clears the registry.
	CloseCaptureSinks()
}

// TestStartCaptureFlusher proves the background flusher makes records durable on its tick
// without any explicit flush, and that its stop function halts it (and is idempotent).
func TestStartCaptureFlusher(t *testing.T) {
	resetCaptureSinks()
	t.Cleanup(func() { CloseCaptureSinks(); resetCaptureSinks() })

	// A non-positive interval disables the flusher — stop is a safe no-op.
	off := StartCaptureFlusher(0)
	off()
	off() // idempotent

	path := filepath.Join(t.TempDir(), "tick.pcap")
	s := captureSink(path, pcapfile.LinkTypeEthernet, 0)
	if s == nil {
		t.Fatal("captureSink returned nil")
	}
	s.WriteFrame(1_000_000_000, link.Frame{0xAA, 0xBB})

	stop := StartCaptureFlusher(20 * time.Millisecond)
	defer stop()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if countRecords(t, path) == 1 {
			stop()
			stop()              // idempotent
			CloseCaptureSinks() // release the file handle before TempDir cleanup (Windows)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("background flusher did not make the record durable within the deadline")
}
