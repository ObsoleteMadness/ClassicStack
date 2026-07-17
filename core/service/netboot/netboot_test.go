package netboot

import (
	"bytes"
	"context"
	"io"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/protocol/abp"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

// fakeRouter is a minimal router.ServiceRouter that records replies.
type fakeRouter struct {
	mu      sync.Mutex
	replies []reply
}

type reply struct {
	ddpType uint8
	data    []byte
}

func (f *fakeRouter) Reply(_ ddp.Datagram, _ router.RoutedPort, ddpType uint8, data []byte) {
	f.mu.Lock()
	f.replies = append(f.replies, reply{ddpType: ddpType, data: append([]byte(nil), data...)})
	f.mu.Unlock()
}
func (f *fakeRouter) Route(ddp.Datagram, bool) error      { return nil }
func (f *fakeRouter) RoutingTable() *router.RoutingTable  { return nil }
func (f *fakeRouter) Zones() *router.ZoneInformationTable { return nil }
func (f *fakeRouter) Ports() []router.RoutedPort          { return nil }

func (f *fakeRouter) waitReplies(n int) []reply {
	for range 5000 {
		f.mu.Lock()
		got := len(f.replies)
		f.mu.Unlock()
		if got >= n {
			break
		}
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]reply(nil), f.replies...)
}

// memDisk is an in-memory Disk for EBP tests.
type memDisk struct {
	mu  sync.Mutex
	buf []byte
}

func (d *memDisk) ReadAt(p []byte, off int64) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if off >= int64(len(d.buf)) {
		return 0, io.EOF
	}
	n := copy(p, d.buf[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (d *memDisk) WriteAt(p []byte, off int64) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if end := off + int64(len(p)); end > int64(len(d.buf)) {
		d.buf = append(d.buf, make([]byte, end-int64(len(d.buf)))...)
	}
	return copy(d.buf[off:], p), nil
}

func (d *memDisk) Size() int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return int64(len(d.buf))
}

// fakeRegistrar records NBP name registrations.
type fakeRegistrar struct {
	mu         sync.Mutex
	registered []string
	removed    []string
}

func (f *fakeRegistrar) RegisterNameAnyObject(obj, typ, zone []byte, socket uint8) {
	f.mu.Lock()
	f.registered = append(f.registered, string(obj)+":"+string(typ)+"@"+string(zone))
	f.mu.Unlock()
}

func (f *fakeRegistrar) UnregisterName(obj, typ, zone []byte) {
	f.mu.Lock()
	f.removed = append(f.removed, string(obj)+":"+string(typ)+"@"+string(zone))
	f.mu.Unlock()
}

func pattern(n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = byte(i*13 + 1)
	}
	return out
}

func startService(t *testing.T, fr *fakeRouter, cfg Config) *Service {
	t.Helper()
	if cfg.Pace == 0 {
		cfg.Pace = time.Microsecond // keep flood tests fast
	}
	if cfg.ChainPace == 0 {
		cfg.ChainPace = time.Microsecond // keep chunk-read tests fast
	}
	s := New(fr, cfg, nil)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	return s
}

func TestMapUserReply(t *testing.T) {
	fr := &fakeRouter{}
	payload := pattern(4 * abp.DiskSector)
	startService(t, fr, Config{Payload: payload}).Inbound(ddp.Datagram{
		DDPType: abp.DDPType,
		SrcNode: 42,
		Data: abp.UserRecordRequest{
			MachineID: 7, // PRAM osType — NOT echoed into osID
			Timestamp: 0x00BEEF00,
			UserName:  []byte("Patrick"),
		}.Marshal(),
	}, nil)

	got := fr.waitReplies(1)
	if len(got) != 1 {
		t.Fatalf("got %d replies, want 1", len(got))
	}
	if got[0].ddpType != abp.DDPType {
		t.Fatalf("reply ddpType = %d", got[0].ddpType)
	}
	if len(got[0].data) != abp.DDPMaxData {
		t.Fatalf("reply length = %d, want %d", len(got[0].data), abp.DDPMaxData)
	}
	var rep abp.BootPktRply
	if err := rep.Unmarshal(got[0].data); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if rep.OSID != abp.MachineMac {
		t.Errorf("osID = %d, want MACHINE_MAC(1)", rep.OSID)
	}
	if rep.UserData != 0x00BEEF00 {
		t.Errorf("userData = %#x, want the echoed timestamp", rep.UserData)
	}
	if rep.BlockSize != abp.DiskSector || rep.ImageSize != 4 || rep.Result != 0 {
		t.Errorf("reply fields = %+v", rep)
	}
}

// TestImageRequestHonorsBitmap checks the retransmit path: a non-empty bitmap
// sends exactly the wanted blocks (LSB-first bit order).
func TestImageRequestHonorsBitmap(t *testing.T) {
	fr := &fakeRouter{}
	payload := pattern(16 * abp.DiskSector)
	// Want blocks 0, 3, 9 and 15: bytes {0b0000_1001, 0b1000_0010}.
	startService(t, fr, Config{Payload: payload}).Inbound(ddp.Datagram{
		DDPType: abp.DDPType,
		Data:    abp.BootImageRequest{ImageID: 1, Bitmap: []byte{0x09, 0x82}}.Marshal(),
	}, nil)

	got := fr.waitReplies(4)
	if len(got) != 4 {
		t.Fatalf("got %d blocks, want 4", len(got))
	}
	wantBlocks := []uint16{0, 3, 9, 15}
	for i, r := range got {
		var blk abp.BootBlock
		if err := blk.Unmarshal(r.data); err != nil {
			t.Fatalf("block %d: %v", i, err)
		}
		if blk.BlockNo != wantBlocks[i] {
			t.Fatalf("block %d has blockNo %d, want %d", i, blk.BlockNo, wantBlocks[i])
		}
		if !bytes.Equal(blk.Data, payload[int(blk.BlockNo)*abp.DiskSector:(int(blk.BlockNo)+1)*abp.DiskSector]) {
			t.Fatalf("block %d data mismatch", i)
		}
	}
}

// TestImageRequestFloods checks the flood path: every block is sent 0-based
// when the request bitmap is empty (the buggy small-image client, spec/19).
func TestImageRequestFloods(t *testing.T) {
	fr := &fakeRouter{}
	payload := pattern(5 * abp.DiskSector)
	startService(t, fr, Config{Payload: payload}).Inbound(ddp.Datagram{
		DDPType: abp.DDPType,
		Data:    abp.BootImageRequest{ImageID: 3}.Marshal(), // empty bitmap
	}, nil)

	got := fr.waitReplies(5)
	if len(got) != 5 {
		t.Fatalf("got %d blocks, want 5", len(got))
	}
	for i, r := range got {
		var blk abp.BootBlock
		if err := blk.Unmarshal(r.data); err != nil {
			t.Fatalf("block %d: %v", i, err)
		}
		if int(blk.BlockNo) != i {
			t.Fatalf("block %d has blockNo %d (must be 0-based, in order)", i, blk.BlockNo)
		}
		if blk.ImageID != 3 {
			t.Fatalf("block %d imageID = %d, want the echoed 3", i, blk.ImageID)
		}
		if !bytes.Equal(blk.Data, payload[i*abp.DiskSector:(i+1)*abp.DiskSector]) {
			t.Fatalf("block %d data mismatch", i)
		}
	}
}

// TestImageRequestRotatesOrder: consecutive send rounds start at successive
// block offsets so a positionally-deterministic receiver loss pattern cannot
// pin the same blocks forever (spec/19 transfer discipline; the only defence
// for <9-block payloads whose bitmap is always empty).
func TestImageRequestRotatesOrder(t *testing.T) {
	fr := &fakeRouter{}
	payload := pattern(7 * abp.DiskSector) // ChainLoader-sized: empty-bitmap regime
	s := startService(t, fr, Config{Payload: payload})

	s.Inbound(ddp.Datagram{DDPType: abp.DDPType, Data: abp.BootImageRequest{}.Marshal()}, nil)
	if got := fr.waitReplies(7); len(got) != 7 {
		t.Fatalf("round 1: %d blocks", len(got))
	}
	s.Inbound(ddp.Datagram{DDPType: abp.DDPType, Data: abp.BootImageRequest{}.Marshal()}, nil)
	got := fr.waitReplies(14)
	if len(got) != 14 {
		t.Fatalf("round 2: %d blocks total", len(got))
	}

	blockNo := func(r reply) uint16 {
		var blk abp.BootBlock
		if err := blk.Unmarshal(r.data); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		return blk.BlockNo
	}
	if blockNo(got[0]) != 0 {
		t.Fatalf("round 1 starts at block %d, want 0", blockNo(got[0]))
	}
	if blockNo(got[7]) != 1 {
		t.Fatalf("round 2 starts at block %d, want rotated start 1", blockNo(got[7]))
	}
	// Round 2 still covers every block exactly once.
	seen := map[uint16]bool{}
	for _, r := range got[7:] {
		seen[blockNo(r)] = true
	}
	if len(seen) != 7 {
		t.Fatalf("round 2 covered %d distinct blocks, want 7", len(seen))
	}
}

func TestChainReadClampAndZeroFill(t *testing.T) {
	fr := &fakeRouter{}
	disk := &memDisk{buf: pattern(3 * abp.ChainBlockSize)}
	startService(t, fr, Config{Payload: pattern(2 * abp.DiskSector), Disk: disk}).Inbound(ddp.Datagram{
		DDPType: abp.DDPType,
		Data:    abp.ChainReadRequest{Seq: 9, BlockOffset: 1, BlockCount: 100}.Marshal(),
	}, nil)

	// The burst is delayed one pace then sent in rotated order (client
	// listener-enable race + positional-loss convergence, spec/19), so verify
	// by BlkIndex, not position.
	got := fr.waitReplies(abp.ChunkBlocks)
	if len(got) != abp.ChunkBlocks {
		t.Fatalf("got %d blocks, want blockCount clamped to %d", len(got), abp.ChunkBlocks)
	}
	byIdx := map[uint8][]byte{}
	for _, r := range got {
		var blk abp.ChainReadData
		if err := blk.Unmarshal(r.data); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if blk.Seq != 9 {
			t.Fatalf("block seq = %d, want 9", blk.Seq)
		}
		if _, dup := byIdx[blk.BlkIndex]; dup {
			t.Fatalf("block %d sent twice", blk.BlkIndex)
		}
		byIdx[blk.BlkIndex] = blk.Data
	}
	if len(byIdx) != abp.ChunkBlocks {
		t.Fatalf("got %d distinct block indexes, want %d", len(byIdx), abp.ChunkBlocks)
	}
	if !bytes.Equal(byIdx[0], disk.buf[abp.ChainBlockSize:2*abp.ChainBlockSize]) {
		t.Fatalf("block 0 data mismatch")
	}
	// Block offset 1+2 = 3 is past EOF (disk is 3 blocks): zero-filled.
	if !bytes.Equal(byIdx[2], make([]byte, abp.ChainBlockSize)) {
		t.Fatalf("past-EOF block not zero-filled")
	}
}

// TestChainBackoffPace pins the retry pace escalation: double per consecutive
// retry, capped so the burst fits inside the client's 1 s retry timer, floored
// at the configured base (real-time-speed snow ingest overrun, spec/19).
func TestChainBackoffPace(t *testing.T) {
	base := 10 * time.Millisecond
	for _, tc := range []struct {
		retries int
		count   uint32
		want    time.Duration
	}{
		{0, 10, base},                                     // first request: base pace
		{1, 10, 20 * time.Millisecond},                    // retry doubles
		{2, 10, 40 * time.Millisecond},                    // and doubles again
		{3, 10, 800 * time.Millisecond / 11},              // capped: burst ≤ 800 ms
		{100, 10, 800 * time.Millisecond / 11},            // shift saturates at the cap
		{2, abp.ChunkBlocks, 800 * time.Millisecond / 33}, // full chunk caps sooner
		{1, 1, 20 * time.Millisecond},                     // tiny burst: plain doubling
	} {
		if got := chainBackoffPace(base, tc.retries, tc.count); got != tc.want {
			t.Errorf("chainBackoffPace(%v, %d, %d) = %v, want %v",
				base, tc.retries, tc.count, got, tc.want)
		}
	}
	// A misconfigured base larger than the cap is never reduced below itself.
	big := 50 * time.Millisecond
	if got := chainBackoffPace(big, 1, abp.ChunkBlocks); got != big {
		t.Errorf("floored pace = %v, want base %v", got, big)
	}
}

// TestChainReadRetryTracking drives the retry detector: re-requesting the same
// chunk (fresh seq — the client's timer behavior) counts retries; asking for a
// different chunk resets the state.
func TestChainReadRetryTracking(t *testing.T) {
	fr := &fakeRouter{}
	disk := &memDisk{buf: pattern(8 * abp.ChainBlockSize)}
	s := startService(t, fr, Config{Payload: pattern(2 * abp.DiskSector), Disk: disk})

	read := func(seq uint16, offset, count uint32) {
		s.Inbound(ddp.Datagram{DDPType: abp.DDPType, SrcNode: 42, Data: abp.ChainReadRequest{
			Seq: seq, BlockOffset: offset, BlockCount: count,
		}.Marshal()}, nil)
	}
	key := uint32(42) << 8 // net 0, node 42, socket 0
	state := func() chainReadState {
		_ = s.Stop(context.Background()) // worker done — safe to inspect state
		st := s.chainRetry[key]
		if st == nil {
			t.Fatalf("no retry state recorded for client key %#x", key)
		}
		return *st
	}

	read(1, 0, 2)
	read(2, 0, 2) // same chunk again = retry
	read(3, 0, 2) // and again
	fr.waitReplies(6)
	if st := state(); st.offset != 0 || st.count != 2 || st.retries != 2 {
		t.Fatalf("retry state after 3 identical requests = %+v, want offset 0 count 2 retries 2", st)
	}

	// A different chunk resets the counter (fresh service = fresh state map).
	fr = &fakeRouter{}
	s = startService(t, fr, Config{Payload: pattern(2 * abp.DiskSector), Disk: disk})
	read(1, 0, 2)
	read(2, 0, 2) // retry...
	read(3, 4, 2) // ...then progress
	fr.waitReplies(6)
	if st := state(); st.offset != 4 || st.count != 2 || st.retries != 0 {
		t.Fatalf("retry state after progress = %+v, want offset 4 count 2 retries 0", st)
	}
}

// TestChainWriteCommit drives a 2-block write chunk and checks the commit and
// the 131 ack, mirroring ChainBoot.py's window semantics (commit truncated at
// the bit7-flagged block).
func TestChainWriteCommit(t *testing.T) {
	fr := &fakeRouter{}
	disk := &memDisk{buf: make([]byte, 8*abp.ChainBlockSize)}
	s := startService(t, fr, Config{Payload: pattern(2 * abp.DiskSector), Disk: disk})

	blockA := bytes.Repeat([]byte{0xAA}, abp.ChainBlockSize)
	blockB := bytes.Repeat([]byte{0xBB}, abp.ChainBlockSize)
	s.Inbound(ddp.Datagram{DDPType: abp.DDPType, SrcNode: 5, Data: abp.ChainWriteBlock{
		BlkIndex: 0, Seq: 77, ImageNum: 1, HunkStart: 2, Data: blockA,
	}.Marshal()}, nil)
	s.Inbound(ddp.Datagram{DDPType: abp.DDPType, SrcNode: 5, Data: abp.ChainWriteBlock{
		BlkIndex: 1 | abp.ChainLastFlag, Seq: 77, ImageNum: 1, HunkStart: 2, Data: blockB,
	}.Marshal()}, nil)

	got := fr.waitReplies(1)
	if len(got) != 1 {
		t.Fatalf("got %d replies, want 1 ack", len(got))
	}
	var ack abp.ChainWriteAck
	if err := ack.Unmarshal(got[0].data); err != nil {
		t.Fatalf("Unmarshal ack: %v", err)
	}
	if ack.Seq != 77 {
		t.Fatalf("ack seq = %d, want 77", ack.Seq)
	}
	if !bytes.Equal(disk.buf[2*abp.ChainBlockSize:3*abp.ChainBlockSize], blockA) ||
		!bytes.Equal(disk.buf[3*abp.ChainBlockSize:4*abp.ChainBlockSize], blockB) {
		t.Fatalf("chunk not committed at hunkStart")
	}
	if !bytes.Equal(disk.buf[4*abp.ChainBlockSize:5*abp.ChainBlockSize], make([]byte, abp.ChainBlockSize)) {
		t.Fatalf("commit overran the flagged block")
	}
}

func TestNBPRegistrationLifecycle(t *testing.T) {
	fr := &fakeRouter{}
	reg := &fakeRegistrar{}
	s := New(fr, Config{Payload: pattern(2 * abp.DiskSector)}, nil)
	s.SetNBP(reg)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if len(reg.registered) != 1 || reg.registered[0] != "0000:BootServer@*" {
		t.Fatalf("registered = %v", reg.registered)
	}
	if len(reg.removed) != 1 || reg.removed[0] != "0000:BootServer@*" {
		t.Fatalf("removed = %v", reg.removed)
	}
}

func TestChainSocketShimForwards(t *testing.T) {
	fr := &fakeRouter{}
	s := startService(t, fr, Config{Payload: pattern(2 * abp.DiskSector), Disk: &memDisk{buf: make([]byte, abp.ChainBlockSize)}})
	extras := s.ExtraRouterServices()
	if len(extras) != 1 || extras[0].Socket() != ChainSocket {
		t.Fatalf("extras = %v", extras)
	}
	extras[0].Inbound(ddp.Datagram{
		DDPType: abp.DDPType,
		Data:    abp.ChainReadRequest{Seq: 1, BlockOffset: 0, BlockCount: 1}.Marshal(),
	}, nil)
	if got := fr.waitReplies(1); len(got) != 1 {
		t.Fatalf("shim did not forward: %d replies", len(got))
	}
}

func TestInboundAfterStopDoesNotPanic(t *testing.T) {
	fr := &fakeRouter{}
	s := New(fr, Config{Payload: pattern(2 * abp.DiskSector)}, nil)
	_ = s.Start(context.Background())
	_ = s.Stop(context.Background())
	s.Inbound(ddp.Datagram{DDPType: abp.DDPType, Data: abp.UserRecordRequest{}.Marshal()}, nil)
}
