// Package mailslot is the NetBIOS mailslot dispatch layer (§3-quater): the shared
// seam between the mailslot consumers (browser, messenger, …) and the NetBIOS
// connectionless datagram path. It plugs into the NetBIOS service as the
// DatagramConsumer (the inbound seam) and uses SendDatagram (the outbound seam);
// it unwraps the \MAILSLOT\* SMB_COM_TRANSACTION envelope from an inbound datagram
// and routes the INNER body to the Consumer registered for that mailslot name, and
// on Send wraps a consumer's body back into the envelope and hands it to NetBIOS.
//
// Consumers (e.g. core/service/browser) hold NO mailslot-envelope code and NO
// transport code: they register a name and exchange bare bodies. The per-NetBIOS-
// transport wire framing (NBF UI-frame / NBIPX NMPI-MailslotSend / NBT UDP-138)
// lives in core/service/netbios; the envelope codec is core/protocol/mailslot.
// This package is the routing in between — one concern, reaching around nothing.
//
// Ring: CORE (stdlib only, reflection-free).
package mailslot

import (
	"sync"

	wire "github.com/ObsoleteMadness/ClassicStack/core/protocol/mailslot"
	nbproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
	"github.com/ObsoleteMadness/ClassicStack/core/service/netbios"
)

// Consumer receives the body written to the mailslot it registered for, with the
// source and destination NetBIOS names and the transport reply endpoint (replyTo,
// nil for a broadcast the consumer only observes). A browser registers for
// \MAILSLOT\BROWSE, a messenger for \MAILSLOT\MESSNGR; neither sees the
// SMB_COM_TRANSACTION envelope. A consumer that answers a specific requester echoes
// replyTo back to SendMailslotTo; it treats it as opaque (the §3 transport-agnostic
// contract).
type Consumer interface {
	HandleMailslot(name string, src, dest nbproto.Name, body []byte, replyTo *netbios.DatagramEndpoint)
}

// DatagramSink is the NetBIOS outbound seam the router sends through. The NetBIOS
// service's SendDatagram satisfies it structurally, so this package depends on the
// small seam rather than reaching into the service for sending.
type DatagramSink interface {
	SendDatagram(d netbios.Datagram) error
}

// Router is the mailslot dispatch layer. It is installed on the NetBIOS service as
// its DatagramConsumer (HandleDatagram) and routes inbound mailslot writes to the
// per-name registered Consumer; consumers send through SendMailslot. Safe for
// concurrent use.
type Router struct {
	sink DatagramSink

	mu        sync.RWMutex
	consumers map[string]Consumer
}

// NewRouter builds a mailslot router that sends through sink (the NetBIOS service).
func NewRouter(sink DatagramSink) *Router {
	return &Router{sink: sink, consumers: make(map[string]Consumer)}
}

// Register binds a Consumer to a mailslot name (e.g. mailslotwire.NameBrowse). The
// name match is exact and case-insensitive on the wire side (Windows mailslot names
// are case-insensitive), but callers should register the canonical upper-case form.
// A second Register for the same name replaces the prior consumer.
func (r *Router) Register(name string, c Consumer) {
	r.mu.Lock()
	r.consumers[upper(name)] = c
	r.mu.Unlock()
}

// Unregister removes a mailslot binding. Idempotent.
func (r *Router) Unregister(name string) {
	r.mu.Lock()
	delete(r.consumers, upper(name))
	r.mu.Unlock()
}

// HandleDatagram implements netbios.DatagramConsumer: unwrap the \MAILSLOT\*
// envelope and route the body to the registered consumer. A datagram that is not a
// mailslot write, or names a mailslot no consumer registered, is dropped after
// decode (the lazy, optional behaviour §3-quater calls for).
func (r *Router) HandleDatagram(d netbios.Datagram) {
	w, err := wire.Unmarshal(d.Payload)
	if err != nil {
		return
	}
	r.mu.RLock()
	c := r.consumers[upper(w.Name)]
	r.mu.RUnlock()
	if c == nil {
		return
	}
	c.HandleMailslot(w.Name, d.Source, d.Destination, w.Body, d.ReplyTo)
}

// SendMailslot wraps body in the \MAILSLOT\* envelope for the named mailslot and
// sends it through NetBIOS to dest, sourced from src. broadcast marks a group
// datagram (the announcement case); a directed reply sets broadcast false. The
// transports do the per-protocol wire framing.
func (r *Router) SendMailslot(name string, src, dest nbproto.Name, body []byte, broadcast bool) error {
	return r.SendMailslotTo(name, src, dest, body, broadcast, nil)
}

// SendMailslotTo is SendMailslot with an explicit transport reply endpoint: when
// replyTo is non-nil the datagram is sent *directed* to that node by the one
// transport it names (a browser answering a specific GetBackupList / Announcement-
// Request requester); when nil it is a normal broadcast/named send fanned to every
// transport. replyTo is the token the consumer received on HandleMailslot.
func (r *Router) SendMailslotTo(name string, src, dest nbproto.Name, body []byte, broadcast bool, replyTo *netbios.DatagramEndpoint) error {
	payload := wire.Write{Name: name, Body: body}.Marshal()
	return r.sink.SendDatagram(netbios.Datagram{
		Source:      src,
		Destination: dest,
		Payload:     payload,
		Broadcast:   broadcast,
		ReplyTo:     replyTo,
	})
}

// upper folds a mailslot name to upper case for case-insensitive matching.
func upper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

// compile-time assertion: the Router is a NetBIOS DatagramConsumer.
var _ netbios.DatagramConsumer = (*Router)(nil)
