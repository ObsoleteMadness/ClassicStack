package runtime

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/adapter/dsi"
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/service/afp"
)

func TestWireDSI_ConfiguredAddrWiresHandlerAndAddr(t *testing.T) {
	af := afp.New(nil)
	af.SetTransports([]string{afp.TransportDDP, afp.TransportTCP})
	af.SetTCPListenAddr(":5480")
	tr := dsi.New("", nil, nil)
	comps := map[string]component.Component{afp.Name: af, dsi.Name: tr}

	wireDSI(af, comps)

	if got := tr.Binding(); got != ":5480" {
		t.Fatalf("Binding() = %q, want %q", got, ":5480")
	}
}

func TestWireDSI_NoTCPAddrStaysInert(t *testing.T) {
	af := afp.New(nil)
	af.SetTransports([]string{afp.TransportDDP, afp.TransportTCP}) // tcp bound, but no address
	tr := dsi.New("", nil, nil)
	comps := map[string]component.Component{afp.Name: af, dsi.Name: tr}

	wireDSI(af, comps)

	if got := tr.Binding(); got != "" {
		t.Fatalf("Binding() = %q, want empty (no tcp_addr configured)", got)
	}
}

func TestWireDSI_TCPNotBoundStaysInert(t *testing.T) {
	af := afp.New(nil)
	af.SetTransports([]string{afp.TransportDDP}) // tcp not in the bound list
	af.SetTCPListenAddr(":5480")
	tr := dsi.New("", nil, nil)
	comps := map[string]component.Component{afp.Name: af, dsi.Name: tr}

	wireDSI(af, comps)

	if got := tr.Binding(); got != "" {
		t.Fatalf("Binding() = %q, want empty (tcp not bound)", got)
	}
}

func TestWireDSI_NoAFPServiceIsNoop(t *testing.T) {
	tr := dsi.New("", nil, nil)
	comps := map[string]component.Component{dsi.Name: tr}
	wireDSI(nil, comps) // must not panic
	if got := tr.Binding(); got != "" {
		t.Fatalf("Binding() = %q, want empty", got)
	}
}
