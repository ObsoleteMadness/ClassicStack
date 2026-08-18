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
	opener := func(iface, _ string) (link.FrameLink, error) {
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
		var openedIface, openedBPF atomic.Value
		opener := func(iface, bpf string) (link.FrameLink, error) {
			openedIface.Store(iface)
			openedBPF.Store(bpf)
			return &idleFrameLink{}, nil
		}
		m := config.NewModel()
		if bridge != "" {
			m.SetInterface(config.InterfaceSection{Name: bridge, Kind: config.IfaceKindBridge, Default: true})
		}
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
		// The port must program ITS OWN capture filter onto the handle — a shared
		// EtherTalk filter across all NIC ports is exactly the regression that starved
		// NetBEUI/IPX of their traffic. EtherTalk's is the AppleTalk (DDP+AARP) set.
		if got := openedBPF.Load(); got != ethertalk.BPFFilter {
			t.Fatalf("opened bpf = %v, want %v", got, ethertalk.BPFFilter)
		}
	}
	t.Run("inherits default interface when iface empty", func(t *testing.T) {
		check(t, "", "br0", "br0")
	})
	t.Run("override beats default interface", func(t *testing.T) {
		check(t, "eth9", "br0", "eth9")
	})
}

// TestEtherTalkFactory_AutoNIC proves the server "Easy mode" auto-NIC: a NIC port with
// NO iface of its own AND no namespace default interface (so its effective device is
// empty) falls back to the injected DefaultDevice — the host's primary NIC — so it comes
// up LIVE rather than inert. A configured iface still wins (DefaultDevice is a fallback
// only), and no DefaultDevice keeps the historical inert-but-routed degradation.
func TestEtherTalkFactory_AutoNIC(t *testing.T) {
	build := func(t *testing.T, sectionIface string, defaultDevice func() (string, error)) string {
		t.Helper()
		var openedIface atomic.Value
		openedIface.Store("<never-opened>")
		opener := func(iface, _ string) (link.FrameLink, error) {
			openedIface.Store(iface)
			return &idleFrameLink{}, nil
		}
		m := config.NewModel()
		// No [[Interface]] entries → DefaultInterface() is the zero section, so a section
		// with no iface resolves to an empty device: the auto-NIC precondition.
		m.Set(&port.Section{SKey: ethertalk.Name, Iface: sectionIface, IsEnabled: true})

		c, ok, err := Build(ethertalk.Name, &BuildContext{Model: m, Opener: opener, DefaultDevice: defaultDevice})
		if err != nil || !ok || c == nil {
			t.Fatalf("Build = (%v, %v, %v)", c, ok, err)
		}
		ctx := context.Background()
		if err := c.Start(ctx); err != nil {
			t.Fatalf("Start: %v", err)
		}
		defer c.Stop(ctx)
		return openedIface.Load().(string)
	}

	primary := func() (string, error) { return "\\Device\\NPF_{PRIMARY}", nil }

	t.Run("empty iface falls back to primary NIC", func(t *testing.T) {
		if got := build(t, "", primary); got != "\\Device\\NPF_{PRIMARY}" {
			t.Fatalf("opened iface = %q, want the auto-detected primary NIC", got)
		}
	})
	t.Run("configured iface beats auto-detect", func(t *testing.T) {
		if got := build(t, "eth7", primary); got != "eth7" {
			t.Fatalf("opened iface = %q, want the configured eth7 (auto-detect must not override)", got)
		}
	})
	t.Run("no resolver leaves the device empty", func(t *testing.T) {
		// With no DefaultDevice and no configured iface, the effective device stays empty —
		// the historical behaviour: the opener is invoked with "" (which a real pcap rejects,
		// giving the inert-but-routed degradation). Auto-detect must add NO new behaviour here.
		if got := build(t, "", nil); got != "" {
			t.Fatalf("opened iface = %q, want empty (no auto-detect) when DefaultDevice is nil", got)
		}
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
	opener := func(string, string) (link.FrameLink, error) {
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

// TestEtherTalkInstances_MultipleNamed proves the §M11 named-instance path: a model
// with TWO named EtherTalk instances on different interfaces expands (via Instances)
// to two distinct components, each named after its instance and opening its OWN
// interface. This is the multi-drop case the singleton shape could not express.
func TestEtherTalkInstances_MultipleNamed(t *testing.T) {
	m := config.NewModel()
	m.AddInstance(&port.Section{SKey: ethertalk.Name, Name: "et-lab", Iface: "eth0", IsEnabled: true})
	m.AddInstance(&port.Section{SKey: ethertalk.Name, Name: "et-dmz", Iface: "eth1", IsEnabled: true})

	// Instances must expand the one EtherTalk key into the two named instances.
	var got []string
	for _, id := range Instances(m) {
		if id.Key == ethertalk.Name {
			got = append(got, id.Instance)
		}
	}
	if len(got) != 2 {
		t.Fatalf("Instances expanded EtherTalk to %v, want [et-lab et-dmz]", got)
	}

	// Build each instance; each opens its own interface and names itself.
	opened := map[string]string{} // component name → opened iface
	for _, id := range Instances(m) {
		if id.Key != ethertalk.Name {
			continue
		}
		var iface atomic.Value
		opener := func(i, _ string) (link.FrameLink, error) { iface.Store(i); return &idleFrameLink{}, nil }
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
	if opened["et-lab"] != "eth0" || opened["et-dmz"] != "eth1" {
		t.Fatalf("instances opened the wrong interfaces: %v", opened)
	}
}

func TestEtherTalkFramer_HostMACUsesAARP(t *testing.T) {
	want := [6]byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}
	ctx := &BuildContext{
		HostMAC: func(device string) ([6]byte, error) {
			if device != "en0" {
				t.Fatalf("HostMAC device = %q, want en0", device)
			}
			return want, nil
		},
	}
	_, aarp := etherTalkFramer(ctx, &port.Section{}, config.InterfaceSection{Name: "en0"})
	if aarp == nil {
		t.Fatal("want AARP framer when HostMAC resolves")
	}
	var got [6]byte
	copy(got[:], aarp.SrcMAC)
	if got != want {
		t.Fatalf("SrcMAC = %v, want %v", got, want)
	}
}

func TestEtherTalkFramer_NoMACBroadcastOnly(t *testing.T) {
	_, aarp := etherTalkFramer(nil, &port.Section{}, config.InterfaceSection{})
	if aarp != nil {
		t.Fatal("want broadcast-only framer when no MAC is configured or detected")
	}
}
