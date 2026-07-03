//go:build localtalk || all

package registry

import (
	"context"
	"io"
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

// swapTashtalkFrame replaces the TashTalk SerialFramer (the byte-stream→FrameLink
// wrapper) for the duration of a test, restoring it on cleanup. The device-open
// itself is the injected ctx.Serial (see serialOpener), so a test drives the serial
// path through both seams: ctx.Serial yields a fake stream; this frames it.
func swapTashtalkFrame(t *testing.T, fn SerialFramer) {
	t.Helper()
	prev := tashtalkFrame
	tashtalkFrame = fn
	t.Cleanup(func() { tashtalkFrame = prev })
}

func enabledSegmentModel(key, iface string) *config.Model {
	m := config.NewModel()
	m.Set(&port.Section{SKey: key, Iface: iface, IsEnabled: true})
	return m
}

// nopStream is an io.ReadWriteCloser standing in for an open serial device in the
// factory tests (the framing itself is tested in adapter/link/tashtalk).
type nopStream struct{}

func (nopStream) Read([]byte) (int, error)    { return 0, io.EOF }
func (nopStream) Write(p []byte) (int, error) { return len(p), nil }
func (nopStream) Close() error                { return nil }

// anyOpener returns a BuildContext-level NIC Opener so a NIC-bound factory takes the
// LIVE path. A LocalTalk segment does NOT call this opener (LToUDP opens its own
// transport; TashTalk uses ctx.Serial) — it is only the "NIC backend enabled" switch.
func anyOpener() LinkOpener {
	return func(string) (link.FrameLink, error) { return &idleFrameLink{}, nil }
}

// serialOpenerRecording returns a SerialOpener that records the device it was asked
// to open and yields a nop stream (so the TashTalk framer succeeds). The recorded
// device is read back via the returned pointer.
func serialOpenerRecording(dev *atomic.Value) SerialOpener {
	return func(device string, _ uint) (io.ReadWriteCloser, error) {
		dev.Store(device)
		return nopStream{}, nil
	}
}

// TestLToUDPFactory_GoesLive proves the LToUDP port builds a LIVE port using the
// LToUDP transport (its own iface address, NOT the bridge or the pcap opener):
// starting it opens an LToUDP link for the configured interface, stopping it
// closes that link.
func TestLToUDPFactory_GoesLive(t *testing.T) {
	var openedIface atomic.Value
	fl := &idleFrameLink{}
	swapLtoudpOpen(t, func(iface string) (link.FrameLink, error) {
		openedIface.Store(iface)
		return fl, nil
	})

	c, ok, err := Build(localtalk.NameLToUDP, &BuildContext{
		Model:  enabledSegmentModel(localtalk.NameLToUDP, "192.168.1.5"),
		Opener: anyOpener(),
	})
	if err != nil || !ok || c == nil {
		t.Fatalf("Build(LToUDP) = (%v, %v, %v), want live component", c, ok, err)
	}
	if c.Name() != localtalk.NameLToUDP {
		t.Fatalf("component Name = %q, want %q", c.Name(), localtalk.NameLToUDP)
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

// TestTashTalkFactory_GoesLive proves the TashTalk port is a DISTINCT component that
// opens the SERIAL transport via the injected serial opener (M11.c/D7) with the
// section's Iface as the device path — never LToUDP, never the NIC opener.
func TestTashTalkFactory_GoesLive(t *testing.T) {
	var openedDev atomic.Value
	var ltoudpCalled, framed atomic.Bool
	swapTashtalkFrame(t, func(s io.ReadWriteCloser) (link.FrameLink, error) {
		framed.Store(true)
		return &idleFrameLink{}, nil
	})
	swapLtoudpOpen(t, func(string) (link.FrameLink, error) {
		ltoudpCalled.Store(true)
		return &idleFrameLink{}, nil
	})

	c, ok, err := Build(localtalk.NameTashTalk, &BuildContext{
		Model:  enabledSegmentModel(localtalk.NameTashTalk, "COM3"),
		Serial: serialOpenerRecording(&openedDev),
	})
	if err != nil || !ok || c == nil {
		t.Fatalf("Build(TashTalk) = (%v, %v, %v)", c, ok, err)
	}
	if c.Name() != localtalk.NameTashTalk {
		t.Fatalf("component Name = %q, want %q", c.Name(), localtalk.NameTashTalk)
	}

	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer c.Stop(ctx)
	if got := openedDev.Load(); got != "COM3" {
		t.Fatalf("TashTalk opened device %v, want COM3", got)
	}
	if !framed.Load() {
		t.Fatal("TashTalk did not frame the opened serial stream")
	}
	if ltoudpCalled.Load() {
		t.Fatal("LToUDP seam was called for the TashTalk segment")
	}
}

// TestTashTalkFactory_ReadsDeviceAndBaudFromPort proves a TashTalk port owns its own
// serial line: the DEVICE and BAUD come from the PORT section (Section.Device/Baud),
// not from a named serial interface. This is the reversal of the earlier §3b/D7
// serial-as-interface move — serial is a port property now ("one interface = the
// uplink bridge").
func TestTashTalkFactory_ReadsDeviceAndBaudFromPort(t *testing.T) {
	var openedDev atomic.Value
	var openedBaud atomic.Uint64
	swapTashtalkFrame(t, func(io.ReadWriteCloser) (link.FrameLink, error) { return &idleFrameLink{}, nil })

	m := config.NewModel()
	// The port carries its own device/baud — no serial interface in the namespace.
	m.AddInstance(&port.Section{SKey: localtalk.NameTashTalk, Name: "tt-attic", Device: "/dev/ttyUSB0", Baud: 57600, IsEnabled: true})

	serial := func(device string, baud uint) (io.ReadWriteCloser, error) {
		openedDev.Store(device)
		openedBaud.Store(uint64(baud))
		return nopStream{}, nil
	}
	c, ok, err := Build(localtalk.NameTashTalk, &BuildContext{Model: m, Instance: "tt-attic", Serial: serial})
	if err != nil || !ok || c == nil {
		t.Fatalf("Build(TashTalk/tt-attic) = (%v, %v, %v)", c, ok, err)
	}
	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer c.Stop(ctx)
	if got := openedDev.Load(); got != "/dev/ttyUSB0" {
		t.Fatalf("opened device %v, want /dev/ttyUSB0 (from the port section)", got)
	}
	if got := openedBaud.Load(); got != 57600 {
		t.Fatalf("opened baud %d, want 57600 (from the port section)", got)
	}
}

// TestLocalTalkSegments_AreDistinct proves LToUDP and TashTalk are two separate
// components that can run at once, each opening its OWN transport — modelling two
// distinct AppleTalk segments, not one port with a transport switch.
func TestLocalTalkSegments_AreDistinct(t *testing.T) {
	var ltoudpDev, ttDev atomic.Value
	swapLtoudpOpen(t, func(iface string) (link.FrameLink, error) {
		ltoudpDev.Store(iface)
		return &idleFrameLink{}, nil
	})
	swapTashtalkFrame(t, func(io.ReadWriteCloser) (link.FrameLink, error) { return &idleFrameLink{}, nil })

	m := config.NewModel()
	m.Set(&port.Section{SKey: localtalk.NameLToUDP, Iface: "", IsEnabled: true})
	m.Set(&port.Section{SKey: localtalk.NameTashTalk, Iface: "/dev/ttyUSB0", IsEnabled: true})

	ctx := context.Background()
	for _, key := range []string{localtalk.NameLToUDP, localtalk.NameTashTalk} {
		c, ok, err := Build(key, &BuildContext{Model: m, Opener: anyOpener(), Serial: serialOpenerRecording(&ttDev)})
		if err != nil || !ok || c == nil {
			t.Fatalf("Build(%s) = (%v, %v, %v)", key, c, ok, err)
		}
		if err := c.Start(ctx); err != nil {
			t.Fatalf("Start(%s): %v", key, err)
		}
		defer c.Stop(ctx)
	}
	if ltoudpDev.Load() != "" {
		t.Fatalf("LToUDP opened %v, want empty", ltoudpDev.Load())
	}
	if ttDev.Load() != "/dev/ttyUSB0" {
		t.Fatalf("TashTalk opened %v, want /dev/ttyUSB0", ttDev.Load())
	}
}

// TestLToUDPFactory_IgnoresBridge proves a LocalTalk segment does NOT consult the
// shared Bridge: even with a bridge NIC set and an empty section iface, the
// LToUDP open gets the empty iface (join-on-any), never the bridge name.
func TestLToUDPFactory_IgnoresBridge(t *testing.T) {
	var openedIface atomic.Value
	swapLtoudpOpen(t, func(iface string) (link.FrameLink, error) {
		openedIface.Store(iface)
		return &idleFrameLink{}, nil
	})
	m := config.NewModel()
	m.SetInterface(config.InterfaceSection{Name: "br0", Kind: config.IfaceKindBridge, Default: true})
	m.Set(&port.Section{SKey: localtalk.NameLToUDP, Iface: "", IsEnabled: true})

	c, ok, err := Build(localtalk.NameLToUDP, &BuildContext{Model: m, Opener: anyOpener()})
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
// nil Opener builds an enabled port that comes up inert — neither transport is
// opened — so it is safe in a tag-free build / the conformance harness. Covers
// both segment keys.
func TestLocalTalkFactory_NilOpenerInert(t *testing.T) {
	var ltoudpOpened, ttFramed atomic.Bool
	swapLtoudpOpen(t, func(string) (link.FrameLink, error) { ltoudpOpened.Store(true); return &idleFrameLink{}, nil })
	swapTashtalkFrame(t, func(io.ReadWriteCloser) (link.FrameLink, error) { ttFramed.Store(true); return &idleFrameLink{}, nil })

	ctx := context.Background()
	for _, key := range []string{localtalk.NameLToUDP, localtalk.NameTashTalk} {
		// No Opener and no Serial in the context: BOTH segments must come up inert.
		c, ok, err := Build(key, &BuildContext{Model: enabledSegmentModel(key, "x")})
		if err != nil || !ok || c == nil {
			t.Fatalf("Build(%s) = (%v, %v, %v), want inert component", key, c, ok, err)
		}
		if err := c.Start(ctx); err != nil {
			t.Fatalf("Start (inert) %s: %v", key, err)
		}
		if err := c.Stop(ctx); err != nil {
			t.Fatalf("Stop (inert) %s: %v", key, err)
		}
	}
	if ltoudpOpened.Load() || ttFramed.Load() {
		t.Fatal("nil backends still opened a transport; should stay inert")
	}
}

// TestLToUDPFactory_ReopensOnRestart proves a Stop→Start reopens the transport:
// the open seam is called once per Start (a closed socket is terminal), so the
// port survives a UI restart with a fresh link.
func TestLToUDPFactory_ReopensOnRestart(t *testing.T) {
	var calls atomic.Int32
	links := []*idleFrameLink{{}, {}}
	swapLtoudpOpen(t, func(string) (link.FrameLink, error) {
		n := calls.Add(1)
		return links[n-1], nil
	})

	c, ok, err := Build(localtalk.NameLToUDP, &BuildContext{Model: enabledSegmentModel(localtalk.NameLToUDP, ""), Opener: anyOpener()})
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
