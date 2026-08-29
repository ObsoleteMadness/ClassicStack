// Package messenger is the NetBIOS Messenger Service (datagram-layer, §3-quater):
// the "net send" / WinPopup receiver. It is the SECOND mailslot consumer (after the
// browser), proving the §3-quater seam is multi-consumer — it registers for
// \MAILSLOT\MESSNGR on the mailslot router and exchanges bare messenger frames,
// holding NO mailslot-envelope code and NO transport code (the router wraps the
// SMB_COM_TRANSACTION envelope; core/service/netbios does the per-transport wire
// framing; core/protocol/messenger is the frame codec).
//
// Receive path only for now: an inbound pop-up is logged and published on the
// telemetry bus (bus.TopicMessage) so a UI can display net-send events. The send
// path (a \MAILSLOT\MESSNGR write for an outgoing "net send") is a thin future
// addition over the same MailslotSink — see SendMessage.
//
// Ring: CORE (stdlib only, reflection-free).
package messenger

import (
	"context"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	mswire "github.com/ObsoleteMadness/ClassicStack/core/protocol/mailslot"
	msframe "github.com/ObsoleteMadness/ClassicStack/core/protocol/messenger"
	nbproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
	"github.com/ObsoleteMadness/ClassicStack/core/service/mailslot"
	nbservice "github.com/ObsoleteMadness/ClassicStack/core/service/netbios"
)

// Name is the component name for the messenger service.
const Name = "Messenger"

// MailslotSink is the outbound seam: write a body to a named mailslot, sourced from
// src to dest. The mailslot router's SendMailslot satisfies it structurally — the
// messenger holds NO envelope/transport code (mirrors the browser's seam).
type MailslotSink interface {
	SendMailslot(name string, src, dest nbproto.Name, body []byte, broadcast bool) error
}

// Publisher is the telemetry-bus seam: the messenger publishes a received pop-up so
// a UI can display it. bus.Bus satisfies it; a nil Publisher disables publishing.
// Kept narrow (Publish only) so the service depends on the seam, not the whole bus.
type Publisher interface {
	Publish(bus.Event)
}

// Service is the messenger command core. It receives net-send pop-ups on
// \MAILSLOT\MESSNGR, logs them, and publishes a bus.MessageReceived event. It is a
// mailslot.Consumer; compose registers it on the mailslot router. server is our
// identity (the recipient name net send targets); workgroup is informational.
type Service struct {
	logger    log.Logger
	pub       Publisher
	sink      MailslotSink
	server    string
	workgroup string

	now func() time.Time

	running bool
}

// New builds a messenger service for the given server identity, logging through
// logger and publishing received pop-ups through pub (nil disables publishing).
// sink is the outbound mailslot seam (used only by the future send path; may be
// nil for a receive-only deployment). An empty server defaults to CLASSICSTACK.
func New(logger log.Logger, pub Publisher, sink MailslotSink, server, workgroup string) *Service {
	if server == "" {
		server = "CLASSICSTACK"
	}
	if workgroup == "" {
		workgroup = "WORKGROUP"
	}
	return &Service{
		logger:    logger,
		pub:       pub,
		sink:      sink,
		server:    server,
		workgroup: workgroup,
		now:       time.Now,
	}
}

// Name returns the component name.
func (s *Service) Name() string { return Name }

// SetSink installs the outbound mailslot seam late, for compose: the messenger
// factory builds the service before the mailslot router exists, so the cross-wire
// injects the sink afterwards (mirroring the browser's SetSink). Used only by the
// send path (SendMessage / a future cmd/csnetsend); the receive path needs no sink.
// Set before Start.
func (s *Service) SetSink(sink MailslotSink) { s.sink = sink }

// SetIdentity restamps the recipient name and workgroup from config.Identity.
func (s *Service) SetIdentity(server, workgroup string) {
	if server == "" {
		server = "CLASSICSTACK"
	}
	if workgroup == "" {
		workgroup = "WORKGROUP"
	}
	s.server = server
	s.workgroup = workgroup
}

// Start brings the messenger up. There is no background loop — it is purely
// reactive to inbound mailslot writes — so Start just marks it running. Idempotent.
func (s *Service) Start(ctx context.Context) error {
	_ = ctx
	s.running = true
	s.logf("messenger started")
	return nil
}

// Stop brings the messenger down. Idempotent.
func (s *Service) Stop(ctx context.Context) error {
	_ = ctx
	s.running = false
	s.logf("messenger stopped")
	return nil
}

// HandleMailslot implements mailslot.Consumer: one messenger frame body delivered on
// \MAILSLOT\MESSNGR (the mailslot layer has already unwrapped the envelope). It
// decodes the single-block pop-up, logs it, and publishes a bus.MessageReceived for
// the UI. The source/destination NetBIOS names are the datagram envelope's; the
// authoritative From/To are inside the messenger frame.
func (s *Service) HandleMailslot(name string, src, dest nbproto.Name, body []byte, replyTo *nbservice.DatagramEndpoint) {
	_ = name
	_ = src
	_ = dest
	_ = replyTo // messenger pop-ups are one-way; no directed reply
	m, err := msframe.Unmarshal(body)
	if err != nil {
		return // not a single-block messenger datagram — drop quietly
	}
	s.logMessage(m)
	s.publish(m)
}

// SendMessage sends an outgoing "net send" pop-up to dest over the mailslot seam:
// a single-block messenger datagram from our identity. Returns nil with no effect
// when no sink is configured. This is the send half exercised by a future
// cmd/csnetsend client (§12); receive is the live path today.
func (s *Service) SendMessage(to string, dest nbproto.Name, text string) error {
	if s.sink == nil {
		return nil
	}
	body := msframe.Message{From: s.server, To: to, Text: text}.Marshal()
	return s.sink.SendMailslot(
		mswire.NameMessenger,
		nbproto.NewName(s.server, nbproto.NameTypeMessenger),
		dest,
		body,
		false,
	)
}

// logMessage emits the received pop-up at Info with typed fields.
func (s *Service) logMessage(m *msframe.Message) {
	if s.logger == nil || !s.logger.Enabled(log.Info) {
		return
	}
	s.logger.Log(log.Info, "net send received",
		log.Str("scope", Name),
		log.Str("from", m.From),
		log.Str("to", m.To),
		log.Str("text", m.Text),
	)
}

// publish puts the pop-up on the telemetry bus for the UI (no-op if no publisher).
func (s *Service) publish(m *msframe.Message) {
	if s.pub == nil {
		return
	}
	s.pub.Publish(bus.MessageReceived{
		Kind: bus.MessageKindMessenger,
		From: m.From,
		To:   m.To,
		Text: m.Text,
		Time: s.now(),
	})
}

// logf emits one info line through the logger if configured.
func (s *Service) logf(msg string) {
	if s.logger == nil || !s.logger.Enabled(log.Info) {
		return
	}
	s.logger.Log1(log.Info, msg, log.Str("scope", Name))
}

// compile-time assertions: the service is a Component and a mailslot Consumer (it
// registers for \MAILSLOT\MESSNGR on the mailslot router).
var (
	_ component.Component = (*Service)(nil)
	_ mailslot.Consumer   = (*Service)(nil)
)
