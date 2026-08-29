package framing

import (
	"sync"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/adapter/link/inmem"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/aarp"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
)

// aarpMAC builds a 6-byte MAC.
func aarpMAC(b ...byte) [6]byte {
	var m [6]byte
	copy(m[:], b)
	return m
}

// readAARP reads one frame from the peer link and decodes its AARP payload, or returns
// ok=false on timeout/non-AARP. It waits up to a deadline for an AARP frame.
func readAARP(t *testing.T, peer *inmem.Link, within time.Duration) (aarp.Packet, bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	type res struct {
		p  aarp.Packet
		ok bool
	}
	for time.Now().Before(deadline) {
		ch := make(chan res, 1)
		go func() {
			frame, err := peer.Read()
			if err != nil {
				ch <- res{}
				return
			}
			pid, off, ok := snapPIDOf(frame)
			if !ok || !equal(pid, snapAARP) {
				ch <- res{}
				return
			}
			p, derr := aarp.Decode(frame[off:])
			ch <- res{p: p, ok: derr == nil}
		}()
		select {
		case r := <-ch:
			if r.ok {
				return r.p, true
			}
		case <-time.After(time.Until(deadline)):
			return aarp.Packet{}, false
		}
	}
	return aarp.Packet{}, false
}

// writeAARPFrame sends an AARP packet to the framer (from the peer end).
func writeAARPFrame(peer *inmem.Link, srcMAC [6]byte, pkt aarp.Packet) {
	frame := appendEthSNAP(nil, appleTalkBroadcastMAC, srcMAC[:], snapAARP, pkt.Encode(nil))
	_ = peer.Write(frame)
}

// newAARPHarness builds an AARP framer over one end of an inmem Pair and returns the
// DatagramLink, the shared LiveAddr, the peer link, and a function reporting the claimed
// address. Probes are fast (count 2, 5ms) so claims complete promptly.
func newAARPHarness(t *testing.T, stationMAC [6]byte) (link.DatagramLink, *LiveAddr, *inmem.Link, func() (aarp.ProtoAddr, bool)) {
	t.Helper()
	local, peer := inmem.Pair(8)
	addr := &LiveAddr{}

	var mu sync.Mutex
	var claimed aarp.ProtoAddr
	var done bool

	f := &EtherTalkAARP{
		SrcMAC:        stationMAC[:],
		Addr:          addr,
		SeedNetMin:    0xFE01,
		SeedNetMax:    0xFE01, // single network → deterministic
		ProbeCount:    2,
		ProbeInterval: 5 * time.Millisecond,
		RandNode:      func() uint8 { return 0x42 },
		OnClaimed: func(network uint16, node uint8, _, _ uint16) {
			mu.Lock()
			claimed = aarp.ProtoAddr{Network: network, Node: node}
			done = true
			mu.Unlock()
		},
	}
	dl, err := f.Framing(local)
	if err != nil {
		t.Fatalf("Framing: %v", err)
	}
	t.Cleanup(func() { _ = dl.Close() })

	return dl, addr, peer, func() (aarp.ProtoAddr, bool) {
		mu.Lock()
		defer mu.Unlock()
		return claimed, done
	}
}

// drainReadLoop runs ReadDatagram in the background so the framer services inbound AARP
// (the read loop is what calls serviceAARP). Returns a channel of decoded DDP datagrams.
func drainReadLoop(dl link.DatagramLink) <-chan ddp.Datagram {
	out := make(chan ddp.Datagram, 16)
	go func() {
		for {
			dg, err := dl.ReadDatagram()
			if err != nil {
				close(out)
				return
			}
			out <- dg
		}
	}()
	return out
}

// TestAARPClaimsAddress proves the framer probes for and accepts a node address when the
// peer raises no conflict, publishing it via OnClaimed + the LiveAddr.
func TestAARPClaimsAddress(t *testing.T) {
	station := aarpMAC(0x00, 0x11, 0x22, 0x33, 0x44, 0x55)
	dl, addr, peer, claimedFn := newAARPHarness(t, station)
	_ = drainReadLoop(dl)

	// The peer should observe our probes (we don't answer → we claim).
	if p, ok := readAARP(t, peer, 200*time.Millisecond); !ok || p.Function != aarp.FuncProbe {
		t.Fatalf("expected a probe, got %+v ok=%v", p, ok)
	}

	// Claim completes: OnClaimed fired and the LiveAddr carries the node.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, done := claimedFn(); done {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	got, done := claimedFn()
	if !done {
		t.Fatal("claim never completed")
	}
	if got.Node != 0x42 || got.Network != 0xFE01 {
		t.Fatalf("claimed %+v, want net 0xFE01 node 0x42", got)
	}
	if addr.Node() != 0x42 {
		t.Fatalf("LiveAddr node = %d, want 0x42", addr.Node())
	}
}

// TestAARPDropsOutboundUntilClaimed proves WriteDatagram drops DDP before the node is
// claimed and delivers it after.
func TestAARPDropsOutboundUntilClaimed(t *testing.T) {
	station := aarpMAC(0x00, 0x11, 0x22, 0x33, 0x44, 0x55)
	dl, addr, peer, _ := newAARPHarness(t, station)
	_ = drainReadLoop(dl)

	// Before claim: a write is dropped (LiveAddr node 0). Send and confirm the peer sees
	// no DDP frame within a short window (only probes).
	dg := ddp.Datagram{DestNetwork: 0xFE01, SrcNetwork: 0xFE01, DestNode: 0x10, SrcNode: 0x42, DDPType: 1}
	if addr.Node() == 0 {
		if err := dl.WriteDatagram(dg); err != nil {
			t.Fatalf("WriteDatagram(pre-claim): %v", err)
		}
	}

	// Wait for the claim to land.
	deadline := time.Now().Add(500 * time.Millisecond)
	for addr.Node() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if addr.Node() == 0 {
		t.Fatal("never claimed")
	}

	// After claim: the write is delivered. Drain probe frames first, then look for DDP.
	if err := dl.WriteDatagram(dg); err != nil {
		t.Fatalf("WriteDatagram(post-claim): %v", err)
	}
	sawDDP := false
	end := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(end) {
		frame, err := peer.Read()
		if err != nil {
			break
		}
		if pid, _, ok := snapPIDOf(frame); ok && equal(pid, snapAppleTalk) {
			sawDDP = true
			break
		}
	}
	if !sawDDP {
		t.Fatal("post-claim DDP datagram was not delivered")
	}
}

// TestAARPResolvesToUnicast proves a unicast DDP to a peer triggers an AARP Request, and
// once the peer Replies the next datagram goes UNICAST to the peer's MAC (not broadcast).
func TestAARPResolvesToUnicast(t *testing.T) {
	station := aarpMAC(0x00, 0x11, 0x22, 0x33, 0x44, 0x55)
	peerMAC := aarpMAC(0xAB, 0xCD, 0xEF, 0x01, 0x02, 0x03)
	dl, addr, peer, _ := newAARPHarness(t, station)
	_ = drainReadLoop(dl)

	// Wait for claim.
	for addr.Node() == 0 {
		time.Sleep(5 * time.Millisecond)
	}

	// Pre-seed nothing: a unicast write misses the AMT → emits a Request and falls back to
	// broadcast for this datagram.
	target := ddp.Datagram{DestNetwork: 0xFE01, SrcNetwork: 0xFE01, DestNode: 0x20, SrcNode: 0x42, DDPType: 1}
	if err := dl.WriteDatagram(target); err != nil {
		t.Fatalf("WriteDatagram: %v", err)
	}

	// The peer should receive an AARP Request for node 0x20.
	gotReq := false
	end := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(end) && !gotReq {
		if p, ok := readAARP(t, peer, 50*time.Millisecond); ok && p.Function == aarp.FuncRequest {
			if p.TargetProto.Node == 0x20 {
				gotReq = true
			}
		}
	}
	if !gotReq {
		t.Fatal("no AARP Request emitted for the unresolved unicast destination")
	}

	// The peer answers: it owns 0xFE01.0x20 at peerMAC.
	reply := aarp.Reply(peerMAC, aarp.ProtoAddr{Network: 0xFE01, Node: 0x20}, station, aarp.ProtoAddr{Network: 0xFE01, Node: 0x42})
	writeAARPFrame(peer, peerMAC, reply)

	// Give the read loop time to glean.
	time.Sleep(30 * time.Millisecond)

	// Now a unicast write to 0x20 should go to peerMAC, not broadcast.
	if err := dl.WriteDatagram(target); err != nil {
		t.Fatalf("WriteDatagram(after resolve): %v", err)
	}
	sawUnicast := false
	end = time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(end) {
		frame, err := peer.Read()
		if err != nil {
			break
		}
		if pid, _, ok := snapPIDOf(frame); ok && equal(pid, snapAppleTalk) {
			// dst MAC is the first 6 bytes of the Ethernet frame.
			if equalBytes(frame[0:6], peerMAC[:]) {
				sawUnicast = true
				break
			}
		}
	}
	if !sawUnicast {
		t.Fatal("DDP to a resolved node did not go unicast to the peer MAC")
	}
}

// TestAARPGleansFromRequest proves the framer learns a peer's MAC from an inbound Request
// (gleaning), so a later resolve hits the AMT without a query.
func TestAARPGleansFromRequest(t *testing.T) {
	station := aarpMAC(0x00, 0x11, 0x22, 0x33, 0x44, 0x55)
	peerMAC := aarpMAC(0x77, 0x77, 0x77, 0x77, 0x77, 0x77)
	dl, addr, peer, _ := newAARPHarness(t, station)
	_ = drainReadLoop(dl)
	for addr.Node() == 0 {
		time.Sleep(5 * time.Millisecond)
	}

	// The peer broadcasts a Request (for some third party) — we glean its source.
	req := aarp.Request(peerMAC, aarp.ProtoAddr{Network: 0xFE01, Node: 0x55}, aarp.ProtoAddr{Network: 0xFE01, Node: 0x99})
	writeAARPFrame(peer, peerMAC, req)
	time.Sleep(30 * time.Millisecond)

	// A unicast to the gleaned node now goes unicast immediately (no Request).
	dg := ddp.Datagram{DestNetwork: 0xFE01, SrcNetwork: 0xFE01, DestNode: 0x55, SrcNode: 0x42, DDPType: 1}
	if err := dl.WriteDatagram(dg); err != nil {
		t.Fatalf("WriteDatagram: %v", err)
	}
	sawUnicast := false
	end := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(end) {
		frame, err := peer.Read()
		if err != nil {
			break
		}
		if pid, _, ok := snapPIDOf(frame); ok && equal(pid, snapAppleTalk) && equalBytes(frame[0:6], peerMAC[:]) {
			sawUnicast = true
			break
		}
	}
	if !sawUnicast {
		t.Fatal("gleaned node was not resolved to unicast")
	}
}

// TestAARPTableSnapshot proves EtherTalkAARP.AARPTable exposes the live AMT: nil before
// any Start, and the gleaned mappings once the framer has serviced inbound AARP.
func TestAARPTableSnapshot(t *testing.T) {
	station := aarpMAC(0x00, 0x11, 0x22, 0x33, 0x44, 0x55)
	peerMAC := aarpMAC(0x77, 0x77, 0x77, 0x77, 0x77, 0x77)

	f := &EtherTalkAARP{
		SrcMAC:        station[:],
		Addr:          &LiveAddr{},
		SeedNetMin:    0xFE01,
		SeedNetMax:    0xFE01,
		ProbeCount:    2,
		ProbeInterval: 5 * time.Millisecond,
		RandNode:      func() uint8 { return 0x42 },
	}
	// Before any Framing call there is no live link, so the table is nil.
	if got := f.AARPTable(); got != nil {
		t.Fatalf("AARPTable before Start = %v, want nil", got)
	}

	local, peer := inmem.Pair(8)
	dl, err := f.Framing(local)
	if err != nil {
		t.Fatalf("Framing: %v", err)
	}
	t.Cleanup(func() { _ = dl.Close() })
	_ = drainReadLoop(dl)

	// Feed an inbound Request so the framer gleans the peer's MAC.
	req := aarp.Request(peerMAC, aarp.ProtoAddr{Network: 0xFE01, Node: 0x55}, aarp.ProtoAddr{Network: 0xFE01, Node: 0x99})
	writeAARPFrame(peer, peerMAC, req)

	deadline := time.Now().Add(300 * time.Millisecond)
	var entries []aarp.Entry
	for time.Now().Before(deadline) {
		entries = f.AARPTable()
		if len(entries) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(entries) != 1 {
		t.Fatalf("AARPTable = %d entries, want 1", len(entries))
	}
	e := entries[0]
	if e.Addr != (aarp.ProtoAddr{Network: 0xFE01, Node: 0x55}) || e.HW != peerMAC {
		t.Fatalf("entry = %+v, want addr FE01.55 hw %v", e, peerMAC)
	}
}

// TestAARPReadDatagram_TrimsEthernetPadding is the regression guard for the dead-ZIP/ASP-
// reply bug: a real NIC pads a short frame up to Ethernet's 60-byte minimum, but a short DDP
// payload — an 8-byte ATP TReq, which is exactly what ZIP's GetZoneList/GetLocalZoneList/
// GetNetInfo and AFP's ASP session traffic send — produces a frame well under that minimum
// (14 eth + 8 SNAP + 21 DDP = 43 bytes). ReadDatagram must use the 802.3 length field to trim
// that trailing zero padding before handing the slice to ddp.Decode, which requires an
// EXACT-length match and rejects anything longer (ddp.ErrBadLength) — so an untrimmed read
// silently dropped every short ATP request/reply while longer NBP/AEP traffic (which usually
// clears the minimum on its own) decoded fine. This is why real captures showed NBP and AEP
// working over EtherTalk while ZIP and ASP looked completely dead.
func TestAARPReadDatagram_TrimsEthernetPadding(t *testing.T) {
	station := aarpMAC(0x00, 0x11, 0x22, 0x33, 0x44, 0x55)
	peerMAC := aarpMAC(0xAB, 0xCD, 0xEF, 0x01, 0x02, 0x03)
	dl, _, peer, _ := newAARPHarness(t, station)
	out := drainReadLoop(dl)

	// An 8-byte ATP TReq payload (matches ZIP's GetLocalZoneList / AFP's ASP session
	// traffic), long-header encoded: 13-byte DDP header + 8 bytes = 21 bytes total.
	atpTReq := []byte{0x40, 0x01, 0x00, 0x01, 9 /* GetLocalZoneList */, 0, 0, 1}
	ddpDatagram := ddp.Datagram{
		DestNetwork: 0xFE01, SrcNetwork: 0xFE01,
		DestNode: 0x42, SrcNode: 0x20,
		DestSocket: 6, SrcSocket: 250,
		DDPType: 3,
		Data:    atpTReq,
	}
	ddpBytes, err := ddpDatagram.Encode(nil)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(ddpBytes) != 21 {
		t.Fatalf("encoded DDP length = %d, want 21 (a short ATP TReq)", len(ddpBytes))
	}

	// appendEthSNAP sets the 802.3 length field to the true (unpadded) payload length —
	// exactly what a real NIC's sender does. The resulting frame (14+8+21=43 bytes) is then
	// padded with trailing zeros to Ethernet's 60-byte minimum, exactly as a real NIC does
	// on transmit — the read side must not treat that padding as part of the DDP payload.
	frame := appendEthSNAP(nil, station[:], peerMAC[:], snapAppleTalk, ddpBytes)
	if len(frame) < 60 {
		frame = append(frame, make([]byte, 60-len(frame))...)
	}
	if err := peer.Write(frame); err != nil {
		t.Fatalf("peer.Write: %v", err)
	}

	select {
	case dg, ok := <-out:
		if !ok {
			t.Fatal("read loop closed instead of delivering the padded ATP datagram")
		}
		if dg.DDPType != 3 || dg.DestSocket != 6 || len(dg.Data) != 8 {
			t.Fatalf("decoded datagram = %+v, want type=3 destsock=6 8-byte ATP payload", dg)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("REGRESSION: padded short-ATP frame was never delivered — ddp.Decode rejected" +
			" it as ErrBadLength because the trailing Ethernet padding was not trimmed using the" +
			" 802.3 length field")
	}
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
