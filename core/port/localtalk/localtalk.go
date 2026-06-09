// Package localtalk is the real (M3) LocalTalk port: DDP over LLAP. It is
// transport-agnostic — the concrete LocalTalk medium (LToUDP multicast UDP,
// TashTalk serial, or Virtual) is whichever link.FrameLink adapter the
// composition layer injects; the LLAP framer (link.Framer) turns that frame
// stream into DDP datagrams. Both are injected because core may not import
// adapters.
//
// Like EtherTalk, the read loop, metering, and lifecycle live in the shared
// runport base; this package only wires the LocalTalk-specific framing seam.
// LocalTalk node-claim is the LLAP ENQ/ACK dance, performed in the framer/link
// adapter (M3-deferred where not yet implemented); SetAddress records the claim.
package localtalk

import (
	"errors"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
	"github.com/ObsoleteMadness/ClassicStack/core/port/internal/runport"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

// Name is the component/section key for the LocalTalk port.
const Name = "LocalTalk"

// Port is the real LocalTalk port. It embeds the runport base (lifecycle, read
// loop, metering, RoutedPort data half) and adds the LocalTalk framing.
type Port struct {
	*runport.Port
}

// New builds the real LocalTalk port. frame is the LocalTalk FrameLink (nil →
// inert until compose injects a transport link). framer turns that into a DDP
// DatagramLink via LLAP (nil with a non-nil frame is an error). rtr is the
// router the port delivers inbound datagrams to (nil → drop until M4). Returns
// (nil, nil) when the section is disabled.
func New(m *config.Model, frame link.FrameLink, framer link.Framer, rtr router.Router, logger log.Logger) (component.Component, error) {
	sec := port.SectionFromModel(m, Name)
	if !sec.IsEnabled {
		return nil, nil
	}
	if frame != nil && framer == nil {
		return nil, errors.New("localtalk: frame link supplied without a framer")
	}

	open := buildLinkFactory(frame, framer)
	p := &Port{Port: runport.New(sec, open, rtr, logger)}
	p.SetOwner(p)
	return p, nil
}

// buildLinkFactory returns a runport.LinkFactory that frames the injected
// FrameLink on each Start. A nil frame yields a nil-link factory (inert).
func buildLinkFactory(frame link.FrameLink, framer link.Framer) runport.LinkFactory {
	if frame == nil {
		return func() (link.DatagramLink, error) { return nil, nil }
	}
	return func() (link.DatagramLink, error) {
		return framer.Framing(frame)
	}
}
