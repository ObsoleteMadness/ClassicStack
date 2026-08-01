// SPDX-FileCopyrightText: Based on Netboot code by Elliot Nunn
// SPDX-License-Identifier: MIT
// Package netboot implements the classic Mac netboot server: the AppleTalk Boot
// Protocol (ABP) the `.netBOOT`/`.ATBOOT` ROM drivers speak, plus Elliot Nunn's
// ChainBoot EBP extension that streams a full-size read/write HFS disk image to
// the chain-loaded driver.
//
// ABP rides DDP type 10 on the boot socket; EBP rides the SAME DDP type on
// boot socket + 1 (the chain-loaded client salvages the ABP server address and
// increments the socket). Discovery is NBP: the client looks up
// `<serverID-hex>:BootServer@*`, so the service registers an any-object
// BootServer name (the object encodes client PRAM and is echoed back).
//
// Ring: CORE (stdlib only). The router is injected at construction; the service
// rides it as a router.Service (lifecycle + socket dispatch) and exposes the
// EBP socket binding via ExtraRouterServices.
//
// Reference: spec/19-netboot.md. Protocol reverse-engineered by Elliot Nunn
// (NetBoot project) and verified against Apple's SuperMario os/netboot source.
package netboot

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/abp"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

// Name is the component/section key for the netboot service.
const Name = "Netboot"

const (
	// Socket is the ABP boot socket this server advertises via NBP (mirrors
	// Apple's ABPSckt; any free static socket works since clients learn it
	// from the NBP tuple).
	Socket = 10
	// ChainSocket is the EBP socket: always the advertised boot socket + 1
	// (Client.a increments the salvaged ABP socket).
	ChainSocket = Socket + 1
	// NBPType is the NBP type name booting clients look up.
	NBPType = "BootServer"
	// DefaultPace is the default inter-packet delay for the ABP block flood.
	//
	// This is netboot's SERVICE-level pace. It coexists with the LocalTalk port's
	// per-destination-node pacing (core/link.Pace, configured via a port's pace_ms):
	// the port enforces a universal minimum inter-frame gap for EVERY service (the
	// floor that keeps any producer from overrunning a slow classic-Mac receiver on a
	// backpressure-free LToUDP segment), while this value is netboot's OWN base gap.
	// The effective gap is the larger of the two, so on LToUDP (default port floor
	// 3 ms) the flood paces at 3 ms even though this is 2 ms; on TashTalk (port floor
	// 0, the serial line self-paces) it paces at this value. Netboot keeps its own
	// pace because it also drives the retry-aware backoff (chainBackoffPace) the port
	// cannot express — see DefaultChainPace.
	DefaultPace = 2 * time.Millisecond
	// DefaultChainPace is the default inter-packet delay for ChainBoot EBP
	// read-reply bursts. The chain client must catch every block of a chunk in
	// ONE burst — its progress bitmap resets on each timer retry — and its
	// interrupt-level listener overruns at the ABP flood rate (observed: 32-block
	// chunks retried 9× at 2 ms, spec/19). Real LocalTalk can never deliver a
	// 530-byte frame faster than ~18 ms (230.4 kbit/s), so 10 ms is still 2×
	// real line rate.
	//
	// This stays a service-level pace (not the port floor) because the chain path
	// ALSO backs it off per consecutive retry (chainBackoffPace): a re-request means
	// the previous burst was dropped mid-assembly, so each retry doubles the gap,
	// bounded to land inside the client's 1 s retry timer. The port's fixed per-node
	// floor cannot express that retry-aware escalation, so netboot owns it and the
	// port floor merely raises the base when it is the larger of the two.
	DefaultChainPace = 10 * time.Millisecond
)

// Disk is the writable EBP disk-image seam: the compose edge opens the
// configured image file and hands it in (no file I/O in core). Reads beyond
// Size are zero-filled; writes may extend the file.
type Disk interface {
	io.ReaderAt
	io.WriterAt
	Size() int64
}

// NameRegistrar is the minimal NBP seam netboot uses to advertise itself. It is
// a LOCAL interface — structurally satisfied by *core/service/nbp.Service — so
// this package does not import the NBP service (the same acyclicity discipline
// as AFP's registrar seam). A nil registrar means "no NBP in this build": the
// server answers ABP but is not discoverable by name.
type NameRegistrar interface {
	RegisterNameAnyObject(obj, typ, zone []byte, socket uint8)
	UnregisterName(obj, typ, zone []byte)
}

type item struct {
	d    ddp.Datagram
	from router.RoutedPort
}

// writeWindow accumulates one in-flight EBP write chunk (≤ 32 × 512 bytes),
// keyed by the client address; a new seq resets it (ChainBoot.py semantics).
type writeWindow struct {
	seq  uint16
	hunk uint32 // hunkStart carried by the chunk's blocks
	got  uint32 // bitmap of block indexes received
	buf  [abp.ChunkBlocks * abp.ChainBlockSize]byte
}

// Service is the netboot responder. It queues inbound datagrams from both
// sockets and serves them on one worker goroutine so the router's read path
// never blocks (and ABP/EBP handling is naturally serialized).
type Service struct {
	rtr    router.ServiceRouter
	logger log.Logger

	// Immutable serving state, set before Start by the compose factory.
	payload   []byte // ABP boot payload, Snefru trailer included
	blockSize int    // ABP block size (512 payload / 256 chain loader)
	disk      Disk   // EBP disk image; nil = EBP disabled
	pace      time.Duration
	chainPace time.Duration
	nbpObject string // NBP object registered (cosmetic — matching is any-object)
	zone      string // NBP zone; "" = "*"

	mu      sync.Mutex
	enabled bool
	running bool
	names   NameRegistrar
	ch      chan item
	stop    chan struct{}
	wg      sync.WaitGroup

	windows map[uint32]*writeWindow // EBP write windows keyed by net<<16|node<<8|socket

	// chainRetry tracks the last chunk each client asked for (same key as
	// windows; worker-goroutine only). A re-request of the same (offset, count)
	// means the client failed to assemble the previous burst — ChainLoader
	// resets its progress bitmap on retry, so replying with identical timing is
	// a fixed point that never converges. Backing the pace off per retry
	// self-tunes to whatever rate the client can actually drain (observed: a
	// real-time-speed snow Mac drops mid-burst packets at 10 ms spacing and
	// looped forever on a 10-block read, while fast-forward kept up; real
	// LocalTalk delivers a 512-byte frame in ~20 ms).
	chainRetry map[uint32]*chainReadState

	// sendRound rotates the block-send starting offset (worker-goroutine only).
	// Receivers drop bursts with a POSITIONALLY-DETERMINISTIC pattern (the same
	// stream offsets lost every round, spec/19 transfer discipline); an
	// identical resend order is then a fixed point that never converges — fatal
	// for <9-block payloads whose request bitmap is always empty (client bug).
	// Rotating the start block each round lands the loss on different blocks.
	sendRound int

	// counters published as Stats (§5).
	statMu      sync.Mutex
	mapUsers    uint64
	imageReqs   uint64
	blocksTx    uint64
	chainReads  uint64
	chainWrites uint64
}

// Config carries the construction-time serving parameters.
type Config struct {
	Payload   []byte        // boot payload (trailered); empty = inert
	BlockSize int           // ABP block size; 0 → abp.DiskSector
	Disk      Disk          // writable EBP image; nil disables EBP
	Pace      time.Duration // flood inter-packet delay; 0 → DefaultPace
	ChainPace time.Duration // EBP read-reply inter-packet delay; 0 → DefaultChainPace
	NBPObject string        // registered object name; "" → "0000"
	Zone      string        // NBP zone; "" → "*"
}

// New builds a netboot service bound to the router it replies through.
func New(rtr router.ServiceRouter, cfg Config, logger log.Logger) *Service {
	if logger == nil {
		logger = log.New(Name)
	}
	if cfg.BlockSize <= 0 {
		cfg.BlockSize = abp.DiskSector
	}
	if cfg.Pace <= 0 {
		cfg.Pace = DefaultPace
	}
	if cfg.ChainPace <= 0 {
		cfg.ChainPace = DefaultChainPace
	}
	if cfg.NBPObject == "" {
		cfg.NBPObject = "0000"
	}
	return &Service{
		rtr:        rtr,
		logger:     logger,
		payload:    cfg.Payload,
		blockSize:  cfg.BlockSize,
		disk:       cfg.Disk,
		pace:       cfg.Pace,
		chainPace:  cfg.ChainPace,
		nbpObject:  cfg.NBPObject,
		zone:       cfg.Zone,
		windows:    map[uint32]*writeWindow{},
		chainRetry: map[uint32]*chainReadState{},
	}
}

// chainReadState records the last EBP read a client issued so a retry (same
// chunk re-requested, fresh seq) is distinguishable from progress.
type chainReadState struct {
	offset  uint32
	count   uint32
	retries int
}

// chainBackoffPace escalates the base EBP reply pace for a retried chunk:
// doubled per consecutive retry (capped at 16× base), bounded so the whole
// burst — the initial hold plus count blocks — lands well inside the client's
// 1-second retry timer (≤ 800 ms total), and never below the configured base.
//
// The ≤800 ms budget assumes this pace is the DOMINANT inter-frame delay. The
// LocalTalk port may add its own per-node floor (core/link.Pace) on top, but the
// default floor (3 ms) is well below the chain base (10 ms), so max(floor, pace) ==
// pace here and the budget holds. If a port floor is ever raised above the chain
// base the effective burst would grow — keep the floor default under DefaultChainPace.
func chainBackoffPace(base time.Duration, retries int, count uint32) time.Duration {
	if retries <= 0 {
		return base
	}
	pace := base * time.Duration(1<<min(retries, 4))
	if lim := 800 * time.Millisecond / time.Duration(count+1); pace > lim {
		pace = lim
	}
	return max(pace, base)
}

// Name returns the component name.
func (s *Service) Name() string { return Name }

// Socket returns the ABP boot socket so the router dispatches boot datagrams here.
func (s *Service) Socket() uint8 { return Socket }

// SetEnabled records the configured-enabled flag (component.Enableable), set by
// the compose factory from the section.
func (s *Service) SetEnabled(enabled bool) {
	s.mu.Lock()
	s.enabled = enabled
	s.mu.Unlock()
}

// Enabled reports the configured-enabled flag (component.Enableable).
func (s *Service) Enabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enabled
}

// SetNBP installs the NBP name-information service the server advertises with.
// Must be called before Start (the compose cross-wire does); nil skips
// registration. Idempotent.
func (s *Service) SetNBP(names NameRegistrar) {
	s.mu.Lock()
	s.names = names
	s.mu.Unlock()
}

// ExtraRouterServices exposes the EBP socket binding: a thin shim on
// ChainSocket forwarding into the same engine. The compose cross-wire
// registers it alongside the service itself.
func (s *Service) ExtraRouterServices() []router.Service {
	return []router.Service{&chainService{s: s}}
}

// zoneBytes resolves the NBP registration zone ("*" default).
func (s *Service) zoneBytes() []byte {
	if s.zone == "" {
		return []byte{'*'}
	}
	return []byte(s.zone)
}

// Start launches the responder goroutine and registers the BootServer NBP name.
// Idempotent (§3).
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil
	}
	s.running = true
	s.ch = make(chan item, 256)
	s.stop = make(chan struct{})
	s.wg.Add(1)
	go s.run(ctx, s.ch, s.stop)
	if s.names != nil {
		// Any-object registration: the client's lookup object encodes its PRAM
		// serverNum (nibble-reversed hex, spec/19), so match every object of
		// type BootServer and echo the requested one back.
		s.names.RegisterNameAnyObject([]byte(s.nbpObject), []byte(NBPType), s.zoneBytes(), Socket)
		s.logger.Log2(log.Debug, "netboot: registered NBP name (any-object)",
			log.Str("name", s.nbpObject+":"+NBPType+"@"+string(s.zoneBytes())),
			log.Int("socket", Socket))
	} else {
		s.logger.Log0(log.Debug, "netboot: no NBP service wired; server is not name-discoverable")
	}
	diskBytes := int64(0)
	if s.disk != nil {
		diskBytes = s.disk.Size()
	}
	s.logger.Log(log.Info, "netboot: started",
		log.Int("payload_blocks", int64(len(s.payload)/s.blockSize)),
		log.Int("block_size", int64(s.blockSize)),
		log.Bool("chainboot", s.disk != nil),
		log.Int("disk_bytes", diskBytes),
		log.Int("boot_socket", Socket),
		log.Int("chain_socket", ChainSocket))
	if len(s.payload) == 0 {
		s.logger.Log0(log.Warn, "netboot: no boot payload configured; boot requests will be ignored")
	}
	return nil
}

// Stop shuts the responder down and withdraws the NBP name. Safe after a
// partial Start (§3) and idempotent.
func (s *Service) Stop(ctx context.Context) error {
	_ = ctx
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	if s.names != nil {
		s.names.UnregisterName([]byte(s.nbpObject), []byte(NBPType), s.zoneBytes())
	}
	close(s.stop)
	s.mu.Unlock()
	s.wg.Wait()
	s.logger.Log0(log.Info, "netboot: stopped")
	return nil
}

// Inbound queues a datagram for the responder; a full queue drops (the client
// retransmits every request on its own timers, so drops are recoverable).
func (s *Service) Inbound(d ddp.Datagram, from router.RoutedPort) {
	s.mu.Lock()
	ch := s.ch
	running := s.running
	s.mu.Unlock()
	if !running {
		return
	}
	select {
	case ch <- item{d: d, from: from}:
	default:
	}
}

func (s *Service) run(ctx context.Context, ch chan item, stop chan struct{}) {
	defer s.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case it := <-ch:
			s.handlePacket(it.d, it.from, ch, stop)
		}
	}
}

// handlePacket dispatches on the ABP command byte (like the reference servers —
// the ABP and EBP command spaces are disjoint, so the receiving socket need not
// disambiguate).
func (s *Service) handlePacket(d ddp.Datagram, from router.RoutedPort, ch chan item, stop chan struct{}) {
	if d.DDPType != abp.DDPType {
		return
	}
	switch abp.Command(d.Data) {
	case abp.CmdUserRecordRequest:
		s.handleMapUser(d, from)
	case abp.CmdBootImageRequest:
		s.handleImageRequest(d, from, ch, stop)
	case abp.CmdChainRead:
		s.handleChainRead(d, from, stop)
	case abp.CmdChainWrite:
		s.handleChainWrite(d, from)
	case abp.CmdNullCommand, abp.CmdImageDone, abp.CmdUserRecordUpdate, abp.CmdUserUpdateReply:
		// Not part of the served boot path; ignore (spec/19).
	default:
		s.logger.Log2(log.Debug, "netboot: unknown ABP command",
			log.Int("cmd", int64(abp.Command(d.Data))), log.Int("from", int64(d.SrcNode)))
	}
}

// handleMapUser answers rbMapUser with the BootPktRply describing the payload.
// Stateless and idempotent — the client retransmits during discovery.
func (s *Service) handleMapUser(d ddp.Datagram, from router.RoutedPort) {
	if len(s.payload) == 0 {
		s.logger.Log1(log.Warn, "netboot: boot request ignored — no payload configured",
			log.Int("node", int64(d.SrcNode)))
		return // inert build (no payload configured)
	}
	var req abp.UserRecordRequest
	if err := req.Unmarshal(d.Data); err != nil {
		s.logger.Log1(log.Debug, "netboot: bad rbMapUser", log.Str("err", err.Error()))
		return
	}
	s.bump(&s.mapUsers)
	s.logger.Log(log.Info, "netboot: boot request (rbMapUser)",
		log.Str("user", string(req.UserName)),
		log.Int("machineID", int64(req.MachineID)),
		log.Int("net", int64(d.SrcNetwork)),
		log.Int("node", int64(d.SrcNode)),
		log.Int("socket", int64(d.SrcSocket)))
	reply := abp.BootPktRply{
		// osID MUST be MACHINE_MAC(1) — the client validates the constant, not
		// an echo of the request machineID (spec/19 errata).
		OSID: abp.MachineMac,
		// userData MUST echo the timestamp — it is the client's RTT source.
		UserData:  req.Timestamp,
		BlockSize: uint16(s.blockSize),
		ImageID:   0,
		Result:    0,
		ImageSize: uint32(len(s.payload) / s.blockSize),
	}
	s.rtr.Reply(d, from, abp.DDPType, reply.Marshal())
	s.logger.Log2(log.Debug, "netboot: sent rbUserReply",
		log.Int("image_blocks", int64(reply.ImageSize)),
		log.Int("block_size", int64(reply.BlockSize)))
}

// wantedBlocks lists the block numbers whose bit is set in an rbImageRequest
// bitmap (LSB-first within each byte, matching the client's SetBitmap/BSET),
// bounded by the payload block count. An empty result means the bitmap carried
// no usable bits (the small-image client bug, spec/19 errata) — flood instead.
func wantedBlocks(bitmap []byte, blocks int) []int {
	var out []int
	for blk := range blocks {
		if blk/8 < len(bitmap) && bitmap[blk/8]>>(blk%8)&1 == 1 {
			out = append(out, blk)
		}
	}
	return out
}

// handleImageRequest serves an rbImageRequest. A non-empty bitmap is HONOURED —
// only the wanted blocks are sent. This matters: the client's receive path
// overruns under a full flood with a positionally-repeating loss pattern (the
// same blocks lost every round, ltoudp pcap 2026-07-16), so re-flooding
// everything plateaus and never converges, while per-bitmap retransmits shift
// positions every round. An EMPTY bitmap floods every block (the initial
// request of a <9-block image is buggy-empty, spec/19 errata); the client
// dedups and re-requests on its own timers either way.
//
// Queued requests from the same client are coalesced first: the client
// re-requests while a send round is still running, and only its freshest
// bitmap matters — serving the stale ones would just repeat the overrun.
func (s *Service) handleImageRequest(d ddp.Datagram, from router.RoutedPort, ch chan item, stop chan struct{}) {
	if len(s.payload) == 0 {
		return
	}
	var req abp.BootImageRequest
	if err := req.Unmarshal(d.Data); err != nil {
		s.logger.Log1(log.Debug, "netboot: bad rbImageRequest", log.Str("err", err.Error()))
		return
	}
	s.bump(&s.imageReqs)

	// Coalesce: adopt the newest queued rbImageRequest from this client; stash
	// everything else to handle after the send round.
	var stash []item
drain:
	for {
		select {
		case it := <-ch:
			var newer abp.BootImageRequest
			if it.d.DDPType == abp.DDPType &&
				it.d.SrcNetwork == d.SrcNetwork && it.d.SrcNode == d.SrcNode && it.d.SrcSocket == d.SrcSocket &&
				newer.Unmarshal(it.d.Data) == nil {
				s.bump(&s.imageReqs)
				req = newer
				d = it.d
				from = it.from
				continue
			}
			stash = append(stash, it)
		default:
			break drain
		}
	}

	blocks := len(s.payload) / s.blockSize
	wanted := wantedBlocks(req.Bitmap, blocks)
	mode := "bitmap"
	if len(wanted) == 0 {
		for blk := range blocks {
			wanted = append(wanted, blk)
		}
		mode = "flood"
	}
	start := s.sendRound % len(wanted)
	s.sendRound++
	s.logger.Log(log.Info, "netboot: payload requested (rbImageRequest); sending blocks",
		log.Str("mode", mode),
		log.Int("blocks", int64(len(wanted))),
		log.Int("of", int64(blocks)),
		log.Int("rotate", int64(start)),
		log.Int("node", int64(d.SrcNode)))
	for i := range wanted {
		blk := wanted[(start+i)%len(wanted)]
		pkt := abp.BootBlock{
			ImageID: req.ImageID,
			BlockNo: uint16(blk), // 0-based on the wire (spec/19 errata)
			Data:    s.payload[blk*s.blockSize : (blk+1)*s.blockSize],
		}
		s.rtr.Reply(d, from, abp.DDPType, pkt.Marshal())
		s.bump(&s.blocksTx)
		select {
		case <-stop:
			s.logger.Log1(log.Debug, "netboot: payload send aborted by Stop",
				log.Int("sent", int64(i+1)))
			return
		case <-time.After(s.pace):
		}
	}
	s.logger.Log2(log.Debug, "netboot: payload send complete",
		log.Int("blocks", int64(len(wanted))), log.Int("node", int64(d.SrcNode)))

	for _, it := range stash {
		s.handlePacket(it.d, it.from, ch, stop)
	}
}

// handleChainRead serves an EBP chunk read: up to ChunkBlocks 512-byte blocks
// from the disk image, zero-filled past EOF (matching ChainBoot.py's slice
// semantics).
func (s *Service) handleChainRead(d ddp.Datagram, from router.RoutedPort, stop chan struct{}) {
	if s.disk == nil {
		s.logger.Log1(log.Warn, "netboot: chain read ignored — no disk image configured",
			log.Int("node", int64(d.SrcNode)))
		return
	}
	var req abp.ChainReadRequest
	if err := req.Unmarshal(d.Data); err != nil {
		s.logger.Log1(log.Debug, "netboot: bad chain read", log.Str("err", err.Error()))
		return
	}
	s.bump(&s.chainReads)
	if int64(req.BlockOffset)*abp.ChainBlockSize >= s.disk.Size() {
		// A read entirely past EOF is never a real request — it's a deranged
		// client (observed: a stale resend timer building requests from freed
		// memory, spec/19). Feeding it zero blocks lets it scribble RAM;
		// ChainBoot.py's slice semantics effectively drop these too.
		s.logger.Log2(log.Warn, "netboot: chain read beyond end of disk — ignored",
			log.Int("block_offset", int64(req.BlockOffset)), log.Int("node", int64(d.SrcNode)))
		return
	}
	count := min(req.BlockCount, abp.ChunkBlocks)
	if count == 0 {
		return
	}
	s.logger.Log(log.Debug, "netboot: chain read",
		log.Int("seq", int64(req.Seq)),
		log.Int("block_offset", int64(req.BlockOffset)),
		log.Int("blocks", int64(count)),
		log.Int("node", int64(d.SrcNode)),
		// The patched ChainLoader repurposes imageNum as a diagnostic: the
		// raw ioPosOffset seen at Prime with ioPosMode in the low 4 bits.
		log.Int("diag", int64(req.ImageNum)))
	// Retry-aware pacing: a re-request of the same chunk means the previous
	// burst was not fully assembled (the client resets its progress bitmap on
	// retry), so double the inter-packet pace each consecutive retry. The whole
	// burst must still land well inside the client's 1 s retry timer or the
	// next retry preempts it mid-burst.
	key := uint32(d.SrcNetwork)<<16 | uint32(d.SrcNode)<<8 | uint32(d.SrcSocket)
	st := s.chainRetry[key]
	if st != nil && st.offset == req.BlockOffset && st.count == count {
		st.retries++
	} else {
		st = &chainReadState{offset: req.BlockOffset, count: count}
		s.chainRetry[key] = st
	}
	pace := chainBackoffPace(s.chainPace, st.retries, count)
	if st.retries > 0 {
		s.logger.Log(log.Debug, "netboot: chain read retried — backing off pace",
			log.Int("seq", int64(req.Seq)),
			log.Int("retries", int64(st.retries)),
			log.Int("pace_ms", pace.Milliseconds()))
	}
	send := func(i uint32) bool {
		buf := make([]byte, abp.ChainBlockSize)
		off := (int64(req.BlockOffset) + int64(i)) * abp.ChainBlockSize
		if off < s.disk.Size() {
			if _, err := s.disk.ReadAt(buf, off); err != nil && !errors.Is(err, io.EOF) {
				s.logger.Log2(log.Warn, "netboot: disk read failed",
					log.Int("off", off), log.Str("err", err.Error()))
			}
		}
		pkt := abp.ChainReadData{BlkIndex: uint8(i), Seq: req.Seq, Data: buf}
		s.rtr.Reply(d, from, abp.DDPType, pkt.Marshal())
		s.bump(&s.blocksTx)
		select {
		case <-stop:
			return false
		case <-time.After(pace):
			return true
		}
	}
	// Hold the burst for one pace interval before the FIRST reply: the client
	// enables its packet filter in the async send-completion, so a reply that
	// beats it is trashed — and retries reset the progress bitmap, making the
	// loss a fixed point (observed: one 32-block chunk re-requested 73×).
	// A bookend duplicate was tried instead and crashed the client twice
	// (double-ReadRest; a frame landing in the System's SCC re-init window,
	// spec/19) — delaying, as with write acks, is safe. Blocks go in order:
	// this matches the reference servers, and the burst-initial delay removes
	// the deterministic first-block loss that order rotation compensated for.
	select {
	case <-stop:
		return
	case <-time.After(pace):
	}
	for i := range count {
		if !send(i) {
			return
		}
	}
}

// handleChainWrite accumulates one write chunk per client; the block flagged
// ChainLastFlag commits the window through that block to the disk image at
// hunkStart and acks with cmd 131. A new seq resets the window.
func (s *Service) handleChainWrite(d ddp.Datagram, from router.RoutedPort) {
	if s.disk == nil {
		s.logger.Log1(log.Warn, "netboot: chain write ignored — no disk image configured",
			log.Int("node", int64(d.SrcNode)))
		return
	}
	var req abp.ChainWriteBlock
	if err := req.Unmarshal(d.Data); err != nil {
		s.logger.Log1(log.Debug, "netboot: bad chain write", log.Str("err", err.Error()))
		return
	}
	s.bump(&s.chainWrites)

	key := uint32(d.SrcNetwork)<<16 | uint32(d.SrcNode)<<8 | uint32(d.SrcSocket)
	w := s.windows[key]
	if w == nil || w.seq != req.Seq {
		if w != nil && w.got != 0 {
			// The unpatched ChainLoader flags only the final block of the
			// whole REQUEST, so a multi-chunk write's intermediate chunks
			// never trigger the flag commit; discarding them here silently
			// loses 16 KB per chunk (observed: a 232-block flush vanished,
			// spec/19). Salvage the contiguous prefix instead.
			s.evictWindow(w, d.SrcNode)
		}
		w = &writeWindow{seq: req.Seq, hunk: req.HunkStart}
		s.windows[key] = w
	}
	idx := int(req.BlkIndex&^abp.ChainLastFlag) % abp.ChunkBlocks
	w.got |= 1 << idx
	data := req.Data
	if len(data) > abp.ChainBlockSize {
		data = data[:abp.ChainBlockSize]
	}
	copy(w.buf[idx*abp.ChainBlockSize:], data)

	if req.BlkIndex&abp.ChainLastFlag == 0 {
		return
	}
	// Last block of the chunk: commit buf[:(idx+1)*512] at hunkStart*512.
	commit := w.buf[:(idx+1)*abp.ChainBlockSize]
	off := int64(req.HunkStart) * abp.ChainBlockSize
	if req.HunkStart <= 1 {
		// A write landing on the boot blocks is almost certainly the client's
		// misdirected-position bug (spec/19: a catalog node was committed at
		// block 0, bricking the image) — commit anyway but make it unmissable.
		s.logger.Log2(log.Warn, "netboot: chain write targets the BOOT BLOCKS — verify the client meant it",
			log.Int("hunk_start", int64(req.HunkStart)), log.Int("node", int64(d.SrcNode)))
	}
	if _, err := s.disk.WriteAt(commit, off); err != nil {
		s.logger.Log2(log.Warn, "netboot: disk write failed",
			log.Int("off", off), log.Str("err", err.Error()))
		return // no ack — the client retransmits the chunk
	}
	delete(s.windows, key)
	// Hold the ack for one pace interval: the client enables its ack filter in
	// the async send-completion, and an ack that beats that completion is
	// either trashed (10 s stall) or — on the unpatched client, whose filter is
	// armed early — re-enters its send routine on a still-queued MPP parameter
	// block and hard-hangs the machine (spec/19).
	time.Sleep(s.chainPace)
	ack := abp.ChainWriteAck{Seq: req.Seq}
	s.rtr.Reply(d, from, abp.DDPType, ack.Marshal())
	s.logger.Log(log.Debug, "netboot: chain write committed",
		log.Int("seq", int64(req.Seq)),
		log.Int("hunk_start", int64(req.HunkStart)),
		log.Int("blocks", int64(idx+1)),
		log.Int("node", int64(d.SrcNode)),
		// See the chain-read handler: imageNum carries the client's raw
		// position diagnostic under the patched ChainLoader.
		log.Int("diag", int64(req.ImageNum)))
}

// evictWindow salvages a write window displaced by a new seq before any block
// carried the last flag: the contiguous block prefix is exactly the data the
// client streamed, so commit it rather than silently drop the chunk. No ack —
// the (buggy) client is not waiting for one.
func (s *Service) evictWindow(w *writeWindow, node uint8) {
	n := 0
	for n < abp.ChunkBlocks && w.got&(1<<n) != 0 {
		n++
	}
	if n == 0 {
		return
	}
	off := int64(w.hunk) * abp.ChainBlockSize
	if _, err := s.disk.WriteAt(w.buf[:n*abp.ChainBlockSize], off); err != nil {
		s.logger.Log2(log.Warn, "netboot: disk write failed",
			log.Int("off", off), log.Str("err", err.Error()))
		return
	}
	s.logger.Log(log.Warn, "netboot: chain write chunk had no last flag — committed on eviction",
		log.Int("seq", int64(w.seq)),
		log.Int("hunk_start", int64(w.hunk)),
		log.Int("blocks", int64(n)),
		log.Int("node", int64(node)))
}

// Stats publishes serving counters (§5).
func (s *Service) Stats() component.Stats {
	s.statMu.Lock()
	defer s.statMu.Unlock()
	return component.Stats{
		Counters: map[string]uint64{
			"map_user_requests": s.mapUsers,
			"image_requests":    s.imageReqs,
			"blocks_tx":         s.blocksTx,
			"chain_reads":       s.chainReads,
			"chain_writes":      s.chainWrites,
		},
		Gauges: map[string]float64{
			"payload_blocks": float64(len(s.payload) / s.blockSize),
		},
	}
}

func (s *Service) bump(c *uint64) {
	s.statMu.Lock()
	*c++
	s.statMu.Unlock()
}

// Dependencies declares netboot's start-order edge: the AppleTalk router must
// be running first. Drops in a no-router build.
func (s *Service) Dependencies() []string { return []string{router.Name} }

// chainService is the EBP socket shim: it binds ChainSocket and forwards into
// the owning service's queue. Lifecycle is a no-op — the engine's worker
// belongs to the main service.
type chainService struct{ s *Service }

func (c *chainService) Name() string                                   { return Name + "-EBP" }
func (c *chainService) Start(ctx context.Context) error                { return nil }
func (c *chainService) Stop(ctx context.Context) error                 { return nil }
func (c *chainService) Socket() uint8                                  { return ChainSocket }
func (c *chainService) Inbound(d ddp.Datagram, from router.RoutedPort) { c.s.Inbound(d, from) }

// compile-time assertions.
var (
	_ router.Service       = (*Service)(nil)
	_ router.Service       = (*chainService)(nil)
	_ component.Component  = (*Service)(nil)
	_ component.DependsOn  = (*Service)(nil)
	_ component.Statful    = (*Service)(nil)
	_ component.Enableable = (*Service)(nil)
)
