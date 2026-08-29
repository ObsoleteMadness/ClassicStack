package mailslot

import (
	"testing"

	wire "github.com/ObsoleteMadness/ClassicStack/core/protocol/mailslot"
	nbproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/netbios"
	"github.com/ObsoleteMadness/ClassicStack/core/service/netbios"
)

// recordingSink captures the NetBIOS datagrams the router sends.
type recordingSink struct{ sent []netbios.Datagram }

func (r *recordingSink) SendDatagram(d netbios.Datagram) error {
	r.sent = append(r.sent, d)
	return nil
}

// recordingConsumer captures the bodies routed to it.
type recordingConsumer struct {
	name    string
	src     nbproto.Name
	body    []byte
	hits    int
	replyTo *netbios.DatagramEndpoint
}

func (c *recordingConsumer) HandleMailslot(name string, src, dest nbproto.Name, body []byte, replyTo *netbios.DatagramEndpoint) {
	c.name, c.src, c.body, c.hits, c.replyTo = name, src, append([]byte(nil), body...), c.hits+1, replyTo
}

// TestRouterRoutesByName proves an inbound mailslot write is unwrapped and routed
// to the consumer registered for its mailslot name, carrying the bare body — and a
// write to an unregistered mailslot is dropped.
func TestRouterRoutesByName(t *testing.T) {
	r := NewRouter(&recordingSink{})
	browse := &recordingConsumer{}
	r.Register(wire.NameBrowse, browse)

	body := []byte{0x01, 0xAA, 0xBB}
	envelope := wire.Write{Name: wire.NameBrowse, Body: body}.Marshal()
	src := nbproto.NewName("CLIENT", nbproto.NameTypeWorkstation)
	r.HandleDatagram(netbios.Datagram{Source: src, Payload: envelope})

	if browse.hits != 1 {
		t.Fatalf("browse consumer hit %d times, want 1", browse.hits)
	}
	if string(browse.body) != string(body) {
		t.Errorf("routed body = % x, want % x", browse.body, body)
	}
	if browse.src.String() != "CLIENT" {
		t.Errorf("routed src = %q, want CLIENT", browse.src.String())
	}

	// A write to a mailslot no one registered is dropped (no panic, no hit).
	r.HandleDatagram(netbios.Datagram{Source: src, Payload: wire.Write{Name: wire.NameMessenger, Body: []byte{1}}.Marshal()})
	if browse.hits != 1 {
		t.Errorf("browse consumer hit on a foreign mailslot: %d", browse.hits)
	}
}

// TestRouterDropsNonMailslot proves a datagram that is not a mailslot write is
// dropped, not mis-routed.
func TestRouterDropsNonMailslot(t *testing.T) {
	c := &recordingConsumer{}
	r := NewRouter(&recordingSink{})
	r.Register(wire.NameBrowse, c)
	r.HandleDatagram(netbios.Datagram{Payload: []byte("not an smb mailslot write at all")})
	if c.hits != 0 {
		t.Errorf("consumer hit on a non-mailslot datagram: %d", c.hits)
	}
}

// TestSendMailslotWrapsAndSends proves SendMailslot wraps the body in the envelope
// and hands a netbios.Datagram (names + broadcast flag preserved) to the sink.
func TestSendMailslotWrapsAndSends(t *testing.T) {
	sink := &recordingSink{}
	r := NewRouter(sink)
	src := nbproto.NewName("CLASSICSTACK", nbproto.NameTypeWorkstation)
	dest := nbproto.NewName("WORKGROUP", nbproto.NameTypeGroup)
	body := []byte("announce-me")

	if err := r.SendMailslot(wire.NameBrowse, src, dest, body, true); err != nil {
		t.Fatalf("SendMailslot: %v", err)
	}
	if len(sink.sent) != 1 {
		t.Fatalf("sent %d datagrams, want 1", len(sink.sent))
	}
	d := sink.sent[0]
	if d.Source != src || d.Destination != dest || !d.Broadcast {
		t.Errorf("datagram names/flag wrong: src=%q dst=%q bcast=%v", d.Source.String(), d.Destination.String(), d.Broadcast)
	}
	w, err := wire.Unmarshal(d.Payload)
	if err != nil {
		t.Fatalf("Unmarshal sent payload: %v", err)
	}
	if w.Name != wire.NameBrowse || string(w.Body) != string(body) {
		t.Errorf("wrapped write = %q/%q, want %q/%q", w.Name, w.Body, wire.NameBrowse, body)
	}
}

// TestRegisterCaseInsensitive proves mailslot-name matching folds case (Windows
// mailslot names are case-insensitive).
func TestRegisterCaseInsensitive(t *testing.T) {
	c := &recordingConsumer{}
	r := NewRouter(&recordingSink{})
	r.Register("\\mailslot\\browse", c) // lower-case registration
	r.HandleDatagram(netbios.Datagram{Payload: wire.Write{Name: wire.NameBrowse, Body: []byte{1}}.Marshal()})
	if c.hits != 1 {
		t.Errorf("case-insensitive match failed: hits=%d", c.hits)
	}
}
