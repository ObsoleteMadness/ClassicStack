//go:build ethertalk || all

package registry

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
	"github.com/ObsoleteMadness/ClassicStack/core/port/ethertalk"
)

// idleFrameLink is a non-blocking FrameLink: Read always reports a timeout so the
// port's read loop spins without busy-erroring, and Close is observable. It stands
// in for a real pcap handle in a core-only test.
type idleFrameLink struct{ closed atomic.Bool }

func (f *idleFrameLink) Read() (link.Frame, error) {
	if f.closed.Load() {
		return nil, link.ErrClosed
	}
	return nil, link.ErrTimeout
}
func (f *idleFrameLink) Write(link.Frame) error { return nil }
func (f *idleFrameLink) Close() error           { f.closed.Store(true); return nil }

func enabledEtherTalkModel(mac string) *config.Model {
	m := config.NewModel()
	m.Set(&port.Section{SKey: ethertalk.Name, Iface: "eth0", IsEnabled: true, MAC: mac})
	return m
}

// TestEtherTalkFactory_OpenerGoesLive proves the EtherTalk factory builds a LIVE
// port when the BuildContext carries an Opener: starting the port calls the opener
// for the configured interface (so a real device would be captured), and stopping
// it closes the opened link. This is the slice-B data path: config → device link.
func TestEtherTalkFactory_OpenerGoesLive(t *testing.T) {
	var openedIface atomic.Value
	fl := &idleFrameLink{}
	opener := func(iface string) (link.FrameLink, error) {
		openedIface.Store(iface)
		return fl, nil
	}

	c, ok, err := Build(ethertalk.Name, &BuildContext{
		Model:  enabledEtherTalkModel("00:11:22:aa:bb:cc"),
		Opener: opener,
	})
	if err != nil || !ok {
		t.Fatalf("Build(EtherTalk) = (_, %v, %v), want (_, true, nil)", ok, err)
	}
	if c == nil {
		t.Fatal("enabled EtherTalk with an opener built a nil component")
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

// TestEtherTalkFactory_InheritsBridgeInterface proves the shared-Bridge concept:
// an EtherTalk section with NO iface of its own inherits the global Bridge NIC, so
// the opener is called with the bridge interface — several ports could thus share
// one NIC. A section that DOES name an iface overrides the bridge.
func TestEtherTalkFactory_InheritsBridgeInterface(t *testing.T) {
	check := func(t *testing.T, sectionIface, bridge, want string) {
		t.Helper()
		var openedIface atomic.Value
		opener := func(iface string) (link.FrameLink, error) {
			openedIface.Store(iface)
			return &idleFrameLink{}, nil
		}
		m := config.NewModel()
		m.Bridge = config.InterfaceSection{Name: bridge}
		m.Set(&port.Section{SKey: ethertalk.Name, Iface: sectionIface, IsEnabled: true})

		c, ok, err := Build(ethertalk.Name, &BuildContext{Model: m, Opener: opener})
		if err != nil || !ok || c == nil {
			t.Fatalf("Build = (%v, %v, %v)", c, ok, err)
		}
		ctx := context.Background()
		if err := c.Start(ctx); err != nil {
			t.Fatalf("Start: %v", err)
		}
		defer c.Stop(ctx)
		if got := openedIface.Load(); got != want {
			t.Fatalf("opened iface = %v, want %v", got, want)
		}
	}
	t.Run("inherits bridge when iface empty", func(t *testing.T) {
		check(t, "", "br0", "br0")
	})
	t.Run("override beats bridge", func(t *testing.T) {
		check(t, "eth9", "br0", "eth9")
	})
}

// TestEtherTalkFactory_NilOpenerInert proves the graceful-degradation contract: a
// nil Opener (no device backend in this build) still builds an enabled port, but it
// comes up inert — no opener is ever invoked.
func TestEtherTalkFactory_NilOpenerInert(t *testing.T) {
	c, ok, err := Build(ethertalk.Name, &BuildContext{Model: enabledEtherTalkModel("")})
	if err != nil || !ok {
		t.Fatalf("Build(EtherTalk) = (_, %v, %v), want (_, true, nil)", ok, err)
	}
	if c == nil {
		t.Fatal("enabled EtherTalk built a nil component with a nil opener")
	}
	// It must still start/stop cleanly (inert lifecycle).
	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start (inert): %v", err)
	}
	if err := c.Stop(ctx); err != nil {
		t.Fatalf("Stop (inert): %v", err)
	}
}

// TestEtherTalkFactory_OpenerReopensOnRestart proves a Stop→Start reopens the
// device: the opener is called once per Start (a closed pcap handle is terminal),
// so the port survives a UI restart. The second link is distinct from the first.
func TestEtherTalkFactory_OpenerReopensOnRestart(t *testing.T) {
	var calls atomic.Int32
	links := []*idleFrameLink{{}, {}}
	opener := func(string) (link.FrameLink, error) {
		n := calls.Add(1)
		return links[n-1], nil
	}
	c, ok, err := Build(ethertalk.Name, &BuildContext{Model: enabledEtherTalkModel(""), Opener: opener})
	if err != nil || !ok {
		t.Fatalf("Build = (_, %v, %v)", ok, err)
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
