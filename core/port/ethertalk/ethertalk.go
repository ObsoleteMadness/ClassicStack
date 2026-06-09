// Package ethertalk is the real (M3) EtherTalk port: DDP over Ethernet/SNAP.
//
// It consumes a link.FrameLink (raw Ethernet frames, supplied by an adapter
// such as adapter/link/pcap or adapter/link/inmem) and a link.Framer (the
// Ethernet/SNAP DDP framer, adapter/link/framing.EtherTalk) — both injected by
// the composition layer, since core may not import adapters. The read loop,
// metering, and lifecycle live in the shared runport base.
//
// Node-claim / address resolution on EtherTalk is driven by AARP, which is
// deferred (TODO(M3-AARP)); the port comes up and frames DDP, and SetAddress
// can be called once a claim completes. The framing seam already sends to the
// AppleTalk broadcast MAC, so broadcast traffic works without AARP.
package ethertalk

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

// Name is the component/section key for the EtherTalk port.
const Name = "EtherTalk"

// Port is the real EtherTalk port. It embeds the runport base (lifecycle, read
// loop, metering, RoutedPort data half) and adds the EtherTalk framing.
type Port struct {
	*runport.Port
}

// New builds the real EtherTalk port. frame is the raw Ethernet FrameLink
// (nil → inert: the port satisfies the lifecycle but moves no data, which keeps
// the registry path working until compose injects a device link). framer turns
// that FrameLink into a DDP DatagramLink (nil → the default Ethernet/SNAP framer
// cannot be built here in core, so a nil framer with a non-nil frame is an
// error). rtr is the router the port delivers inbound datagrams to (nil → drop
// until the router is wired in M4). Returns (nil, nil) when the section is
// disabled.
func New(m *config.Model, frame link.FrameLink, framer link.Framer, rtr router.Router, logger log.Logger) (component.Component, error) {
	sec := port.SectionFromModel(m, Name)
	if !sec.IsEnabled {
		return nil, nil
	}
	if frame != nil && framer == nil {
		return nil, errors.New("ethertalk: frame link supplied without a framer")
	}

	open := buildLinkFactory(frame, framer)
	p := &Port{Port: runport.New(sec, open, rtr, logger)}
	// The rx-port identity handed to router.Inbound must be this outer *Port (the
	// router uses it to avoid echoing a datagram back out the interface it
	// arrived on). It exists only after runport.New, so set it now.
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
