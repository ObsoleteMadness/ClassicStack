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

	return newPort(sec, buildLinkFactory(frame, framer), rtr, logger), nil
}

// NewFromOpener builds the EtherTalk port from a per-Start FrameLink opener
// rather than a single pre-opened FrameLink. opener is called on every Start to
// obtain a FRESH raw-Ethernet link, which framer then wraps as a DDP
// DatagramLink — so the port survives a Stop→Start by reopening the device (a
// libpcap handle, once Closed on Stop, cannot be reused; see the pcap
// port-restart lifecycle). It is the constructor the composition layer uses once
// it can build a real device link from config; core stays free of the pcap/cgo
// adapter because opener is injected.
//
// A nil opener yields the inert form (no data path); a non-nil opener with a nil
// framer is an error (a raw link cannot become DDP without framing). Returns
// (nil, nil) when the section is disabled.
func NewFromOpener(m *config.Model, opener func() (link.FrameLink, error), framer link.Framer, rtr router.Router, logger log.Logger) (component.Component, error) {
	sec := port.SectionFromModel(m, Name)
	if !sec.IsEnabled {
		return nil, nil
	}
	if opener != nil && framer == nil {
		return nil, errors.New("ethertalk: frame opener supplied without a framer")
	}
	return newPort(sec, buildOpenerFactory(opener, framer), rtr, logger), nil
}

// newPort wires the runport base and stamps the rx-port owner identity. The
// rx-port handed to router.Inbound must be this outer *Port (the router uses it
// to avoid echoing a datagram back out the interface it arrived on); it exists
// only after runport.New, so SetOwner runs here.
func newPort(sec *port.Section, open runport.LinkFactory, rtr router.Router, logger log.Logger) *Port {
	p := &Port{Port: runport.New(sec, open, rtr, logger)}
	p.SetOwner(p)
	return p
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

// buildOpenerFactory returns a runport.LinkFactory that, on each Start, opens a
// FRESH FrameLink from opener and frames it — so a reopened device gets a new
// handle. A nil opener yields a nil-link factory (inert).
func buildOpenerFactory(opener func() (link.FrameLink, error), framer link.Framer) runport.LinkFactory {
	if opener == nil {
		return func() (link.DatagramLink, error) { return nil, nil }
	}
	return func() (link.DatagramLink, error) {
		frame, err := opener()
		if err != nil {
			return nil, err
		}
		return framer.Framing(frame)
	}
}
