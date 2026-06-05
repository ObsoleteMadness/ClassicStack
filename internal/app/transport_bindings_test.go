//go:build all

package app

import (
	"context"
	"strings"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/config"
	"github.com/ObsoleteMadness/ClassicStack/pkg/status"
	netbiosproto "github.com/ObsoleteMadness/ClassicStack/protocol/netbios"
	"github.com/ObsoleteMadness/ClassicStack/service/netbios"
)

// fakeBindingNetBIOS is a minimal NetBIOSHook whose Service() is a real
// netbios.Service, so transport attach/detach exercises the live add/remove
// path while BuildTransport hands back inert fake transports.
type fakeBindingNetBIOS struct {
	svc *netbios.Service
}

func (f *fakeBindingNetBIOS) Start(_ context.Context) error    { return nil }
func (f *fakeBindingNetBIOS) Stop() error                      { return nil }
func (f *fakeBindingNetBIOS) NameService() netbios.NameService { return f.svc.NameService() }
func (f *fakeBindingNetBIOS) Service() *netbios.Service        { return f.svc }
func (f *fakeBindingNetBIOS) BuildTransport(string) netbios.Transport {
	return &bindingFakeTransport{}
}

// bindingFakeTransport is an inert netbios.Transport for binding tests.
type bindingFakeTransport struct{}

func (*bindingFakeTransport) Start(_ context.Context) error                   { return nil }
func (*bindingFakeTransport) Stop() error                                     { return nil }
func (*bindingFakeTransport) SendName(_ netbiosproto.Name) error              { return nil }
func (*bindingFakeTransport) SendDatagram(_ *netbiosproto.Datagram) error     { return nil }
func (*bindingFakeTransport) SendSession(_ *netbiosproto.SessionPacket) error { return nil }
func (*bindingFakeTransport) SetCommandHandler(_ netbios.CommandHandler)      {}

// TestDetachAttachTransportBindings verifies the supervisor's binding helpers:
// detaching the NetBEUI binding removes only that transport from NetBIOS and
// refreshes the NetBIOS status; attaching re-adds it. This is the unit-level
// proof of "stopping NetBEUI just removes the NetBEUI binding".
func TestDetachAttachTransportBindings(t *testing.T) {
	reg := status.NewRegistry()
	svc := netbios.NewService("CLASSICSTACK", "", nil)
	nb := &fakeBindingNetBIOS{svc: svc}

	s := &Supervisor{
		reg:     reg,
		hooks:   map[string]hook{},
		netbios: nb,
	}
	s.cfg.NetBIOSEnabled = true
	s.transportBindings = map[string][]transportBinding{
		"NetBEUI": {s.netbiosTransportBinding("netbeui")},
		"IPX":     {s.netbiosTransportBinding("ipx")},
	}
	reg.Set(status.Unit{Name: "NetBIOS", Kind: status.KindHook, Enabled: true, Running: true})

	// Start with both transports bound.
	s.attachTransportBindings("NetBEUI")
	s.attachTransportBindings("IPX")
	if got := svc.Transports(); len(got) != 2 {
		t.Fatalf("after attach: Transports()=%v, want 2", got)
	}

	// Detach NetBEUI: only "netbeui" leaves; "ipx" stays.
	s.detachTransportBindings("NetBEUI")
	got := svc.Transports()
	if len(got) != 1 || got[0] != "ipx" {
		t.Fatalf("after detach NetBEUI: Transports()=%v, want [ipx]", got)
	}
	// NetBIOS status must reflect the reduced transport set and stay running.
	nbUnit := unitByName(reg, "NetBIOS")
	if nbUnit.Properties["transports"] != "IPX" {
		t.Fatalf("NetBIOS transports property=%q, want %q", nbUnit.Properties["transports"], "IPX")
	}
	if !nbUnit.Running {
		t.Fatal("NetBIOS must stay running after a transport detach")
	}

	// Re-attach NetBEUI.
	s.attachTransportBindings("NetBEUI")
	if got := svc.Transports(); len(got) != 2 {
		t.Fatalf("after re-attach: Transports()=%v, want 2", got)
	}
}

// TestSMBStatusShowsTransportsNotPhantomPort verifies SMB's status no longer
// advertises the unimplemented NBT :139 binding and instead lists the real
// served transports sourced from NetBIOS.
func TestSMBStatusShowsTransportsNotPhantomPort(t *testing.T) {
	reg := status.NewRegistry()
	svc := netbios.NewService("CLASSICSTACK", "", nil)
	_ = svc.AddTransport("ipx", &bindingFakeTransport{})
	_ = svc.AddTransport("netbeui", &bindingFakeTransport{})

	model := &config.Model{}
	model.SMB.NBTBinding = ":139"
	model.SMB.Workgroup = "WORKGROUP"
	model.SMB.ServerName = "CLASSICSTACK"

	s := &Supervisor{
		reg:     reg,
		hooks:   map[string]hook{},
		model:   model,
		netbios: &fakeBindingNetBIOS{svc: svc},
	}
	// SMB only lists NetBIOS as a transport while NetBIOS is running.
	reg.Set(status.Unit{Name: "NetBIOS", Kind: status.KindHook, Running: true})
	s.registerSMBStatus(true)

	u := unitByName(reg, "SMB")
	if u.Binding == ":139" {
		t.Fatalf("SMB binding still shows phantom :139")
	}
	transports := u.Properties["transports"]
	if !strings.Contains(transports, "NetBIOS") || !strings.Contains(transports, "IPX") || !strings.Contains(transports, "NetBEUI") {
		t.Fatalf("SMB transports property = %q, want it to name NetBIOS/IPX/NetBEUI", transports)
	}
}

// TestSMBDropsNetBIOSWhenStopped verifies that when NetBIOS is not running,
// SMB no longer lists NetBIOS as a served transport (the reported bug: SMB
// kept showing NetBIOS after NetBIOS was stopped).
func TestSMBDropsNetBIOSWhenStopped(t *testing.T) {
	reg := status.NewRegistry()
	svc := netbios.NewService("CLASSICSTACK", "", nil)
	_ = svc.AddTransport("ipx", &bindingFakeTransport{})

	model := &config.Model{}
	s := &Supervisor{
		reg:     reg,
		hooks:   map[string]hook{},
		model:   model,
		netbios: &fakeBindingNetBIOS{svc: svc},
	}
	// NetBIOS stopped.
	reg.Set(status.Unit{Name: "NetBIOS", Kind: status.KindHook, Running: false})
	s.registerSMBStatus(true)

	transports := unitByName(reg, "SMB").Properties["transports"]
	if strings.Contains(transports, "NetBIOS") {
		t.Fatalf("SMB still lists NetBIOS while NetBIOS is stopped: %q", transports)
	}
	if transports != "none" {
		t.Fatalf("SMB transports = %q, want \"none\" with no other transports running", transports)
	}
}

// TestNetBIOSStatusShowsTransportsAfterStart verifies the reported bug fix:
// after NetBIOS starts with transports bound, its status lists them (rather
// than the stale empty set captured at wire time). onHookStateChanged drives
// the refresh; here we call refreshNetBIOSStatus with NetBIOS marked running.
func TestNetBIOSStatusShowsTransportsAfterStart(t *testing.T) {
	reg := status.NewRegistry()
	svc := netbios.NewService("CLASSICSTACK", "", nil)
	_ = svc.AddTransport("ipx", &bindingFakeTransport{})
	_ = svc.AddTransport("netbeui", &bindingFakeTransport{})

	s := &Supervisor{reg: reg, hooks: map[string]hook{}, netbios: &fakeBindingNetBIOS{svc: svc}}
	s.cfg.NetBIOSEnabled = true

	// Before start: not running -> "none".
	reg.Set(status.Unit{Name: "NetBIOS", Kind: status.KindHook, Running: false})
	s.refreshNetBIOSStatus(true)
	if got := unitByName(reg, "NetBIOS").Properties["transports"]; got != "none" {
		t.Fatalf("stopped NetBIOS transports = %q, want none", got)
	}

	// After start: running -> lists IPX, NetBEUI.
	reg.SetRunning("NetBIOS", true)
	s.refreshNetBIOSStatus(true)
	got := unitByName(reg, "NetBIOS").Properties["transports"]
	if !strings.Contains(got, "IPX") || !strings.Contains(got, "NetBEUI") {
		t.Fatalf("running NetBIOS transports = %q, want IPX and NetBEUI", got)
	}
}

func unitByName(reg *status.Registry, name string) status.Unit {
	for _, u := range reg.Snapshot() {
		if u.Name == name {
			return u
		}
	}
	return status.Unit{}
}
