// Package etherdfs is the EtherDFS port: EtherDFS request/reply frames over raw
// Ethernet with the custom EtherType 0xEDF5. Like the IPX and NetBEUI ports it
// does not ride the DDP router — EtherDFS is a single-frame request/response
// protocol with its own dispatch (the EtherDFS file service) — so this port
// exchanges raw frames via the frameport base and demuxes the EtherType here.
//
// It is the thin wire half of the EtherDFS server: it owns the NIC link
// (open/read/restart/dedup/metering via frameport) and hands each inbound
// EtherDFS frame to an installed Handler, transmitting the Handler's reply back
// out the same link. The Handler (the file service) holds no link knowledge; the
// port holds no filesystem knowledge.
package etherdfs

import (
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
	"github.com/ObsoleteMadness/ClassicStack/core/port/internal/frameport"
	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/etherdfs"
)

// Name is the component/section key for the EtherDFS port (also the service name;
// EtherDFS is a single component whose port half lives here).
const Name = "EtherDFS"

// BPFFilter is the kernel capture filter for the EtherDFS port: Ethernet II frames with
// the custom EtherDFS EtherType 0xEDF5. Applied at the pcap handle so the read loop is
// not fed the AppleTalk/IPv4/etc. background a promiscuous handle would otherwise
// surface; the onFrame path re-validates the EtherType regardless. Each NIC transport
// owns its filter; this is EtherDFS's.
const BPFFilter = "ether proto 0xedf5"

// Handler processes one decoded inbound EtherDFS request frame and returns the
// reply payload (the per-opcode body) to send back, or nil to send nothing. It
// runs on the read goroutine: decode-and-respond, do not block. srcMAC is the
// station address the port stamps as the reply's source.
type Handler func(req proto.Frame) (reply []byte)

// Port is the EtherDFS port. It embeds the frameport base and adds the EtherType
// 0xEDF5 demux, the own-MAC/broadcast acceptance filter, and the request handler.
type Port struct {
	*frameport.Port

	srcMAC  [6]byte
	mu      sync.Mutex
	handler Handler
}

// NewInstanceFromOpener builds an EtherDFS port whose link is opened by a
// per-Start factory (§M11.c device-link injection): the compose factory injects
// the NIC opener resolved from the section's interface, so the port opens a FRESH
// link on every Start and survives a UI Stop→Start (a closed pcap handle is
// terminal). A nil opener yields the inert-but-configured form. srcMAC is this
// station's hardware address, stamped as the reply source and matched against
// inbound destinations. Returns (nil, nil) when the section is disabled.
func NewInstanceFromOpener(sec *port.Section, open func() (link.FrameLink, error), srcMAC [6]byte, logger log.Logger) (*Port, error) {
	if !sec.IsEnabled {
		return nil, nil
	}
	if open == nil {
		open = func() (link.FrameLink, error) { return nil, nil }
	}
	p := &Port{srcMAC: srcMAC}
	p.Port = frameport.New(sec, open, p.onFrame, logger)
	return p, nil
}

// SetHandler installs the request handler. May be called before or after Start.
func (p *Port) SetHandler(h Handler) {
	p.mu.Lock()
	p.handler = h
	p.mu.Unlock()
}

// SrcMAC returns the station address the port stamps on replies (and matches
// inbound frames against), for the service to report/diagnose.
func (p *Port) SrcMAC() [6]byte { return p.srcMAC }

// onFrame is the frameport FrameSink: filter EtherDFS frames addressed to us (or
// broadcast), decode, dispatch to the handler, and send the reply.
func (p *Port) onFrame(frame link.Frame) {
	if len(frame) < proto.MinFrameLen {
		return
	}
	if !p.addressedToUs(frame) {
		return
	}
	req, err := proto.ParseFrame(frame)
	if err != nil {
		p.CountDecodeError()
		return
	}
	p.mu.Lock()
	h := p.handler
	p.mu.Unlock()
	if h == nil {
		return
	}
	reply := h(req)
	if reply == nil {
		return
	}
	out := req.Reply(p.srcMAC, reply).Encode(nil)
	_ = p.Send(out)
}

// addressedToUs reports whether an inbound frame's destination MAC is our station
// address or the Ethernet broadcast (AL_INSTALLCHK is broadcast). A zero srcMAC
// (interface MAC unresolved) matches anything, so a station that could not learn
// its own MAC still answers.
func (p *Port) addressedToUs(frame link.Frame) bool {
	var dst [6]byte
	copy(dst[:], frame[0:6])
	if dst == broadcastMAC {
		return true
	}
	if p.srcMAC == ([6]byte{}) {
		return true
	}
	return dst == p.srcMAC
}

// broadcastMAC is the Ethernet broadcast address (AL_INSTALLCHK target).
var broadcastMAC = [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

// compile-time assertion: *Port is a component (via the embedded frameport.Port).
var _ component.Component = (*Port)(nil)
