package main

import (
	"github.com/ObsoleteMadness/ClassicStack/service"
	"github.com/ObsoleteMadness/ClassicStack/service/zip"
)

// MacIPHook is the cmd-layer abstraction over the optional MacIP gateway.
// The real implementation lives behind //go:build macip; the stub returns
// nil so router-only builds compile without the macip dependency surface.
type MacIPHook interface {
	Service() service.Service
	PinLeaseToSession(net uint16, node, sessID uint8)
	UnpinLeaseFromSession(sessID uint8)
	MarkSessionActivity(sessID uint8)
}

// macIPAFPHooks adapts a MacIPHook to the AFPSessionHooks interface
// expected by AFP's ASP transport, so the two optional subsystems can
// be wired together without either side importing the other.
type macIPAFPHooks struct{ h MacIPHook }

func (a macIPAFPHooks) OnOpen(net uint16, node, sessID uint8) {
	a.h.PinLeaseToSession(net, node, sessID)
}
func (a macIPAFPHooks) OnClose(sessID uint8)    { a.h.UnpinLeaseFromSession(sessID) }
func (a macIPAFPHooks) OnActivity(sessID uint8) { a.h.MarkSessionActivity(sessID) }

// MacIPConfig collects every flag value wireMacIP needs, decoupling the
// caller (main.go, tag-neutral) from the macip package directly.
type MacIPConfig struct {
	Enabled         bool
	NATGatewayIP    string
	NATSubnet       string
	Nameserver      string
	Zone            string
	IPGateway       string
	NAT             bool
	DHCPRelay       bool
	StateFile       string
	PcapDevice      string
	BridgeHostMAC   string
	PcapHWAddr      string
	EtherTalkZone   string
	EtherTalkBackend string
	NBP             *zip.NameInformationService
}
