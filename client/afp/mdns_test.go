package afp

import (
	"strings"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

func TestPackAFPMDNSQuery(t *testing.T) {
	q, err := packAFPMDNSQuery()
	if err != nil {
		t.Fatal(err)
	}
	var p dnsmessage.Parser
	if _, err := p.Start(q); err != nil {
		t.Fatal(err)
	}
	qs, err := p.AllQuestions()
	if err != nil {
		t.Fatal(err)
	}
	if len(qs) != 1 {
		t.Fatalf("questions = %d, want 1", len(qs))
	}
	if dnsName(qs[0].Name) != "_afpovertcp._tcp.local" {
		t.Errorf("name = %q", qs[0].Name)
	}
	if qs[0].Type != dnsmessage.TypePTR {
		t.Errorf("type = %v, want PTR", qs[0].Type)
	}
	if uint16(qs[0].Class)&classQU == 0 {
		t.Errorf("class %v missing QU bit", qs[0].Class)
	}
}

func TestParseAFPMDNS_PTRSRVAndA(t *testing.T) {
	svc := dnsmessage.MustNewName(afpOverTCPSvc)
	inst := dnsmessage.MustNewName("ClassicStack._afpovertcp._tcp.local.")
	host := dnsmessage.MustNewName("imac.local.")
	msg := dnsmessage.Message{
		Header: dnsmessage.Header{Response: true, Authoritative: true},
		Answers: []dnsmessage.Resource{{
			Header: dnsmessage.ResourceHeader{Name: svc, Type: dnsmessage.TypePTR, Class: dnsmessage.ClassINET},
			Body:   &dnsmessage.PTRResource{PTR: inst},
		}},
		Additionals: []dnsmessage.Resource{
			{
				Header: dnsmessage.ResourceHeader{Name: inst, Type: dnsmessage.TypeSRV, Class: dnsmessage.ClassINET},
				Body:   &dnsmessage.SRVResource{Port: 548, Target: host},
			},
			{
				Header: dnsmessage.ResourceHeader{Name: host, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET},
				Body:   &dnsmessage.AResource{A: [4]byte{192, 168, 1, 9}},
			},
		},
	}
	packed, err := msg.Pack()
	if err != nil {
		t.Fatal(err)
	}
	acc := newMDNSAccum()
	acc.add(packed)
	got := acc.servers()
	if len(got) != 1 {
		t.Fatalf("servers = %+v, want 1", got)
	}
	s := got[0]
	if s.Name != "ClassicStack" {
		t.Errorf("Name = %q, want ClassicStack", s.Name)
	}
	if s.Host != "192.168.1.9" {
		t.Errorf("Host = %q, want 192.168.1.9", s.Host)
	}
	if s.Port != 548 {
		t.Errorf("Port = %d, want 548", s.Port)
	}
}

func TestParseAFPMDNS_SRVWithoutAUsesHostname(t *testing.T) {
	inst := dnsmessage.MustNewName("Files._afpovertcp._tcp.local.")
	host := dnsmessage.MustNewName("files.local.")
	msg := dnsmessage.Message{
		Header: dnsmessage.Header{Response: true},
		Answers: []dnsmessage.Resource{{
			Header: dnsmessage.ResourceHeader{Name: inst, Type: dnsmessage.TypeSRV, Class: dnsmessage.ClassINET},
			Body:   &dnsmessage.SRVResource{Port: 10548, Target: host},
		}},
	}
	packed, err := msg.Pack()
	if err != nil {
		t.Fatal(err)
	}
	acc := newMDNSAccum()
	acc.add(packed)
	got := acc.servers()
	if len(got) != 1 {
		t.Fatalf("servers = %+v", got)
	}
	if got[0].Name != "Files" || got[0].Host != "files.local" || got[0].Port != 10548 {
		t.Fatalf("got %+v", got[0])
	}
}

func TestMDNSInstanceLabel(t *testing.T) {
	if g := mdnsInstanceLabel("ClassicStack._afpovertcp._tcp.local."); g != "ClassicStack" {
		t.Errorf("got %q", g)
	}
	if g := mdnsInstanceLabel("classicstack._afpovertcp._tcp.local"); !strings.EqualFold(g, "classicstack") {
		t.Errorf("got %q", g)
	}
}
