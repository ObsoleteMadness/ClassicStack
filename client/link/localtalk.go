//go:build !tinygo

// LToUDP needs a real multicast UDP socket (golang.org/x/net/ipv4, via
// adapter/link/ltoudp), which TinyGo's baremetal targets don't implement (see
// localtalk_tinygo.go for the stub those targets get instead).

package link

import (
	"fmt"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/adapter/link/framing"
	"github.com/ObsoleteMadness/ClassicStack/adapter/link/ltoudp"
	"github.com/ObsoleteMadness/ClassicStack/adapter/link/tashtalk"
	"github.com/ObsoleteMadness/ClassicStack/adapter/serial"
	"github.com/ObsoleteMadness/ClassicStack/client/trace"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
)

// claimTimeout bounds how long claimLocalTalk waits for the LLAP node-claim to
// complete before giving up. The claim itself finishes within ProbeCount*ProbeInterval
// (2s at the spec default, llap.DefaultProbeCount=8 @ 250ms) even with zero peers on
// the segment — an unanswered probe burst is exactly what accepts the candidate, per
// spec. This timeout is a safety net against a pathological reroll storm (a peer
// contesting every candidate we try), not the normal case.
const claimTimeout = 5 * time.Second

// openLToUDP opens the LToUDP multicast segment with LLAP framing (mirrors atlink).
// When claim is true it runs a real LLAP ENQ/ACK node-claim (srcNode is the desired
// first candidate; the actual claimed node — returned — may differ if that candidate
// is taken); when false it asserts network/srcNode directly with no negotiation, the
// original behaviour. The Logger is wired to the shared client trace sink (client/trace)
// so `-v` surfaces peer activity and malformed-frame drops the same way the server side
// already logs them (adapter/link/ltoudp: "ltoudp: peer seen" / "ltoudp: dropping
// malformed frame from peer") — without it, a peer answering with a corrupt frame looks
// identical to a peer that never answered at all, from every client probe tool (csnbp,
// csecho, csclient).
func openLToUDP(iface string, network uint16, srcNode uint8, claim bool) (link.DatagramLink, uint8, error) {
	cfg := ltoudp.DefaultConfig(iface)
	cfg.Logger = trace.Logger("ltoudp")
	fl, err := ltoudp.Open(cfg)
	if err != nil {
		return nil, 0, fmt.Errorf("open LToUDP: %w", err)
	}
	if !claim {
		dl, err := frameLocalTalk(fl, network, srcNode)
		return dl, srcNode, err
	}
	// RespondToEnq: true — LToUDP is a shared simulated segment (spec §"respondToEnq
	// Flag"), so this node must defend its claimed address against a later ENQ itself;
	// there is no hardware medium to do it for us the way a real LocalTalk line does.
	return claimLocalTalk(fl, network, srcNode, true)
}

// openTashTalk opens a TashTalk serial adapter with LLAP framing (mirrors atlink).
// See openLToUDP for the claim parameter and return values.
func openTashTalk(device string, baud uint, network uint16, srcNode uint8, claim bool) (link.DatagramLink, uint8, error) {
	if device == "" {
		return nil, 0, fmt.Errorf("tashtalk transport needs a device (a serial port path)")
	}
	s, err := serial.Open(serial.Config{Device: device, Baud: baud})
	if err != nil {
		return nil, 0, fmt.Errorf("open serial %s: %w", device, err)
	}
	fl, err := tashtalk.NewStream(s)
	if err != nil {
		_ = s.Close()
		return nil, 0, fmt.Errorf("frame TashTalk: %w", err)
	}
	if !claim {
		dl, err := frameLocalTalk(fl, network, srcNode)
		return dl, srcNode, err
	}
	// RespondToEnq: false — the physical LocalTalk medium defends a claimed node in
	// hardware (the TashTalk adapter's own node filter), unlike the simulated LToUDP
	// segment above.
	return claimLocalTalk(fl, network, srcNode, false)
}

// claimLocalTalk runs a real LLAP ENQ/ACK node-claim (adapter/link/framing.LocalTalk,
// EnableClaim) over fl, starting from desiredNode as the first candidate, and blocks
// until a node is accepted (or claimTimeout elapses). It returns the framed
// DatagramLink and the node actually claimed — which may differ from desiredNode if
// that candidate was already taken and the engine rerolled.
func claimLocalTalk(fl link.FrameLink, seedNetwork uint16, desiredNode uint8, respondToEnq bool) (link.DatagramLink, uint8, error) {
	live := &framing.LiveAddr{}
	claimed := make(chan uint8, 1)
	framer := &framing.LocalTalk{
		Addr:         live,
		EnableClaim:  true,
		Live:         live,
		RespondToEnq: respondToEnq,
		SeedNetwork:  seedNetwork,
		DesiredNode:  desiredNode,
		Logger:       trace.Logger("llap"),
		OnClaimed: func(_ uint16, node uint8, _, _ uint16) {
			select {
			case claimed <- node:
			default:
			}
		},
	}
	dl, err := framer.Framing(fl)
	if err != nil {
		_ = fl.Close()
		return nil, 0, fmt.Errorf("frame LocalTalk: %w", err)
	}
	select {
	case node := <-claimed:
		return dl, node, nil
	case <-time.After(claimTimeout):
		_ = dl.Close()
		return nil, 0, fmt.Errorf("LLAP node-claim timed out after %s (pass -claim=false to assert a node directly instead)", claimTimeout)
	}
}
