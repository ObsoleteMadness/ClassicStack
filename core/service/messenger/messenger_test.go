package messenger

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	mswire "github.com/ObsoleteMadness/ClassicStack/core/protocol/mailslot"
	msframe "github.com/ObsoleteMadness/ClassicStack/core/protocol/messenger"
	nbproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
	"github.com/ObsoleteMadness/ClassicStack/core/service/mailslot"
	"github.com/ObsoleteMadness/ClassicStack/core/service/netbios"
)

// recordingPub captures the events the messenger publishes on the telemetry bus.
type recordingPub struct{ events []bus.Event }

func (p *recordingPub) Publish(e bus.Event) { p.events = append(p.events, e) }

// recordingSink captures outbound mailslot writes (the send path).
type recordingSink struct {
	name      string
	src, dest nbproto.Name
	body      []byte
	broadcast bool
	hits      int
}

func (r *recordingSink) SendMailslot(name string, src, dest nbproto.Name, body []byte, broadcast bool) error {
	r.name, r.src, r.dest, r.body, r.broadcast, r.hits = name, src, dest, body, broadcast, r.hits+1
	return nil
}

func (r *recordingSink) SendMailslotTo(name string, src, dest nbproto.Name, body []byte, broadcast bool, _ *netbios.DatagramEndpoint) error {
	return r.SendMailslot(name, src, dest, body, broadcast)
}

func clientName(s string) nbproto.Name {
	return nbproto.NewName(s, nbproto.NameTypeWorkstation)
}

// TestHandleMailslotPublishesAndLogs proves an inbound single-block net-send pop-up
// is decoded and published on the telemetry bus as a MessageReceived with From/To/
// Text preserved.
func TestHandleMailslotPublishesAndLogs(t *testing.T) {
	pub := &recordingPub{}
	svc := New(nil, pub, nil, "CLASSICSTACK", "WORKGROUP")

	body := msframe.Message{From: "ALICE", To: "CLASSICSTACK", Text: "ping"}.Marshal()
	svc.HandleMailslot(mswire.NameMessenger, clientName("ALICE"), clientName("CLASSICSTACK"), body, nil)

	if len(pub.events) != 1 {
		t.Fatalf("published %d events, want 1", len(pub.events))
	}
	ev, ok := pub.events[0].(bus.MessageReceived)
	if !ok {
		t.Fatalf("event is %T, want bus.MessageReceived", pub.events[0])
	}
	if ev.From != "ALICE" || ev.To != "CLASSICSTACK" || ev.Text != "ping" {
		t.Errorf("event = %+v, want from ALICE to CLASSICSTACK text ping", ev)
	}
	if ev.Topic() != bus.TopicMessage {
		t.Errorf("topic = %q, want %q", ev.Topic(), bus.TopicMessage)
	}
}

// TestHandleMailslotDropsNonMessenger proves a body that is not a single-block
// messenger datagram is dropped — no publish, no panic.
func TestHandleMailslotDropsNonMessenger(t *testing.T) {
	pub := &recordingPub{}
	svc := New(nil, pub, nil, "CLASSICSTACK", "")
	svc.HandleMailslot(mswire.NameMessenger, clientName("X"), clientName("CLASSICSTACK"), []byte{0xD0, 'a', 0}, nil)
	if len(pub.events) != 0 {
		t.Errorf("published %d events on a non-messenger datagram, want 0", len(pub.events))
	}
}

// TestHandleMailslotNilPublisher proves a receive-only deployment with no publisher
// still decodes without panicking (publishing is a no-op).
func TestHandleMailslotNilPublisher(t *testing.T) {
	svc := New(nil, nil, nil, "CLASSICSTACK", "")
	body := msframe.Message{From: "A", To: "CLASSICSTACK", Text: "hi"}.Marshal()
	svc.HandleMailslot(mswire.NameMessenger, clientName("A"), clientName("CLASSICSTACK"), body, nil)
}

// TestSendMessageWraps proves SendMessage writes a single-block messenger datagram
// to \MAILSLOT\MESSNGR, sourced from our identity, directed (not broadcast).
func TestSendMessageWraps(t *testing.T) {
	sink := &recordingSink{}
	svc := New(nil, nil, sink, "CLASSICSTACK", "")
	dest := nbproto.NewName("BOB", nbproto.NameTypeMessenger)

	if err := svc.SendMessage("BOB", dest, "hello there"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if sink.hits != 1 {
		t.Fatalf("sink hit %d times, want 1", sink.hits)
	}
	if sink.name != mswire.NameMessenger {
		t.Errorf("mailslot = %q, want %q", sink.name, mswire.NameMessenger)
	}
	if sink.broadcast {
		t.Error("net send was broadcast, want directed")
	}
	if sink.src.String() != "CLASSICSTACK" {
		t.Errorf("source = %q, want CLASSICSTACK", sink.src.String())
	}
	m, err := msframe.Unmarshal(sink.body)
	if err != nil {
		t.Fatalf("decode sent body: %v", err)
	}
	if m.From != "CLASSICSTACK" || m.To != "BOB" || m.Text != "hello there" {
		t.Errorf("sent message = %+v", *m)
	}
}

// TestSendMessageNoSink proves SendMessage is a safe no-op when no sink is wired
// (a receive-only deployment).
func TestSendMessageNoSink(t *testing.T) {
	svc := New(nil, nil, nil, "CLASSICSTACK", "")
	if err := svc.SendMessage("BOB", nbproto.NewName("BOB", nbproto.NameTypeMessenger), "x"); err != nil {
		t.Errorf("SendMessage with no sink returned %v, want nil", err)
	}
}

// TestRoutesThroughMailslotRouter proves the messenger plugs into the real mailslot
// router as a second consumer (alongside the browser) and receives net-send
// datagrams routed by mailslot name — the §3-quater multi-consumer guarantee.
func TestRoutesThroughMailslotRouter(t *testing.T) {
	pub := &recordingPub{}
	svc := New(nil, pub, nil, "CLASSICSTACK", "")

	router := mailslot.NewRouter(&nullSink{})
	router.Register(mswire.NameMessenger, svc)

	body := msframe.Message{From: "ALICE", To: "CLASSICSTACK", Text: "via router"}.Marshal()
	envelope := mswire.Write{Name: mswire.NameMessenger, Body: body}.Marshal()
	router.HandleDatagram(netbios.Datagram{Source: clientName("ALICE"), Payload: envelope})

	if len(pub.events) != 1 {
		t.Fatalf("router did not route to messenger: %d events", len(pub.events))
	}
	if ev := pub.events[0].(bus.MessageReceived); ev.Text != "via router" {
		t.Errorf("routed text = %q, want 'via router'", ev.Text)
	}
}

// nullSink is a no-op DatagramSink for constructing a router in tests.
type nullSink struct{}

func (nullSink) SendDatagram(netbios.Datagram) error { return nil }
