package sap

import (
	"testing"

	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
	ncpproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ncp"
)

// recordingSender captures datagrams the advertiser sends.
type recordingSender struct{ sent []*ipxproto.Datagram }

func (s *recordingSender) Send(d *ipxproto.Datagram) error { s.sent = append(s.sent, d); return nil }

var (
	testNet  = [4]byte{0, 0, 0, 42}
	testNode = [6]byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}
)

func newAdv() (*Advertiser, *recordingSender) {
	s := &recordingSender{}
	a := New(s)
	a.SetIdentity(testNet, testNode)
	return a, s
}

// TestSAP_RegisterFillsIdentity proves a registered entry that left Network/Node zero
// is broadcast with the advertiser's shared IPX identity filled in.
func TestSAP_RegisterFillsIdentity(t *testing.T) {
	a, s := newAdv()
	a.Register(ncpproto.SAPEntry{Type: ncpproto.SAPServerTypeNetBIOS, Name: "CLASSICSTACK", Socket: [2]byte{0x04, 0x55}, Hops: 1})
	a.broadcast()

	if len(s.sent) != 1 {
		t.Fatalf("broadcast sent %d datagrams, want 1", len(s.sent))
	}
	dg := s.sent[0]
	if dg.DstNode != ipxBroadcastNode || dg.DstSock != ncpproto.SAPSocket {
		t.Errorf("broadcast dst = %x:%v, want broadcast on SAP socket", dg.DstNode, dg.DstSock)
	}
	entries := decodeEntries(t, dg.Payload)
	if len(entries) != 1 {
		t.Fatalf("broadcast carried %d entries, want 1", len(entries))
	}
	e := entries[0]
	if e.Type != ncpproto.SAPServerTypeNetBIOS || e.Name != "CLASSICSTACK" {
		t.Errorf("entry type/name = %#x/%q", e.Type, e.Name)
	}
	if e.Network != testNet || e.Node != testNode {
		t.Errorf("entry net/node = %x/%x, want identity %x/%x", e.Network, e.Node, testNet, testNode)
	}
}

// TestSAP_MultipleServicesAdvertised proves NCP and NB-IPX entries registered through
// the one shared advertiser are both broadcast — the whole point of the shared
// registrar (one 0x0452 handler, many services).
func TestSAP_MultipleServicesAdvertised(t *testing.T) {
	a, s := newAdv()
	a.Register(ncpproto.SAPEntry{Type: ncpproto.SAPServerTypeFileServer, Name: "NWSERVER", Socket: ncpproto.NCPSocket, Hops: 1})
	a.Register(ncpproto.SAPEntry{Type: ncpproto.SAPServerTypeNetBIOS, Name: "CLASSICSTACK", Socket: [2]byte{0x04, 0x55}, Hops: 1})
	a.broadcast()

	entries := decodeEntries(t, s.sent[0].Payload)
	if len(entries) != 2 {
		t.Fatalf("broadcast carried %d entries, want 2", len(entries))
	}
	types := map[uint16]bool{}
	for _, e := range entries {
		types[e.Type] = true
	}
	if !types[ncpproto.SAPServerTypeFileServer] || !types[ncpproto.SAPServerTypeNetBIOS] {
		t.Errorf("advertised types = %v, want both FileServer and NetBIOS", types)
	}
}

// TestSAP_AnswersGeneralQueryByType proves a general query for the NetBIOS type is
// answered with only the matching entry, unicast back to the querier.
func TestSAP_AnswersGeneralQueryByType(t *testing.T) {
	a, s := newAdv()
	a.Register(ncpproto.SAPEntry{Type: ncpproto.SAPServerTypeFileServer, Name: "NWSERVER", Socket: ncpproto.NCPSocket, Hops: 1})
	a.Register(ncpproto.SAPEntry{Type: ncpproto.SAPServerTypeNetBIOS, Name: "CLASSICSTACK", Socket: [2]byte{0x04, 0x55}, Hops: 1})

	query := []byte{0x00, 0x01, byte(uint16(ncpproto.SAPServerTypeNetBIOS) >> 8), byte(uint16(ncpproto.SAPServerTypeNetBIOS) & 0xFF)}
	a.HandleDatagram(&ipxproto.Datagram{
		Payload: query,
		SrcNet:  [4]byte{0, 0, 0, 9},
		SrcNode: [6]byte{0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f},
		SrcSock: [2]byte{0x40, 0x00},
	})
	if len(s.sent) != 1 {
		t.Fatalf("query answered with %d datagrams, want 1", len(s.sent))
	}
	reply := s.sent[0]
	if reply.DstNode != ([6]byte{0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f}) {
		t.Errorf("reply not addressed to the querier: %x", reply.DstNode)
	}
	entries := decodeEntries(t, reply.Payload)
	if len(entries) != 1 || entries[0].Type != ncpproto.SAPServerTypeNetBIOS {
		t.Fatalf("query answer entries = %+v, want only the NetBIOS entry", entries)
	}
}

// TestSAP_WildcardQueryReturnsAll proves a wildcard GENERAL query returns every
// registered entry. (A nearest query returns exactly one — see below.)
func TestSAP_WildcardQueryReturnsAll(t *testing.T) {
	a, s := newAdv()
	a.Register(ncpproto.SAPEntry{Type: ncpproto.SAPServerTypeFileServer, Name: "NWSERVER", Socket: ncpproto.NCPSocket})
	a.Register(ncpproto.SAPEntry{Type: ncpproto.SAPServerTypeNetBIOS, Name: "CLASSICSTACK", Socket: [2]byte{0x04, 0x55}})

	query := []byte{0x00, 0x01, 0xFF, 0xFF} // general query, wildcard type
	a.HandleDatagram(&ipxproto.Datagram{Payload: query})
	if len(s.sent) != 1 {
		t.Fatalf("wildcard query answered with %d datagrams, want 1", len(s.sent))
	}
	if entries := decodeEntries(t, s.sent[0].Payload); len(entries) != 2 {
		t.Fatalf("wildcard answer carried %d entries, want 2", len(entries))
	}
}

// TestSAP_NearestResponseSingleEntry proves a nearest query is answered with exactly
// ONE entry even when several match — the client attaches to it (mars_nwe
// send_server_response picks a single best server; a real NetWare 4 server answers
// GetNearestServer with one entry too).
func TestSAP_NearestResponseSingleEntry(t *testing.T) {
	a, s := newAdv()
	a.Register(ncpproto.SAPEntry{Type: ncpproto.SAPServerTypeFileServer, Name: "NWSERVER", Socket: ncpproto.NCPSocket})
	a.Register(ncpproto.SAPEntry{Type: ncpproto.SAPServerTypeNetBIOS, Name: "CLASSICSTACK", Socket: [2]byte{0x04, 0x55}})

	query := []byte{0x00, 0x03, 0xFF, 0xFF} // nearest query, wildcard type
	a.HandleDatagram(&ipxproto.Datagram{Payload: query})
	if len(s.sent) != 1 {
		t.Fatalf("nearest query answered with %d datagrams, want 1", len(s.sent))
	}
	if entries := decodeEntries(t, s.sent[0].Payload); len(entries) != 1 {
		t.Fatalf("nearest answer carried %d entries, want exactly 1", len(entries))
	}
}

// TestSAP_WithdrawnEntryNotAdvertised proves the cancel returned by Register removes
// the entry from later broadcasts.
func TestSAP_WithdrawnEntryNotAdvertised(t *testing.T) {
	a, s := newAdv()
	cancel := a.Register(ncpproto.SAPEntry{Type: ncpproto.SAPServerTypeNetBIOS, Name: "X", Socket: [2]byte{0x04, 0x55}})
	cancel()
	a.broadcast()
	if len(s.sent) != 0 {
		t.Fatalf("withdrawn-only advertiser broadcast %d datagrams, want 0", len(s.sent))
	}
}

// decodeEntries parses the SAP entries out of a response payload (operation + 64-byte
// entries), for assertions.
func decodeEntries(t *testing.T, payload []byte) []ncpproto.SAPEntry {
	t.Helper()
	if len(payload) < 2 {
		t.Fatalf("payload too short: %d bytes", len(payload))
	}
	body := payload[2:]
	var out []ncpproto.SAPEntry
	for len(body) >= ncpproto.SAPEntryLen {
		rec := body[:ncpproto.SAPEntryLen]
		e := ncpproto.SAPEntry{
			Type: uint16(rec[0])<<8 | uint16(rec[1]),
			Name: trimNUL(rec[2:50]),
		}
		copy(e.Network[:], rec[50:54])
		copy(e.Node[:], rec[54:60])
		copy(e.Socket[:], rec[60:62])
		e.Hops = uint16(rec[62])<<8 | uint16(rec[63])
		out = append(out, e)
		body = body[ncpproto.SAPEntryLen:]
	}
	return out
}

func trimNUL(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}
