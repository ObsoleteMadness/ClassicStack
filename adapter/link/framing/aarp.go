package framing

// aarp.go is the AARP-aware EtherTalk framer: a SEPARATE link.Framer from the plain
// EtherTalk (phase-2 SNAP-DDP) framer in this package — it does NOT overload it. It owns
// a pure core/protocol/aarp.Engine and the same FrameLink, so on one Ethernet link it:
//
//   - CLAIMS a unique AppleTalk node address by probing (background goroutine), then
//     publishes it via the LiveAddr (src stamping) + an OnClaimed callback (compose wires
//     that to port.SetAddress) — the EtherTalk analogue of LocalTalk LLAP node-claim;
//   - SERVICES inbound AARP frames (answer Requests/Probes for our address, glean peers,
//     defend our address, age the AMT) while the read loop still only sees DDP;
//   - RESOLVES the destination node→MAC via the AMT so outbound DDP goes UNICAST instead
//     of always broadcast (today's behaviour).
//
// It reuses this package's in-package SNAP frame helpers (appendEthSNAP, snapPIDOf,
// decode) and the LiveAddr/Addr seam from localtalk.go. Until a node is claimed
// (Addr.Node()==0) outbound DDP is DROPPED — the same "drop until claimed" contract the
// LocalTalk framer + runport already use. The timing (probe interval, AMT tick) lives
// here; the pure engine takes an explicit `now`.

import (
	"errors"
	"math/rand"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/aarp"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
)

const (
	// probeInterval is the gap between node-claim probes (Linux msleep(100)).
	probeInterval = 100 * time.Millisecond
	// amtTickInterval drives AMT aging + resolve retransmits.
	amtTickInterval = time.Second
)

// EtherTalkAARP is the AARP-aware EtherTalk Framer. Compose builds it with the station
// MAC, a LiveAddr (shared with the port), and the seed network range, then wires
// OnClaimed to port.SetAddress + LiveAddr.Set.
type EtherTalkAARP struct {
	// SrcMAC is this station's 6-byte hardware address (sender on all frames).
	SrcMAC []byte
	// Addr is the live claimed address the framer stamps and reads (shared with the
	// port). The claim goroutine Set()s it once an address is accepted.
	Addr *LiveAddr
	// SeedNetMin/SeedNetMax bound the network number the tentative address is drawn from
	// (the EtherTalk startup-range seed). When both are 0 the startup network 0 is used
	// until a router teaches the real range.
	SeedNetMin, SeedNetMax uint16
	// OnClaimed is called once a node address is accepted, so compose can drive
	// port.SetAddress. nil is allowed (the LiveAddr update alone suffices for framing).
	OnClaimed func(network uint16, node uint8, netMin, netMax uint16)
	// RandNode picks a tentative node value (1..254); nil → a default random source.
	RandNode func() uint8
	// ProbeCount / ProbeInterval override the claim probe burst (0 → the defaults:
	// DefaultProbeCount probes at probeInterval). Tests set a small count + interval to
	// claim quickly.
	ProbeCount    int
	ProbeInterval time.Duration

	// live points at the most recent aarpLink built by Framing, so a diagnostic can read
	// its AMT (AARPTable) without the framer owning the table. A port reopens on every
	// Start (the libpcap handle is terminal), so this is replaced each Framing call; the
	// mutex guards the swap against a concurrent AARPTable read.
	mu   sync.Mutex
	live *aarpLink
}

// Framing wraps a FrameLink as an AARP-aware DatagramLink and starts the claim goroutine
// + AMT ticker. It returns immediately (async claim): the port comes up at once and
// outbound DDP is dropped until the claim publishes a node.
func (e *EtherTalkAARP) Framing(fl link.FrameLink) (link.DatagramLink, error) {
	if fl == nil {
		return nil, errors.New("framing: nil FrameLink")
	}
	var srcMAC [6]byte
	copy(srcMAC[:], e.SrcMAC)

	d := &aarpLink{
		fl:            fl,
		engine:        aarp.NewEngine(aarp.Config{HardwareAddr: srcMAC, ProbeCount: e.ProbeCount}),
		srcMAC:        srcMAC,
		addr:          e.Addr,
		seedMin:       e.SeedNetMin,
		seedMax:       e.SeedNetMax,
		onClaimed:     e.OnClaimed,
		randNode:      e.RandNode,
		probeInterval: e.ProbeInterval,
		done:          make(chan struct{}),
	}
	if d.randNode == nil {
		d.randNode = defaultRandNode
	}
	if d.probeInterval <= 0 {
		d.probeInterval = probeInterval
	}
	e.mu.Lock()
	e.live = d
	e.mu.Unlock()
	d.wg.Add(2)
	go d.claimLoop()
	go d.tickLoop()
	return d, nil
}

// AARPTable returns a snapshot of the current AMT (address→MAC mappings) for diagnostics,
// or nil before the first Start (no link yet). It reads the most recently built link's
// table under the link's own lock, so it is safe to call concurrently with the read/claim/
// tick paths. The compose layer wires it to the EtherTalk port's AARPTable accessor.
func (e *EtherTalkAARP) AARPTable() []aarp.Entry {
	e.mu.Lock()
	d := e.live
	e.mu.Unlock()
	if d == nil {
		return nil
	}
	return d.amtSnapshot()
}

var _ link.Framer = (*EtherTalkAARP)(nil)
var _ link.DatagramLink = (*aarpLink)(nil)

// aarpLink is the AARP-aware DatagramLink. The read loop services AARP and surfaces DDP;
// the write path resolves the dest MAC via the engine's AMT.
type aarpLink struct {
	fl     link.FrameLink
	srcMAC [6]byte
	addr   *LiveAddr

	seedMin, seedMax uint16
	onClaimed        func(uint16, uint8, uint16, uint16)
	randNode         func() uint8
	probeInterval    time.Duration

	mu     sync.Mutex // guards the engine (read loop, claim, tick all touch it)
	engine *aarp.Engine

	done chan struct{}
	wg   sync.WaitGroup
}

// ReadDatagram reads frames until one is a DDP datagram, returning it. AARP frames are
// fed to the engine (and any replies written back) and then skipped; everything else is
// skipped. Errors from the link (ErrTimeout/ErrClosed) surface to the caller.
func (d *aarpLink) ReadDatagram() (ddp.Datagram, error) {
	for {
		frame, err := d.fl.Read()
		if err != nil {
			return ddp.Datagram{}, err
		}
		pid, off, ok := snapPIDOf(frame)
		if !ok {
			continue // not an 802.2 SNAP frame
		}
		switch {
		case equal(pid, snapAppleTalk):
			dg, derr := ddp.Decode(frame[off:])
			if derr != nil {
				continue
			}
			return dg, nil
		case equal(pid, snapAARP):
			d.serviceAARP(frame[off:])
			continue
		default:
			continue
		}
	}
}

// amtSnapshot returns a copy of the engine's AMT under the engine lock (diagnostics).
func (d *aarpLink) amtSnapshot() []aarp.Entry {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.engine.AMT().Entries()
}

// serviceAARP feeds one inbound AARP payload to the engine and writes back any replies.
func (d *aarpLink) serviceAARP(payload []byte) {
	now := time.Now().UnixNano()
	d.mu.Lock()
	replies, _ := d.engine.Inbound(payload, now)
	d.mu.Unlock()
	for _, r := range replies {
		d.writeAARP(r)
	}
}

// WriteDatagram resolves the destination MAC and writes the DDP frame. Before a node is
// claimed (Addr node 0) the datagram is DROPPED. A broadcast destination uses the
// AppleTalk broadcast MAC; a unicast destination uses the AMT (unicast) or, on a miss,
// kicks off resolution and falls back to broadcast for this one datagram.
func (d *aarpLink) WriteDatagram(dg ddp.Datagram) error {
	if d.addr == nil || d.addr.Node() == 0 {
		return nil // unclaimed — drop, like the LocalTalk pre-claim contract
	}

	dst := append([]byte(nil), appleTalkBroadcastMAC...)
	if dg.DestNode != 0 && dg.DestNode != 0xFF {
		want := aarp.ProtoAddr{Network: dg.DestNetwork, Node: dg.DestNode}
		d.mu.Lock()
		hw, ok := d.engine.Resolve(want)
		var req []byte
		if !ok {
			req = d.engine.StartResolve(want, time.Now().UnixNano())
		}
		d.mu.Unlock()
		if ok {
			copy(dst, hw[:])
		} else if req != nil {
			d.writeAARP(req) // broadcast the resolution request; this dg falls back to broadcast
		}
	}

	frame, err := encode(nil, d.srcMAC[:], dst, dg)
	if err != nil {
		return err
	}
	return d.fl.Write(frame)
}

// writeAARP frames an AARP packet (the bytes after the SNAP header) under the AARP SNAP
// PID and writes it. AARP packets always go to the AppleTalk broadcast MAC (requests,
// probes) or carry their own target — broadcasting is correct for all the engine emits.
func (d *aarpLink) writeAARP(pkt []byte) {
	frame := appendEthSNAP(nil, appleTalkBroadcastMAC, d.srcMAC[:], snapAARP, pkt)
	_ = d.fl.Write(frame)
}

// Close stops the claim/tick goroutines and closes the link.
func (d *aarpLink) Close() error {
	select {
	case <-d.done:
	default:
		close(d.done)
	}
	err := d.fl.Close()
	d.wg.Wait()
	return err
}

// claimLoop runs the node-address acquisition: pick a tentative address, probe it, and on
// conflict pick another; on success publish via LiveAddr + OnClaimed. It exits on
// success or Close.
func (d *aarpLink) claimLoop() {
	defer d.wg.Done()
	for {
		tent := aarp.ProtoAddr{Network: d.seedNetwork(), Node: d.randNode()}
		d.mu.Lock()
		d.engine.BeginProbe(tent)
		d.mu.Unlock()

		if d.probeOnce() {
			return // claimed or closed
		}
		// conflict → loop and pick a new tentative address
	}
}

// probeOnce sends the probe burst for the current tentative address. It returns true when
// the address is accepted (claim done, publishes it) or the link closes; false on a
// conflict (the caller restarts with a new tentative).
func (d *aarpLink) probeOnce() bool {
	for {
		d.mu.Lock()
		pkt, ok := d.engine.NextProbe()
		conflicted := d.engine.Conflicted()
		d.mu.Unlock()

		if conflicted {
			return false
		}
		if !ok {
			// No probes left and no conflict → accept.
			d.mu.Lock()
			claimed, accepted := d.engine.AcceptTentative()
			d.mu.Unlock()
			if accepted {
				d.publishClaim(claimed)
			}
			return true
		}
		d.writeAARP(pkt)

		select {
		case <-d.done:
			return true
		case <-time.After(d.probeInterval):
		}
	}
}

// publishClaim records the claimed address into the LiveAddr (so the framer stamps it)
// and notifies compose via OnClaimed (so the port's SetAddress runs). The network range
// passed to OnClaimed is the seed range (a router refines it later via RTMP).
func (d *aarpLink) publishClaim(a aarp.ProtoAddr) {
	if d.addr != nil {
		d.addr.Set(NewStaticAddr(a.Network, a.Node))
	}
	if d.onClaimed != nil {
		d.onClaimed(a.Network, a.Node, d.seedMin, d.seedMax)
	}
}

// tickLoop drives AMT aging + resolve retransmits until Close.
func (d *aarpLink) tickLoop() {
	defer d.wg.Done()
	t := time.NewTicker(amtTickInterval)
	defer t.Stop()
	for {
		select {
		case <-d.done:
			return
		case <-t.C:
			now := time.Now().UnixNano()
			d.mu.Lock()
			reqs := d.engine.Tick(now)
			d.mu.Unlock()
			for _, r := range reqs {
				d.writeAARP(r)
			}
		}
	}
}

// seedNetwork picks a network number from the seed range for a tentative address. When
// the range is unset (0/0) it returns the startup network 0 (a router teaches the real
// network later; the node value is what AARP actually probes for uniqueness).
func (d *aarpLink) seedNetwork() uint16 {
	if d.seedMin == 0 && d.seedMax == 0 {
		return 0
	}
	if d.seedMax <= d.seedMin {
		return d.seedMin
	}
	return d.seedMin + uint16(rand.Intn(int(d.seedMax-d.seedMin)+1))
}

// defaultRandNode picks a node value in the valid AppleTalk range 1..254 (0 invalid,
// 255 broadcast).
func defaultRandNode() uint8 {
	return uint8(1 + rand.Intn(254))
}
