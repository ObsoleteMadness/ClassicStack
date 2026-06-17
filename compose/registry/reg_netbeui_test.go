//go:build netbeui || all

package registry

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
	"github.com/ObsoleteMadness/ClassicStack/core/port/netbeui"
)

// nbIdleLink is a non-blocking FrameLink standing in for a real pcap handle.
type nbIdleLink struct{ closed atomic.Bool }

func (l *nbIdleLink) Read() (link.Frame, error) {
	if l.closed.Load() {
		return nil, link.ErrClosed
	}
	return nil, link.ErrTimeout
}
func (l *nbIdleLink) Write(link.Frame) error { return nil }
func (l *nbIdleLink) Close() error           { l.closed.Store(true); return nil }

// TestNetBEUIFactory_OpenerGoesLive proves the NetBEUI factory builds a LIVE port
// when the BuildContext carries a NIC Opener (M11 device-link injection): Start opens
// the configured interface and Stop closes the opened link.
func TestNetBEUIFactory_OpenerGoesLive(t *testing.T) {
	var openedIface atomic.Value
	fl := &nbIdleLink{}
	opener := func(iface string) (link.FrameLink, error) {
		openedIface.Store(iface)
		return fl, nil
	}
	m := config.NewModel()
	m.Set(&port.Section{SKey: netbeui.Name, Iface: "eth0", IsEnabled: true, MAC: "00:aa:bb:cc:dd:ee"})

	c, ok, err := Build(netbeui.Name, &BuildContext{Model: m, Opener: opener})
	if err != nil || !ok || c == nil {
		t.Fatalf("Build(NetBEUI) = (%v, %v, %v), want a live component", c, ok, err)
	}

	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := openedIface.Load(); got != "eth0" {
		t.Fatalf("opener called with iface %v, want eth0 (port did not go live)", got)
	}
	if err := c.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !fl.closed.Load() {
		t.Fatal("Stop did not close the opened FrameLink")
	}
}

// TestNetBEUIFactory_NilOpenerInert proves the graceful-degradation contract: a nil
// Opener still builds an enabled port that comes up inert.
func TestNetBEUIFactory_NilOpenerInert(t *testing.T) {
	m := config.NewModel()
	m.Set(&port.Section{SKey: netbeui.Name, Iface: "eth0", IsEnabled: true})

	c, ok, err := Build(netbeui.Name, &BuildContext{Model: m})
	if err != nil || !ok || c == nil {
		t.Fatalf("Build(NetBEUI) = (%v, %v, %v), want an enabled (inert) component", c, ok, err)
	}
	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start (inert): %v", err)
	}
	if err := c.Stop(ctx); err != nil {
		t.Fatalf("Stop (inert): %v", err)
	}
}

// TestNetBEUIFactory_OpenerReopensOnRestart proves a Stop→Start reopens the device.
func TestNetBEUIFactory_OpenerReopensOnRestart(t *testing.T) {
	var calls atomic.Int32
	links := []*nbIdleLink{{}, {}}
	opener := func(string) (link.FrameLink, error) {
		n := calls.Add(1)
		return links[n-1], nil
	}
	m := config.NewModel()
	m.Set(&port.Section{SKey: netbeui.Name, Iface: "eth0", IsEnabled: true})

	c, ok, err := Build(netbeui.Name, &BuildContext{Model: m, Opener: opener})
	if err != nil || !ok || c == nil {
		t.Fatalf("Build(NetBEUI) = (%v, %v, %v)", c, ok, err)
	}
	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start #1: %v", err)
	}
	if err := c.Stop(ctx); err != nil {
		t.Fatalf("Stop #1: %v", err)
	}
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start #2: %v", err)
	}
	defer c.Stop(ctx)
	if calls.Load() != 2 {
		t.Fatalf("opener called %d times across two Starts, want 2 (no reopen)", calls.Load())
	}
	if !links[0].closed.Load() {
		t.Fatal("first link not closed on Stop #1")
	}
}
