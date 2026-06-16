//go:build localtalk || all

package registry

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
	"github.com/ObsoleteMadness/ClassicStack/core/port/localtalk"
)

// swapLtoudpOpen replaces the LToUDP transport open seam for the duration of a
// test, restoring it on cleanup.
func swapLtoudpOpen(t *testing.T, fn func(iface string) (link.FrameLink, error)) {
	t.Helper()
	prev := ltoudpOpen
	ltoudpOpen = fn
	t.Cleanup(func() { ltoudpOpen = prev })
}

// swapTashtalkOpen replaces the TashTalk serial open seam for the duration of a
// test, restoring it on cleanup.
func swapTashtalkOpen(t *testing.T, fn func(dev string) (link.FrameLink, error)) {
	t.Helper()
	prev := tashtalkOpen
	tashtalkOpen = fn
	t.Cleanup(func() { tashtalkOpen = prev })
}

func enabledLocalTalkModel(iface string) *config.Model {
	m := config.NewModel()
	m.Set(&port.Section{SKey: localtalk.Name, Iface: iface, IsEnabled: true})
	return m
}

// anyOpener returns a BuildContext-level Opener so the factory takes the LIVE
// path. LocalTalk does NOT call this opener (it opens LToUDP directly); it is
// only the "device backends enabled" switch.
func anyOpener() LinkOpener {
	return func(string) (link.FrameLink, error) { return &idleFrameLink{}, nil }
}

// TestLocalTalkFactory_GoesLiveOnLToUDP proves the factory builds a LIVE port
// using the LToUDP transport (its own iface address, NOT the bridge or the pcap
// opener): starting it opens an LToUDP link for the configured interface, and
// stopping it closes that link.
func TestLocalTalkFactory_GoesLiveOnLToUDP(t *testing.T) {
	var openedIface atomic.Value
	fl := &idleFrameLink{}
	swapLtoudpOpen(t, func(iface string) (link.FrameLink, error) {
		openedIface.Store(iface)
		return fl, nil
	})

	c, ok, err := Build(localtalk.Name, &BuildContext{
		Model:  enabledLocalTalkModel("192.168.1.5"),
		Opener: anyOpener(),
	})
	if err != nil || !ok || c == nil {
		t.Fatalf("Build(LocalTalk) = (%v, %v, %v), want live component", c, ok, err)
	}

	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := openedIface.Load(); got != "192.168.1.5" {
		t.Fatalf("LToUDP opened with iface %v, want 192.168.1.5 (the section addr, not the bridge)", got)
	}
	if err := c.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !fl.closed.Load() {
		t.Fatal("Stop did not close the opened LToUDP link")
	}
}

// TestLocalTalkFactory_SerialTransport proves transport = "serial" dispatches to
// the TashTalk serial open seam with the section's Iface as the device path —
// NOT to LToUDP. The LToUDP seam must NOT be touched.
func TestLocalTalkFactory_SerialTransport(t *testing.T) {
	var openedDev atomic.Value
	var ltoudpCalled atomic.Bool
	swapTashtalkOpen(t, func(dev string) (link.FrameLink, error) {
		openedDev.Store(dev)
		return &idleFrameLink{}, nil
	})
	swapLtoudpOpen(t, func(string) (link.FrameLink, error) {
		ltoudpCalled.Store(true)
		return &idleFrameLink{}, nil
	})

	m := config.NewModel()
	m.Set(&port.Section{SKey: localtalk.Name, Iface: "COM3", Transport: port.TransportSerial, IsEnabled: true})

	c, ok, err := Build(localtalk.Name, &BuildContext{Model: m, Opener: anyOpener()})
	if err != nil || !ok || c == nil {
		t.Fatalf("Build(LocalTalk serial) = (%v, %v, %v)", c, ok, err)
	}
	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer c.Stop(ctx)
	if got := openedDev.Load(); got != "COM3" {
		t.Fatalf("TashTalk opened device %v, want COM3", got)
	}
	if ltoudpCalled.Load() {
		t.Fatal("LToUDP seam was called for a serial-transport LocalTalk port")
	}
}

// TestLocalTalkFactory_IgnoresBridge proves LocalTalk does NOT consult the shared
// Bridge: even with a bridge NIC set and an empty section iface, the LToUDP open
// gets the empty iface (join-on-any), never the bridge name. LToUDP is a
// UDP-tunnelled segment, not an L2/NIC port.
func TestLocalTalkFactory_IgnoresBridge(t *testing.T) {
	var openedIface atomic.Value
	swapLtoudpOpen(t, func(iface string) (link.FrameLink, error) {
		openedIface.Store(iface)
		return &idleFrameLink{}, nil
	})
	m := config.NewModel()
	m.Bridge = config.InterfaceSection{Name: "br0"}
	m.Set(&port.Section{SKey: localtalk.Name, Iface: "", IsEnabled: true})

	c, ok, err := Build(localtalk.Name, &BuildContext{Model: m, Opener: anyOpener()})
	if err != nil || !ok || c == nil {
		t.Fatalf("Build = (%v, %v, %v)", c, ok, err)
	}
	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer c.Stop(ctx)
	if got := openedIface.Load(); got != "" {
		t.Fatalf("LToUDP opened with iface %v, want empty (bridge must NOT leak in)", got)
	}
}

// TestLocalTalkFactory_NilOpenerInert proves the graceful-degradation contract: a
// nil Opener builds an enabled port that comes up inert — the LToUDP transport is
// never opened (no socket bound), so it is safe in a tag-free build / the
// conformance harness.
func TestLocalTalkFactory_NilOpenerInert(t *testing.T) {
	var opened atomic.Bool
	swapLtoudpOpen(t, func(string) (link.FrameLink, error) {
		opened.Store(true)
		return &idleFrameLink{}, nil
	})

	c, ok, err := Build(localtalk.Name, &BuildContext{Model: enabledLocalTalkModel("eth0")})
	if err != nil || !ok || c == nil {
		t.Fatalf("Build(LocalTalk) = (%v, %v, %v), want inert component", c, ok, err)
	}
	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start (inert): %v", err)
	}
	if err := c.Stop(ctx); err != nil {
		t.Fatalf("Stop (inert): %v", err)
	}
	if opened.Load() {
		t.Fatal("nil Opener still opened an LToUDP socket; should stay inert")
	}
}

// TestLocalTalkFactory_ReopensOnRestart proves a Stop→Start reopens the LToUDP
// transport: the open seam is called once per Start (a closed socket is
// terminal), so the port survives a UI restart with a fresh link.
func TestLocalTalkFactory_ReopensOnRestart(t *testing.T) {
	var calls atomic.Int32
	links := []*idleFrameLink{{}, {}}
	swapLtoudpOpen(t, func(string) (link.FrameLink, error) {
		n := calls.Add(1)
		return links[n-1], nil
	})

	c, ok, err := Build(localtalk.Name, &BuildContext{Model: enabledLocalTalkModel(""), Opener: anyOpener()})
	if err != nil || !ok || c == nil {
		t.Fatalf("Build = (%v, %v, %v)", c, ok, err)
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
		t.Fatalf("LToUDP opened %d times across two Starts, want 2", calls.Load())
	}
	if !links[0].closed.Load() {
		t.Fatal("first LToUDP link not closed on Stop #1")
	}
}
