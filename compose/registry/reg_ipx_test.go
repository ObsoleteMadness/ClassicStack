//go:build ipx || all

package registry

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
	"github.com/ObsoleteMadness/ClassicStack/core/port/ipx"
)

// ipxIdleLink is a non-blocking FrameLink standing in for a real pcap handle: Read
// reports a timeout so the frame read loop spins without busy-erroring, and Close is
// observable.
type ipxIdleLink struct{ closed atomic.Bool }

func (l *ipxIdleLink) Read() (link.Frame, error) {
	if l.closed.Load() {
		return nil, link.ErrClosed
	}
	return nil, link.ErrTimeout
}
func (l *ipxIdleLink) Write(link.Frame) error { return nil }
func (l *ipxIdleLink) Close() error           { l.closed.Store(true); return nil }

// TestIPXFactory_OpenerGoesLive proves the IPX factory builds a LIVE port when the
// BuildContext carries a NIC Opener (M11 device-link injection): Start opens the
// configured interface via the opener (so a real device would be captured) and Stop
// closes the opened link.
func TestIPXFactory_OpenerGoesLive(t *testing.T) {
	var openedIface atomic.Value
	fl := &ipxIdleLink{}
	opener := func(iface string) (link.FrameLink, error) {
		openedIface.Store(iface)
		return fl, nil
	}
	m := config.NewModel()
	m.Set(&port.Section{SKey: ipx.Name, Iface: "eth0", IsEnabled: true, MAC: "00:11:22:33:44:55"})

	c, ok, err := Build(ipx.Name, &BuildContext{Model: m, Opener: opener})
	if err != nil || !ok || c == nil {
		t.Fatalf("Build(IPX) = (%v, %v, %v), want a live component", c, ok, err)
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

// TestIPXFactory_NilOpenerInert proves the graceful-degradation contract: a nil
// Opener still builds an enabled port, but it comes up inert (no opener invoked).
func TestIPXFactory_NilOpenerInert(t *testing.T) {
	m := config.NewModel()
	m.Set(&port.Section{SKey: ipx.Name, Iface: "eth0", IsEnabled: true})

	c, ok, err := Build(ipx.Name, &BuildContext{Model: m})
	if err != nil || !ok || c == nil {
		t.Fatalf("Build(IPX) = (%v, %v, %v), want an enabled (inert) component", c, ok, err)
	}
	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start (inert): %v", err)
	}
	if err := c.Stop(ctx); err != nil {
		t.Fatalf("Stop (inert): %v", err)
	}
}

// TestIPXFactory_OpenerReopensOnRestart proves a Stop→Start reopens the device: the
// opener is called once per Start (a closed handle is terminal), so the port survives
// a UI restart with a fresh link.
func TestIPXFactory_OpenerReopensOnRestart(t *testing.T) {
	var calls atomic.Int32
	links := []*ipxIdleLink{{}, {}}
	opener := func(string) (link.FrameLink, error) {
		n := calls.Add(1)
		return links[n-1], nil
	}
	m := config.NewModel()
	m.Set(&port.Section{SKey: ipx.Name, Iface: "eth0", IsEnabled: true})

	c, ok, err := Build(ipx.Name, &BuildContext{Model: m, Opener: opener})
	if err != nil || !ok || c == nil {
		t.Fatalf("Build(IPX) = (%v, %v, %v)", c, ok, err)
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

// TestIPXInstances_MultipleNamed proves the §M11 named-instance path for IPX: two
// named IPX instances on different interfaces expand to two distinct components, each
// opening its OWN interface — the multi-segment case the singleton shape could not
// express.
func TestIPXInstances_MultipleNamed(t *testing.T) {
	m := config.NewModel()
	m.AddInstance(&port.Section{SKey: ipx.Name, Name: "ipx-lab", Iface: "eth0", IsEnabled: true})
	m.AddInstance(&port.Section{SKey: ipx.Name, Name: "ipx-dmz", Iface: "eth1", IsEnabled: true})

	opened := map[string]string{}
	for _, id := range Instances(m) {
		if id.Key != ipx.Name {
			continue
		}
		var iface atomic.Value
		opener := func(i string) (link.FrameLink, error) { iface.Store(i); return &ipxIdleLink{}, nil }
		c, ok, err := Build(id.Key, &BuildContext{Model: m, Instance: id.Instance, Opener: opener})
		if err != nil || !ok || c == nil {
			t.Fatalf("Build(%s/%s) = (%v, %v, %v)", id.Key, id.Instance, c, ok, err)
		}
		if c.Name() != id.Instance {
			t.Fatalf("instance %q built a component named %q", id.Instance, c.Name())
		}
		ctx := context.Background()
		if err := c.Start(ctx); err != nil {
			t.Fatalf("Start %s: %v", id.Instance, err)
		}
		opened[c.Name()] = iface.Load().(string)
		c.Stop(ctx)
	}
	if opened["ipx-lab"] != "eth0" || opened["ipx-dmz"] != "eth1" {
		t.Fatalf("instances opened the wrong interfaces: %v", opened)
	}
}
