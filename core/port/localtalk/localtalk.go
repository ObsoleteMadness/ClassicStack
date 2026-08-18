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
// adapter (adapter/link/framing.LocalTalk over the pure core/protocol/llap engine);
// the claim goroutine calls SetAddress via OnClaimed to record the claim.
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

// Component/section keys for the LocalTalk ports. LToUDP and TashTalk are
// DISTINCT AppleTalk segments — each its own network number, zone, node space,
// and node-claim — reached over different transports (UDP multicast vs serial),
// NOT two ways onto one segment. So they are two ports with two keys, and a
// router can bridge both at once. Both are served by this one transport-agnostic
// package (LLAP framing + runport); the transport differs only in the FrameLink
// the compose factory injects.
const (
	NameLToUDP   = "LToUDP"   // LocalTalk-over-UDP-multicast segment
	NameTashTalk = "TashTalk" // physical LocalTalk segment via TashTalk serial

	// Name is retained for callers/tests that predate the segment split; it names
	// the LToUDP segment (the historical default LocalTalk transport).
	Name = NameLToUDP
)

// Port is the real LocalTalk port. It embeds the runport base (lifecycle, read
// loop, metering, RoutedPort data half) and adds the LocalTalk framing.
type Port struct {
	*runport.Port
}

// New builds the real LocalTalk port for the default (LToUDP) segment key. See
// NewNamed for the key-parameterised form.
func New(m *config.Model, frame link.FrameLink, framer link.Framer, rtr router.Router, logger log.Logger) (component.Component, error) {
	return NewNamed(Name, m, frame, framer, rtr, logger)
}

// NewNamed builds the real LocalTalk port for segment key (NameLToUDP or
// NameTashTalk). frame is the LocalTalk FrameLink (nil → inert until compose
// injects a transport link). framer turns that into a DDP DatagramLink via LLAP
// (nil with a non-nil frame is an error). rtr is the router the port delivers
// inbound datagrams to (nil → drop until M4). Returns (nil, nil) when the
// section is disabled.
func NewNamed(key string, m *config.Model, frame link.FrameLink, framer link.Framer, rtr router.Router, logger log.Logger) (component.Component, error) {
	return NewInstance(port.SectionFromModel(m, key), frame, framer, rtr, logger)
}

// NewInstance builds a LocalTalk port from an already-resolved section — the
// repeated-INSTANCE form (§M11): the compose factory resolves one instance from
// Model.Lists (under either segment key) and hands it here, so the port names
// itself from the instance's InstanceName(). nil frame → inert; a non-nil frame
// with a nil framer is an error; a disabled section yields (nil, nil).
func NewInstance(sec *port.Section, frame link.FrameLink, framer link.Framer, rtr router.Router, logger log.Logger) (component.Component, error) {
	if !sec.IsEnabled {
		return nil, nil
	}
	if frame != nil && framer == nil {
		return nil, errors.New("localtalk: frame link supplied without a framer")
	}
	return newPort(sec, buildLinkFactory(frame, framer), rtr, logger), nil
}

// NewFromOpener builds the LocalTalk port for the default (LToUDP) segment key
// from a per-Start FrameLink opener. See NewFromOpenerNamed for the
// key-parameterised form.
func NewFromOpener(m *config.Model, opener func() (link.FrameLink, error), framer link.Framer, rtr router.Router, logger log.Logger) (component.Component, error) {
	return NewFromOpenerNamed(Name, m, opener, framer, rtr, logger)
}

// NewFromOpenerNamed builds the LocalTalk port for segment key from a per-Start
// FrameLink opener rather than a single pre-opened FrameLink. opener is called
// on every Start to obtain a FRESH LocalTalk link, which framer then wraps as a
// DDP DatagramLink via LLAP — so the port survives a Stop→Start by reopening the
// transport. It is the constructor the composition layer uses once it can build
// a real device link from config; core stays free of the transport adapter
// because opener is injected.
//
// A nil opener yields the inert form; a non-nil opener with a nil framer is an
// error. Returns (nil, nil) when the section is disabled.
func NewFromOpenerNamed(key string, m *config.Model, opener func() (link.FrameLink, error), framer link.Framer, rtr router.Router, logger log.Logger) (component.Component, error) {
	return NewInstanceFromOpener(port.SectionFromModel(m, key), opener, framer, rtr, logger)
}

// NewInstanceFromOpener is the repeated-INSTANCE form of NewFromOpenerNamed (§M11):
// it takes an already-resolved section and the per-Start opener. A nil opener yields
// the inert form; a non-nil opener with a nil framer is an error; a disabled section
// yields (nil, nil).
func NewInstanceFromOpener(sec *port.Section, opener func() (link.FrameLink, error), framer link.Framer, rtr router.Router, logger log.Logger) (component.Component, error) {
	if !sec.IsEnabled {
		return nil, nil
	}
	if opener != nil && framer == nil {
		return nil, errors.New("localtalk: frame opener supplied without a framer")
	}
	return newPort(sec, buildOpenerFactory(opener, framer), rtr, logger), nil
}

// newPort wires the runport base and stamps the rx-port owner identity (see the
// ethertalk equivalent).
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
// FRESH FrameLink from opener and frames it. A nil opener yields a nil-link
// factory (inert).
func buildOpenerFactory(opener func() (link.FrameLink, error), framer link.Framer) runport.LinkFactory {
	if opener == nil {
		return func() (link.DatagramLink, error) { return nil, nil }
	}
	return func() (link.DatagramLink, error) {
		frame, err := opener()
		if err != nil {
			return nil, err
		}
		// A nil FrameLink is the no-pcap / inert contract. Framing rejects nil;
		// treat it as a successful no-data-path start, matching runport.Start.
		if frame == nil {
			return nil, nil
		}
		return framer.Framing(frame)
	}
}
